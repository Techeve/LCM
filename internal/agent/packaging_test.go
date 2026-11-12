package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// TestPaketSkripte führt packaging/scripts/agent-scripts-test.sh aus: Die
// Paket-Skripte stoppen den Agenten nur beim Entfernen und starten ihn nach
// einem Upgrade nie aus einem seiner eigenen Kommandos heraus neu - für deb,
// rpm, apk und pacman. Hier, damit es mit go test in jeder Pipeline läuft.
func TestPaketSkripte(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	out, err := exec.Command(sh, "../../packaging/scripts/agent-scripts-test.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("paket-skripte: %v\n%s", err, out)
	}
}

// TestPaketVersion prüft die Schreibweise der Version je Paketformat. Die
// erwarteten Werte sind an echtem nfpm 2.48, apk, pacman, rpm und dpkg
// nachgemessen: Jede Beta ist gültig und sortiert vor ihrem Finale.
func TestPaketVersion(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	cases := []struct{ format, in, want string }{
		{"deb", "2.0.0-beta.16", "2.0.0-beta.16"},
		{"rpm", "2.0.0-beta.16", "2.0.0-beta.16"},
		{"apk", "2.0.0-beta.16", "2.0.0-beta16"},
		{"apk", "2.0.0-rc.1", "2.0.0-rc1"},
		{"apk", "2.0.0", "2.0.0"},
		{"archlinux", "2.0.0-beta.16", "2.0.0beta.16"},
		{"archlinux", "2.0.0", "2.0.0"},
	}
	for _, c := range cases {
		out, err := exec.Command(sh, "../../packaging/pkg-version.sh", c.format, c.in).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", c.format, c.in, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != c.want {
			t.Errorf("%s %s: %q, erwartet %q", c.format, c.in, got, c.want)
		}
	}
	if err := exec.Command(sh, "../../packaging/pkg-version.sh", "snap", "2.0.0").Run(); err == nil {
		t.Error("unbekanntes Format muss scheitern")
	}
}
