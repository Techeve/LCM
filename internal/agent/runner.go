package agent

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// defaultIdleTimeout ist die Agent-eigene Notbremse, wenn der Server keine
// Frist mitschickt (älterer Server). Sie greift nach STILLE, nicht nach
// Gesamtdauer: Ein Kommando darf beliebig lange laufen, solange es dabei
// Ausgabe erzeugt - nur wer gar nichts mehr von sich gibt, gilt als hängend.
// Normalerweise bricht der LCM-Server längst vorher per Cancel ab.
const defaultIdleTimeout = 60 * time.Minute

// commandIDWindow ist die Zahl der Befehls-IDs, die sich der Runner merkt -
// für erkannte Doppel und für Abbrüche, die vor ihrem Kommando eintreffen.
// LCM schickt einem Agenten ein Kommando nach dem anderen; 1024 reichen
// weit über jede erneute Zustellung hinaus und kosten unter 100 KB.
const commandIDWindow = 1024

// errCancelledBeforeStart meldet ein Kommando, dessen Abbruch schon da war,
// als es an die Reihe kam.
var errCancelledBeforeStart = errors.New("kommando abgebrochen, bevor es startete")

// Runner führt Kommandos des LCM-Servers aus: /bin/sh -c in einer eigenen
// Prozessgruppe (Setsid), kombinierter Output mit Größenlimit, Abbruch per
// Kill der ganzen Gruppe. Es läuft höchstens ein Kommando gleichzeitig -
// dieselbe Serialisierung, die serverseitig der ConnLimiter erzwingt.
type Runner struct {
	execMu sync.Mutex // serialisiert die Ausführung

	mu        sync.Mutex
	running   map[string]*exec.Cmd // Request-ID → laufender Prozess
	seen      *idWindow            // angenommene Befehls-IDs
	cancelled *idWindow            // abgebrochene Befehls-IDs
}

func NewRunner() *Runner {
	return &Runner{
		running:   map[string]*exec.Cmd{},
		seen:      newIDWindow(commandIDWindow),
		cancelled: newIDWindow(commandIDWindow),
	}
}

// Claim nimmt ein Kommando an und meldet false, wenn seine ID schon einmal
// da war. MQTT mit QoS 1 stellt mindestens einmal zu - eine zweite Zustellung
// darf das Kommando nicht ein zweites Mal ausführen.
func (r *Runner) Claim(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen.add(id)
}

// Run führt das Kommando aus und liefert das Ergebnis (blockiert). progress
// wird - sofern gesetzt - im Takt von wire.ProgressInterval mit dem bisher
// gesammelten Ausgabe-Umfang aufgerufen, solange das Kommando läuft; darüber
// erfährt der Server, dass der Lauf noch arbeitet.
func (r *Runner) Run(cmd wire.Command, progress func(outputBytes int)) wire.Result {
	r.execMu.Lock()
	defer r.execMu.Unlock()

	res := wire.Result{ID: cmd.ID, ExitCode: -1}

	proc := exec.Command("/bin/sh", "-c", cmd.Cmd)
	// Eigene Prozessgruppe: Cancel killt das Kommando MIT allen Kindern.
	proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Stdin != "" {
		proc.Stdin = strings.NewReader(cmd.Stdin)
	}
	out := &limitedBuffer{max: wire.MaxOutputBytes}
	proc.Stdout = out
	proc.Stderr = out
	// WaitDelay entkoppelt Wait() von Hintergrund-Kindern, die die
	// stdout/stderr-Pipes geerbt haben (dasselbe fd-Problem, das server-
	// seitig detachBackgroundFDs löst - hier als zweites Netz): nach dem
	// Exit des Hauptprozesses wird maximal so lange auf die Pipes gewartet.
	proc.WaitDelay = 10 * time.Second

	if err := r.start(cmd.ID, proc); err != nil {
		res.Error = err.Error()
		return res
	}
	defer r.forget(cmd.ID)

	// Notbremse gegen verwaiste, hängende Kommandos (Cancel kommt normal
	// lange vorher vom Server) - und zugleich der Takt der Lebenszeichen.
	stopWatch := r.watch(cmd, out, progress)
	defer stopWatch()

	err := proc.Wait()
	switch {
	case err == nil:
		res.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.Error = err.Error()
		}
	}
	// Zuletzt, wenn alle übrigen Felder stehen: Gemessen wird das ganze
	// serialisierte Ergebnis.
	head, tail, omitted := out.parts()
	fitOutput(&res, head, tail, omitted, wire.MaxResultBytes)
	return res
}

// start startet den Prozess und trägt ihn ein - unter derselben Sperre, die
// Cancel nimmt. So geht kein Abbruch verloren: Er kam entweder vorher (dann
// startet das Kommando gar nicht, auch wenn es hinter einem anderen wartete
// oder der Abbruch es auf dem Weg überholt hat) oder er trifft den Prozess.
func (r *Runner) start(id string, proc *exec.Cmd) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled.has(id) {
		return errCancelledBeforeStart
	}
	if err := proc.Start(); err != nil {
		return errors.New("kommando starten: " + err.Error())
	}
	r.running[id] = proc
	return nil
}

// forget trägt ein beendetes Kommando aus.
func (r *Runner) forget(id string) {
	r.mu.Lock()
	delete(r.running, id)
	r.mu.Unlock()
}

// watch begleitet ein laufendes Kommando: Es meldet den Fortschritt an den
// Server und bricht ab, sobald der Ausgabe-Umfang über die erlaubte Stille
// hinweg unverändert bleibt. Rückgabe ist die Abschaltfunktion.
func (r *Runner) watch(cmd wire.Command, out *limitedBuffer, progress func(int)) func() {
	idle := time.Duration(cmd.IdleTimeoutSec) * time.Second
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	done := make(chan struct{})
	ticker := time.NewTicker(wire.ProgressInterval)
	safego.Go("agent-watch:"+cmd.ID, func() {
		defer ticker.Stop()
		seen, since := out.Written(), time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if n := out.Written(); n != seen {
					seen, since = n, time.Now()
				} else if time.Since(since) >= idle {
					r.Cancel(cmd.ID)
					return
				}
				if progress != nil {
					progress(seen)
				}
			}
		}
	})
	return func() { close(done) }
}

// Cancel bricht ein Kommando ab (Kill der Prozessgruppe). Läuft es noch
// nicht, merkt sich der Runner den Abbruch, und es startet gar nicht erst.
func (r *Runner) Cancel(reqID string) {
	r.mu.Lock()
	r.cancelled.add(reqID)
	proc := r.running[reqID]
	r.mu.Unlock()
	if proc == nil || proc.Process == nil {
		return
	}
	// Negative PID = Prozessgruppe (Setsid oben).
	_ = syscall.Kill(-proc.Process.Pid, syscall.SIGKILL)
}

// idWindow merkt sich die zuletzt eingetragenen IDs, höchstens max Stück;
// die älteste fällt zuerst heraus. Nicht threadsicher - der Runner hält mu.
type idWindow struct {
	max   int
	order []string
	set   map[string]struct{}
}

func newIDWindow(max int) *idWindow {
	return &idWindow{max: max, set: map[string]struct{}{}}
}

// add trägt id ein und meldet false, wenn sie schon drin war.
func (w *idWindow) add(id string) bool {
	if w.has(id) {
		return false
	}
	w.set[id] = struct{}{}
	w.order = append(w.order, id)
	if len(w.order) > w.max {
		delete(w.set, w.order[0])
		w.order = w.order[1:]
	}
	return true
}

func (w *idWindow) has(id string) bool {
	_, ok := w.set[id]
	return ok
}
