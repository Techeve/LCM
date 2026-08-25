//go:build linux

package sandbox

import (
	"strings"
	"testing"
)

// argsAsString macht die Aufrufzeile für Prüfungen greifbar.
func argsAsString(spec Spec, name string, args []string) string {
	return strings.Join(bwrapArgs(spec, name, args), " ")
}

// TestBwrapTrenntNetzNurWennErlaubt: der SBOM-Scan läuft ohne Netz, der
// Datenbank-Download mit. Das ist der Unterschied zwischen „kann nichts
// abfließen lassen" und „kann es doch".
func TestBwrapTrenntNetzNurWennErlaubt(t *testing.T) {
	ohne := argsAsString(BaseSystemSpec().WithNet(false), "/usr/bin/trivy", nil)
	if !strings.Contains(ohne, "--unshare-all") {
		t.Error("ohne Netzfreigabe fehlt --unshare-all - der Prozess käme ins Netz")
	}
	mit := argsAsString(BaseSystemSpec().WithNet(true), "/usr/bin/trivy", nil)
	if strings.Contains(mit, "--unshare-all") {
		t.Error("mit Netzfreigabe darf --unshare-all nicht gesetzt sein (der Download bräuchte das Netz)")
	}
	for _, want := range []string{"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts"} {
		if !strings.Contains(mit, want) {
			t.Errorf("auch mit Netz muss %s getrennt bleiben", want)
		}
	}
}

// TestBwrapBindetNurDasErlaubte: das LCM-Datenverzeichnis darf in der
// Aufrufzeile nicht auftauchen - weder lesend noch schreibend.
func TestBwrapBindetNurDasErlaubte(t *testing.T) {
	spec := BaseSystemSpec().WithPaths(nil, []string{"/tmp/lcm-sbom-42.json"}, []string{"/var/lib/lcm/trivy"})
	spec.ScratchTmp = true
	line := argsAsString(spec, "/usr/bin/trivy", []string{"sbom", "/tmp/lcm-sbom-42.json"})

	if !strings.Contains(line, "--bind-try /var/lib/lcm/trivy /var/lib/lcm/trivy") {
		t.Error("der Trivy-Cache muss beschreibbar hineingereicht werden")
	}
	if !strings.Contains(line, "--ro-bind-try /tmp/lcm-sbom-42.json /tmp/lcm-sbom-42.json") {
		t.Error("die SBOM-Datei muss lesbar hineingereicht werden")
	}
	// Der Cache liegt INNERHALB des Datenverzeichnisses - das ist erlaubt und
	// unschädlich (es entsteht nur ein leeres Gerüst). Verboten ist, das
	// Verzeichnis SELBST einzubinden: dann lägen lcm.key und app.db offen.
	for _, verboten := range []string{
		"--ro-bind-try /var/lib/lcm /var/lib/lcm",
		"--bind-try /var/lib/lcm /var/lib/lcm",
		"/var/lib/lcm/lcm.key", "/var/lib/lcm/app.db",
	} {
		if strings.Contains(line, verboten) {
			t.Errorf("die Aufrufzeile reicht %q hinein - Master-Key bzw. Datenbank wären lesbar", verboten)
		}
	}
}

// TestBwrapEigenesTmpZuerst: Reihenfolge ist bedeutsam. Bubblewrap arbeitet
// die Optionen der Reihe nach ab - steht das --tmpfs NACH einem eingehängten
// Pfad unterhalb von /tmp, verdeckt es diesen wieder.
//
// Genau daran scheiterte der erste echte Scan: die SBOM-Datei liegt in /tmp,
// wurde eingehängt und vom nachfolgenden tmpfs unsichtbar gemacht. Der
// ursprüngliche Test prüfte nur die SCHREIBpfade und ging deshalb durch -
// deshalb deckt er jetzt lesende wie schreibende Pfade ab.
func TestBwrapEigenesTmpZuerst(t *testing.T) {
	spec := BaseSystemSpec().WithPaths(nil,
		[]string{"/tmp/lcm-sbom-1.json"}, // lesend (die SBOM-Datei)
		[]string{"/tmp/cache"})           // schreibend
	spec.ScratchTmp = true
	line := argsAsString(spec, "/bin/true", nil)

	tmpfs := strings.Index(line, "--tmpfs /tmp")
	if tmpfs < 0 {
		t.Fatalf("--tmpfs /tmp fehlt: %s", line)
	}
	for _, nach := range []string{
		"--ro-bind-try /tmp/lcm-sbom-1.json",
		"--bind-try /tmp/cache",
	} {
		pos := strings.Index(line, nach)
		if pos < 0 {
			t.Fatalf("erwartete Option %q fehlt: %s", nach, line)
		}
		if tmpfs > pos {
			t.Errorf("--tmpfs /tmp steht nach %q und verdeckt den Pfad damit", nach)
		}
	}
}

// TestBwrapProgrammStehtAmEnde: nach „--" folgt das Programm mit seinen
// Argumenten - sonst deutete Bubblewrap sie als eigene Optionen.
func TestBwrapProgrammStehtAmEnde(t *testing.T) {
	args := bwrapArgs(BaseSystemSpec(), "/usr/bin/trivy", []string{"sbom", "--quiet"})
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		t.Fatal("der Trenner -- fehlt")
	}
	got := strings.Join(args[sep+1:], " ")
	if got != "/usr/bin/trivy sbom --quiet" {
		t.Errorf("nach dem Trenner steht %q", got)
	}
}
