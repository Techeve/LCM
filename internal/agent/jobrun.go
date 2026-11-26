package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Start eines Kommandos außerhalb des Agents (C-1).
//
// Ein Kommando soll weder an der Control-Group noch an den Pipes des Agents
// hängen: systemd beendet beim Stopp des Dienstes alle Prozesse seiner Group,
// und ein Schreibzugriff auf eine Pipe, deren Leser gestorben ist, schickt
// SIGPIPE (apt endet dann mitten im Lauf). Deshalb
//   - läuft es unter systemd als eigener Scope (systemd-run --scope): ein
//     Absturz, Neustart oder `systemctl stop` des Agents lässt es unberührt.
//     Ohne systemd (OpenRC, von Hand gestartet) gibt es nichts, was die Kinder
//     des Agents mitreißt - dort genügt die eigene Sitzung (Setsid).
//   - gehen Stdout und Stderr in eine Datei der Job-Ablage statt in eine Pipe,
//   - schreibt ein kleiner Wrapper den Exit-Code in eine Datei, wenn das
//     Kommando endet - auch wenn der Agent dann gar nicht läuft.
//
// Das Kommando bleibt Kind des Agents, solange er lebt (so sammelt er es ein);
// stirbt er, übernimmt es init. Beides ist für das Kommando unsichtbar.

// jobWrapper ist das Skript, das jedes Kommando umschließt. $1 ist das
// Verzeichnis des Jobs. Es schreibt als Erstes seine PID (die der Sitzung und
// der Prozessgruppe), führt dann cmd.sh mit Ausgabe in die Datei aus und
// schreibt zuletzt den Exit-Code - über eine Zwischendatei, damit nie ein halber
// Wert liegt.
//
// Das trap fängt das SIGTERM eines Abbruchs ab: Die Shell wartet dann das Ende
// des Kommandos ab (das dasselbe Signal in der Gruppe erhält, ein trap gilt
// nicht für Kinder) und schreibt den Code trotzdem. Gesetzt wird es erst, wenn
// das Kommando schon läuft: Ein Abbruch gleich nach dem Start träfe sonst nur
// die Shell, die ihn schluckt, und das Kommando liefe danach unbehelligt los.
// So beendet ein frühes SIGTERM beide, ein spätes nur das Kommando. Ein
// unterbrochenes wait (Exit über 128) wartet weiter, bis das Kommando wirklich
// zu Ende ist. Die Eingabe geht über fd 3: Ein Hintergrundkommando bekommt
// sonst /dev/null, und dash setzt das noch vor einem ausdrücklichen <&0.
const jobWrapper = `d=$1; echo $$ >"$d/pid"; exec 3<&0
/bin/sh "$d/cmd.sh" <&3 3<&- >>"$d/out" 2>&1 &
c=$!
trap : TERM
while :; do wait "$c"; rc=$?; kill -0 "$c" 2>/dev/null || break; done
if [ "$rc" -gt 128 ]; then wait "$c"; r=$?; [ "$r" -ne 127 ] && rc=$r; fi
echo $rc >"$d/exit.tmp" && mv "$d/exit.tmp" "$d/exit"`

// scopeAuto lässt den Runner selbst prüfen, ob systemd-run taugt.
const scopeAuto = "auto"

const (
	// scopeProbeWait ist die Zeit, die die Probe von systemd-run hat, scopeKillWait
	// die von systemctl kill.
	scopeProbeWait = 20 * time.Second
	scopeKillWait  = 10 * time.Second
	// scopeRetryAfter ist die Wartezeit, nach der der Runner eine gescheiterte
	// Probe wiederholt: Ein vorübergehender Fehler (systemd überlastet, dbus
	// gerade neu gestartet) soll nicht bis zum nächsten Neustart des Agents
	// kosten, dass Kommandos an seiner Control-Group hängen.
	scopeRetryAfter = time.Minute
)

// errNoSystemd: Auf dem System läuft kein systemd (oder kein systemd-run).
var errNoSystemd = errors.New("kein systemd")

// scopeName ist der Name der systemd-Unit eines Jobs.
func scopeName(id string) string { return "lcm-job-" + safeID(id) }

