//go:build linux

package sandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

// ExecArg ist das interne Unterkommando, mit dem sich LCM selbst als
// Sandbox-Starter aufruft. Der Umweg über einen eigenen Prozess ist nötig,
// weil Landlock-Regeln für den aufrufenden Prozess UND seine Kinder gelten:
// im LCM-Dienst selbst gesetzt, würden sie LCM lahmlegen. Das Kind setzt sie
// deshalb erst kurz vor dem exec auf sich selbst.
const ExecArg = "__sandbox-exec"

// minABI ist die Landlock-Stufe, ab der die Dateisperre trägt (ABI 1,
// Kernel 5.13). Debian 12 und Ubuntu 22.04 liegen darüber.
const minABI = 1

// netABI ist die Stufe, ab der auch TCP-Verbindungen sperrbar sind
// (ABI 4, Kernel 6.7 - Debian 13, Ubuntu 24.04).
const netABI = 4

// Das Prüfergebnis wird gemerkt (probe startet einen Prozess) und lässt sich
// gezielt verwerfen - siehe Recheck.
var (
	statusMu  sync.RWMutex
	statusSet bool
	statusVal Status
)

// Available ermittelt, ob eine Sandbox auf diesem Host nutzbar ist - beim
// ersten Aufruf tatsächlich, danach aus dem Gemerkten.
func Available() Status {
	statusMu.RLock()
	if statusSet {
		defer statusMu.RUnlock()
		return statusVal
	}
	statusMu.RUnlock()
	return Recheck()
}

// Recheck prüft neu und ersetzt das Gemerkte. Nötig, wenn sich der Host im
// Betrieb ändert - etwa wenn bubblewrap nachinstalliert wird: sonst bliebe
// LCM bis zum nächsten Dienst-Neustart bei „ohne Sandbox", obwohl die
// Voraussetzung längst da ist.
func Recheck() Status {
	// Außerhalb der Sperre prüfen: probe startet einen Prozess, und solange
	// darf niemand blockieren. Zwei gleichzeitige Erstaufrufe prüfen dann
	// doppelt - das kostet nichts und verfälscht nichts.
	st := probe()
	statusMu.Lock()
	statusVal, statusSet = st, true
	statusMu.Unlock()
	return st
}

// probe wählt das Backend. Bubblewrap zuerst: es trennt über Namespaces und
// macht Fremdes damit unsichtbar statt nur unlesbar, und es sperrt das Netz
// unabhängig von der Kernel-Version. Landlock ist die Rückfallebene für
// Hosts ohne Bubblewrap - es braucht dafür allerdings einen Kernel, der
// Landlock nicht nur mitbringt, sondern auch AKTIVIERT hat (LSM-Liste; der
// Proxmox-Kernel etwa tut das ohne `lsm=`-Bootparameter nicht).
func probe() Status {
	if bwrapUsable() {
		return Status{Active: true, Backend: "bubblewrap", NetEnforced: true}
	}
	abi, err := llsyscall.LandlockGetABIVersion()
	switch {
	case err != nil:
		return Status{Reason: "weder Bubblewrap noch Landlock verfügbar - Landlock ist in diesem Kernel nicht aktiviert " +
			"(LSM-Liste, siehe /sys/kernel/security/lsm), und das Paket bubblewrap fehlt"}
	case abi < minABI:
		return Status{Reason: fmt.Sprintf("kein Bubblewrap, und die Landlock-Stufe %d ist zu alt (nötig: %d)", abi, minABI)}
	}
	if _, err := os.Executable(); err != nil {
		return Status{Reason: "der eigene Programmpfad ist nicht ermittelbar: " + err.Error()}
	}
	return Status{Active: true, Backend: "landlock", NetEnforced: abi >= netABI}
}

// sandboxedCommand wählt anhand des ermittelten Backends.
func sandboxedCommand(ctx context.Context, spec Spec, name string, args ...string) *exec.Cmd {
	if Available().Backend == "bubblewrap" {
		return bwrapCommand(ctx, spec, name, args...)
	}
	return landlockCommand(ctx, spec, name, args...)
}

// landlockCommand baut den Selbstaufruf: <lcm> __sandbox-exec <spec> -- <programm> <args…>
func landlockCommand(ctx context.Context, spec Spec, name string, args ...string) *exec.Cmd {
	self, err := os.Executable()
	if err != nil {
		// Kann nach dem erfolgreichen probe() praktisch nicht passieren;
		// dann lieber gewöhnlich starten als gar nicht scannen.
		return exec.CommandContext(ctx, name, args...)
	}
	encoded, err := encodeSpec(spec)
	if err != nil {
		return exec.CommandContext(ctx, name, args...)
	}
	full := append([]string{ExecArg, encoded, "--", name}, args...)
	cmd := exec.CommandContext(ctx, self, full...)
	// Die Umgebung wird bewusst nicht vererbt: der Kindprozess braucht nur
	// einen PATH und eine HOME-Angabe. Alles Weitere aus LCMs Umgebung geht
	// ihn nichts an.
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/nonexistent",
	}
	return cmd
}

