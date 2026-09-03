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

// Runner führt Kommandos des LCM-Servers aus: /bin/sh -c in einer eigenen
// Prozessgruppe (Setsid), kombinierter Output mit Größenlimit, Abbruch per
// Kill der ganzen Gruppe. Es läuft höchstens ein Kommando gleichzeitig -
// dieselbe Serialisierung, die serverseitig der ConnLimiter erzwingt.
type Runner struct {
	execMu sync.Mutex // serialisiert die Ausführung

	mu      sync.Mutex
	running map[string]*exec.Cmd // Request-ID → laufender Prozess
}

func NewRunner() *Runner {
	return &Runner{running: map[string]*exec.Cmd{}}
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

	if err := proc.Start(); err != nil {
		res.Error = "kommando starten: " + err.Error()
		return res
	}
	r.mu.Lock()
	r.running[cmd.ID] = proc
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.running, cmd.ID)
		r.mu.Unlock()
	}()

	// Notbremse gegen verwaiste, hängende Kommandos (Cancel kommt normal
	// lange vorher vom Server) - und zugleich der Takt der Lebenszeichen.
	stopWatch := r.watch(cmd, out, progress)
	defer stopWatch()

	err := proc.Wait()
	res.Output = out.String()
	res.Truncated = out.truncated
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
	return res
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

// Cancel bricht ein laufendes Kommando ab (Kill der Prozessgruppe).
// No-op, wenn nichts (mehr) läuft.
func (r *Runner) Cancel(reqID string) {
	r.mu.Lock()
	proc := r.running[reqID]
	r.mu.Unlock()
	if proc == nil || proc.Process == nil {
		return
	}
	// Negative PID = Prozessgruppe (Setsid oben).
	_ = syscall.Kill(-proc.Process.Pid, syscall.SIGKILL)
}

// limitedBuffer sammelt Output bis max Bytes; Überschuss wird verworfen
// und truncated gesetzt (der Server hängt dann einen Hinweis an).
type limitedBuffer struct {
	mu        sync.Mutex
	buf       strings.Builder
	max       int
	truncated bool
	written   int // alle geschriebenen Bytes, auch die verworfenen
}

// Written ist die Zahl ALLER bisher geschriebenen Bytes - auch der wegen des
// Limits verworfenen. Daran erkennt watch, ob das Kommando noch arbeitet; die
// Länge des Puffers taugt dafür nicht, weil sie nach dem Kürzen stehen bliebe
// und ein weiterhin arbeitendes Kommando als hängend erscheinen ließe.
func (b *limitedBuffer) Written() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.written += len(p)
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	// Immer len(p) melden - der Schreiber (das Kommando) soll weiterlaufen.
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