// scopeState hält das Ergebnis der Erkennung von systemd-run.
type scopeState struct {
	mu         sync.Mutex
	path       string        // gefundenes systemd-run
	none       bool          // kein systemd: endgültig
	failedAt   time.Time     // letzte gescheiterte Probe (Null: keine offen)
	retryAfter time.Duration // Wartezeit bis zur nächsten Probe
	// probe prüft systemd-run (Tests ersetzen es).
	probe func() (string, error)
}

// useScope meldet, ob Kommandos als systemd-Scope laufen (prüft bei Bedarf,
// siehe probeScope).
func (r *Runner) useScope() bool {
	r.probeScope()
	path, _ := r.scopeCached()
	return path != ""
}

// scopeCached liefert den Pfad von systemd-run, "" ohne Scope - ohne je zu
// prüfen, also auch unter r.mu aufrufbar. degraded meldet, dass systemd da ist,
// die Probe aber scheiterte: Dann hängen Kommandos an der Control-Group des
// Agents.
func (r *Runner) scopeCached() (path string, degraded bool) {
	if r.systemdRun != scopeAuto {
		return r.systemdRun, false
	}
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	return r.scope.path, r.scope.path == "" && !r.scope.failedAt.IsZero()
}

// probeScope prüft systemd-run beim ersten Mal und nach jedem Fehlschlag erneut,
// höchstens alle retryAfter. Die Probe braucht Sekunden: Run ruft sie vor dem
// Start des Kommandos, nie unter r.mu (Cancel und Vitals warteten sonst darauf).
func (r *Runner) probeScope() {
	if r.systemdRun != scopeAuto {
		return
	}
	s := &r.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" || s.none {
		return
	}
	if !s.failedAt.IsZero() && time.Since(s.failedAt) < s.retryAfter {
		return
	}
	probe := s.probe
	if probe == nil {
		probe = r.detectSystemdRun
	}
	found, err := probe()
	switch {
	case err == nil:
		if !s.failedAt.IsZero() {
			r.log.Info("systemd-run works again - commands run as scopes")
		}
		s.path, s.failedAt = found, time.Time{}
	case errors.Is(err, errNoSystemd):
		s.none = true
	default:
		r.log.Warn("systemd-run cannot create a scope - commands run in the agent's control group until it can; "+
			"stopping or restarting the agent ends them", "error", err, "retry_in", s.retryAfter)
		s.failedAt = time.Now()
	}
}

// detectSystemdRun liefert den Pfad von systemd-run, wenn systemd das System
// verwaltet und eine Probe einen Scope anlegen konnte. errNoSystemd, wenn es
// kein systemd gibt - dann gibt es auch nichts, was die Kinder des Agents
// mitreißt.
func (r *Runner) detectSystemdRun() (string, error) {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return "", errNoSystemd
	}
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return "", errNoSystemd
	}
	ctx, cancel := context.WithTimeout(context.Background(), scopeProbeWait)
	defer cancel()
	probe := exec.CommandContext(ctx, path, "--scope", "--collect", "--quiet",
		"--unit=lcm-probe-"+strconv.Itoa(os.Getpid()), "/bin/true")
	if out, err := probe.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}

// spawn startet den Wrapper des Jobs. stdin läuft über eine Pipe: Es kann ein
// Passwort sein und gehört nicht auf die Platte, und ein Leser ohne Schreiber
// sieht nur das Ende der Eingabe, kein SIGPIPE.
func (r *Runner) spawn(j *job, stdin string) error {
	argv := []string{"/bin/sh", "-c", jobWrapper, "lcm-job", j.dir}
	name := argv[0]
	systemdRun, degraded := r.scopeCached()
	if degraded {
		r.log.Warn("command runs without a systemd scope - an agent stop or restart ends it", "id", j.id)
	}
	if systemdRun != "" {
		scope := scopeName(j.id)
		j.meta.Scope = scope + ".scope"
		name = systemdRun
		argv = append([]string{name, "--scope", "--collect", "--quiet", "--unit=" + scope,
			"--description=LCM job " + safeID(j.id), "--"}, argv...)
	}
	proc := exec.Command(name, argv[1:]...)
	proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if stdin != "" {
		proc.Stdin = strings.NewReader(stdin)
	}
	// Nur für die Eingabe-Pipe: Ein Hintergrundprozess des Kommandos, der sie
	// geerbt hat, hielte sonst Wait() fest.
	proc.WaitDelay = 10 * time.Second
	if err := proc.Start(); err != nil {
		return errors.New("kommando starten: " + err.Error())
	}
	j.proc = proc
	j.meta.PIDStart = procStartTime(proc.Process.Pid)
	return j.save()
}

