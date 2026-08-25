//go:build linux

package sandbox

import (
	"context"
	"os/exec"
)

// Bubblewrap-Backend: sperrt den Kindprozess in einen eigenen Mount- (und
// optional Netz-) Namespace. Das ist stärker als eine Zugriffsregel - was
// nicht ausdrücklich hineingereicht wird, EXISTIERT für den Prozess nicht.
// Ein Leseversuch auf den Master-Key endet nicht mit „keine Berechtigung",
// sondern mit „Datei nicht gefunden".
//
// Bubblewrap braucht keine Sonderrechte (unprivilegierte User-Namespaces) und
// liegt in Debian/Ubuntu in main - kein Fremd-Repository, anders als Trivy
// selbst. Es ist eine WEICHE Abhängigkeit: fehlt es, greift Landlock oder,
// wenn auch das fehlt, gar nichts - und die Oberfläche sagt das.
const bwrapBinary = "bwrap"

// bwrapArgs baut die Aufrufzeile. Reihenfolge ist bedeutsam: Bubblewrap
// arbeitet die Optionen der Reihe nach ab, ein späteres --bind liegt also
// ÜBER einem früheren --tmpfs.
func bwrapArgs(spec Spec, name string, args []string) []string {
	out := []string{
		// Kein geerbtes Wurzeldateisystem: alles Sichtbare wird unten
		// ausdrücklich hineingereicht.
		"--die-with-parent", // stirbt LCM, stirbt auch der Scan
		"--new-session",     // eigene Sitzung: keine Eingaben ins Terminal des Elternprozesses
		"--proc", "/proc",
		"--dev", "/dev",
	}
	// Das eigene /tmp muss VOR alle Einhängungen: es verdeckt sonst jeden
	// zuvor eingehängten Pfad unterhalb von /tmp. Genau daran scheiterte der
	// erste echte Scan - die SBOM-Datei liegt in /tmp, wurde eingehängt und
	// vom nachfolgenden tmpfs wieder unsichtbar gemacht („failed to open
	// sbom file: no such file or directory"). Das echte /tmp des Hosts bleibt
	// dem Kindprozess damit trotzdem verborgen.
	if spec.ScratchTmp {
		out = append(out, "--tmpfs", "/tmp")
	}
	// Lesbare Verzeichnisse und Dateien. „-try" überspringt fehlende Pfade -
	// welche Verzeichnisse eine Distribution mitbringt, ist verschieden.
	for _, d := range spec.ReadDirs {
		out = append(out, "--ro-bind-try", d, d)
	}
	for _, f := range spec.ReadFiles {
		out = append(out, "--ro-bind-try", f, f)
	}
	for _, d := range spec.WriteDirs {
		out = append(out, "--bind-try", d, d)
	}
	// Namespaces trennen. Ohne Netz: --unshare-all nimmt auch den
	// Netz-Namespace, der Prozess hat dann nur noch ein Loopback-Gerät.
	if spec.AllowNet {
		out = append(out, "--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts")
	} else {
		out = append(out, "--unshare-all")
	}
	return append(append(out, "--"), append([]string{name}, args...)...)
}

// bwrapCommand baut den fertigen Aufruf.
func bwrapCommand(ctx context.Context, spec Spec, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, bwrapBinary, bwrapArgs(spec, name, args)...)
}

// bwrapUsable prüft, ob Bubblewrap vorhanden UND tatsächlich benutzbar ist.
// Die zweite Hälfte ist wesentlich: unprivilegierte User-Namespaces sind auf
// manchen Systemen abgeschaltet (Kernel-Schalter, AppArmor-Regel bei Ubuntu,
// verschachtelte Container). Dann ist das Binary zwar da, aber wirkungslos -
// das muss vor dem ersten Scan auffallen, nicht mittendrin.
func bwrapUsable() bool {
	if _, err := exec.LookPath(bwrapBinary); err != nil {
		return false
	}
	// Geprobt wird mit GENAU der Aufrufzeile, die später auch benutzt wird.
	// Eine vereinfachte Probe (nur /usr eingebunden) scheitert am fehlenden
	// Dynamic Linker und stempelte Bubblewrap fälschlich als unbrauchbar ab -
	// die Sandbox wäre dann stillschweigend nie zum Einsatz gekommen.
	spec := BaseSystemSpec().WithNet(false)
	spec.ScratchTmp = true
	return exec.Command(bwrapBinary, bwrapArgs(spec, "/usr/bin/env", []string{"true"})...).Run() == nil
}
