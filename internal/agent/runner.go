package agent

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"LCM/internal/remote/wire"
)

// hardRuntimeCap ist die Agent-eigene Notbremse je Kommando - normalerweise
// bricht der LCM-Server (Watchdog) viel früher per Cancel ab; die Kappe
// verhindert nur, dass ein verwaistes Kommando ewig weiterläuft.
const hardRuntimeCap = 4 * time.Hour

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

// Run führt das Kommando aus und liefert das Ergebnis (blockiert).
func (r *Runner) Run(cmd wire.Command) wire.Result {
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

	// Notbremse gegen verwaiste Endlos-Kommandos (Cancel kommt normal
	// lange vorher vom Server).
	capTimer := time.AfterFunc(hardRuntimeCap, func() { r.Cancel(cmd.ID) })
	defer capTimer.Stop()

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
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
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