// exited liefert einen Kanal, der schließt, sobald das Kommando zu Ende ist.
// Ein selbst gestartetes sammelt Wait ein; ein wieder aufgenommenes (kein Kind
// dieses Prozesses, oder Kind des alten Abbilds nach einem exec) prüft die
// Ablage im Takt poll. Der Kanal schließt auch bei quit, damit niemand
// hängen bleibt.
func (r *Runner) exited(j *job) <-chan struct{} {
	done := make(chan struct{})
	if j.proc != nil {
		go func() {
			_ = j.proc.Wait()
			j.waited.Store(true)
			close(done)
		}()
		return done
	}
	go func() {
		defer close(done)
		for !j.gone() {
			select {
			case <-time.After(r.pollEvery):
			case <-r.quit:
				return
			}
		}
		// Ein Kind des alten Abbilds (exec behält die PID) sammelt sonst niemand ein.
		if !j.rebooted() {
			go reapWrapper(j.pid(), j.meta.PIDStart)
		}
	}()
	return done
}

// jobTarget ist, was ein Signal an einen Job braucht - festgehalten beim Abbruch,
// denn nach der Frist ist die Ablage womöglich schon aufgeräumt.
type jobTarget struct {
	pid   int
	start uint64 // Startzeit des Wrappers, 0 = unbekannt
	scope string // systemd-Scope, leer ohne
}

func (j *job) target() jobTarget {
	j.mu.Lock()
	defer j.mu.Unlock()
	return jobTarget{pid: j.pid(), start: j.meta.PIDStart, scope: j.meta.Scope}
}

// abort hält das Kommando ausdrücklich an: SIGTERM an alles, was zum Job gehört,
// und nach der Frist SIGKILL an alles, was dann noch übrig ist - auch wenn der
// Wrapper selbst längst beendet ist und nur ein Hintergrundprozess des Kommandos
// weiterläuft. Anders als ein Kill allein lässt es dem Kommando Zeit, aufzuräumen
// (apt gibt seine Sperren frei, dpkg beendet den laufenden Schritt).
func (r *Runner) abort(j *job) {
	_ = j.update(func(m *jobMeta) { m.Aborted = true })
	if j.rebooted() {
		return // nach einem Neustart gibt es nichts mehr zu beenden, die PIDs sind fremd
	}
	t := j.target()
	r.signal(t, "SIGTERM", syscall.SIGTERM)
	time.AfterFunc(r.abortGrace, func() { r.signal(t, "SIGKILL", syscall.SIGKILL) })
}

// killProcess sendet ein Signal (eine Variable, damit Tests es abfangen, statt
// es wirklich zu senden).
var killProcess = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

// signal schickt das Signal an den Job: Mit Scope über systemd, der alle
// Prozesse des Scopes trifft, sonst an die Prozessgruppe des Wrappers - und nur,
// wenn die PID noch ihm gehört (groupIsOurs).
func (r *Runner) signal(t jobTarget, name string, sig syscall.Signal) {
	if t.scope != "" && r.killScope(t.scope, name) {
		return
	}
	// pid 1 nie: kill(-1) träfe jeden Prozess des Systems, und die Datei pid
	// stammt von der Platte (halb geschrieben oder beschädigt).
	if t.pid > 1 && groupIsOurs(t.pid, t.start) {
		// Negative PID = Prozessgruppe (Setsid).
		_ = killProcess(-t.pid, sig)
	}
}

// killScope schickt das Signal über `systemctl kill` an alle Prozesse des Scopes.
// false, wenn es nicht ging (kein systemctl, der Scope ist schon weg).
func (r *Runner) killScope(scope, signal string) bool {
	path, err := exec.LookPath(r.systemctl)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), scopeKillWait)
	defer cancel()
	return exec.CommandContext(ctx, path, "kill", "--signal="+signal, scope).Run() == nil
}
