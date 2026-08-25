//go:build !linux

package sandbox

import (
	"context"
	"os/exec"
)

// ExecArg gibt es auch hier, damit main() ohne Plattform-Weiche auskommt.
const ExecArg = "__sandbox-exec"

// Available: außerhalb von Linux gibt es kein Landlock. LCM als Dienst ist
// ohnehin für Debian/Ubuntu gedacht; auf anderen Plattformen läuft der
// CVE-Scan ungesperrt - und die Oberfläche sagt das auch.
func Available() Status {
	return Status{Reason: "Sandbox nur unter Linux verfügbar (Bubblewrap bzw. Landlock)"}
}

// Recheck gibt es auch hier, damit der Aufrufer keine Plattform-Weiche
// braucht - außerhalb von Linux ändert eine erneute Prüfung nichts.
func Recheck() Status { return Available() }

// sandboxedCommand wird nie aufgerufen (Available().Active ist false),
// existiert aber, damit sandbox.go plattformunabhängig übersetzt.
func sandboxedCommand(ctx context.Context, _ Spec, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// RunExec ist außerhalb von Linux ein Fehlerfall - das Unterkommando wird
// dort nie erzeugt.
func RunExec([]string) int { return 2 }