func encodeSpec(spec Spec) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// RunExec ist der Einstiegspunkt des Kindprozesses: Regeln setzen, dann das
// eigentliche Programm werden. Wird aus main() aufgerufen, BEVOR LCM
// irgendetwas anderes tut - der Prozess hier ist kein Server, sondern nur ein
// Türsteher, der sich selbst einsperrt und dann verschwindet.
//
// Fail-closed: Lassen sich die Regeln nicht setzen, wird NICHT ersatzweise
// ungesperrt gestartet. Der aufrufende Dienst hat über Available() bereits
// festgestellt, dass Landlock trägt; scheitert es hier trotzdem, ist etwas
// grundlegend anders als angenommen - dann ist ein ehrlicher Abbruch besser
// als ein Programm mit vollem Zugriff auf den Master-Key.
func RunExec(argv []string) int {
	spec, target, targetArgs, err := parseExecArgs(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcm-sandbox:", err)
		return 2
	}
	if err := apply(spec); err != nil {
		fmt.Fprintln(os.Stderr, "lcm-sandbox: regeln nicht durchsetzbar:", err)
		return 2
	}
	// Ab hier gelten die Regeln. exec ersetzt das Prozessabbild - die
	// Beschränkungen bleiben dabei bestehen und lassen sich nicht ablegen.
	if err := syscall.Exec(target, append([]string{target}, targetArgs...), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "lcm-sandbox: %s nicht startbar: %v\n", target, err)
		return 2
	}
	return 0 // nie erreicht
}

// parseExecArgs zerlegt „<spec> -- <programm> [args…]".
func parseExecArgs(argv []string) (Spec, string, []string, error) {
	var spec Spec
	if len(argv) < 3 {
		return spec, "", nil, fmt.Errorf("aufruf: %s <spec> -- <programm> [args…]", ExecArg)
	}
	raw, err := base64.StdEncoding.DecodeString(argv[0])
	if err != nil {
		return spec, "", nil, fmt.Errorf("spec nicht lesbar: %w", err)
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return spec, "", nil, fmt.Errorf("spec nicht lesbar: %w", err)
	}
	if argv[1] != "--" {
		return spec, "", nil, fmt.Errorf("erwartet „--" + "\" vor dem Programmnamen")
	}
	return spec, argv[2], argv[3:], nil
}

// apply setzt die Landlock-Regeln auf den laufenden Prozess.
//
// BestEffort schaltet auf die höchste Stufe herunter, die der Kernel kann:
// die Dateisperre greift ab ABI 1, die Netzsperre erst ab ABI 4. Fehlende
// Pfade werden übersprungen (IgnoreIfMissing) - welche Verzeichnisse eine
// Distribution mitbringt, ist verschieden, und ein fehlendes /usr/lib64 darf
// den Scan nicht verhindern.
func apply(spec Spec) error {
	runtime.LockOSThread()

	rules := make([]landlock.Rule, 0, len(spec.ReadDirs)+len(spec.ReadFiles)+len(spec.WriteDirs))
	if len(spec.ReadDirs) > 0 {
		rules = append(rules, landlock.RODirs(spec.ReadDirs...).IgnoreIfMissing())
	}
	if len(spec.ReadFiles) > 0 {
		rules = append(rules, landlock.ROFiles(spec.ReadFiles...).IgnoreIfMissing())
	}
	if len(spec.WriteDirs) > 0 {
		rules = append(rules, landlock.RWDirs(spec.WriteDirs...).IgnoreIfMissing())
	}
	// Landlock kann kein eigenes /tmp erzeugen wie Bubblewrap - hier bleibt
	// nur, das vorhandene freizugeben. Unter systemd ist es dank PrivateTmp
	// immerhin schon vom übrigen System getrennt.
	if spec.ScratchTmp {
		rules = append(rules, landlock.RWDirs(os.TempDir()).IgnoreIfMissing())
	}

	cfg := landlock.V4.BestEffort()
	if spec.AllowNet {
		// Netz offen lassen: nur die Dateiregeln setzen. Würde man hier
		// zusätzlich RestrictNet ohne erlaubte Ports aufrufen, wäre jede
		// ausgehende Verbindung dicht - das braucht der DB-Download aber.
		return cfg.RestrictPaths(rules...)
	}
	// Restrict setzt Datei- UND Netzregeln in einem Schritt. Ohne ConnectTCP-
	// Regel bleibt keine ausgehende Verbindung erlaubt.
	return cfg.Restrict(rules...)
}
