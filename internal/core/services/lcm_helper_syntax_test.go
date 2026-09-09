package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestHelperSkriptIstGueltigesShell: Der Helper wird auf fremden Rechnern als
// root ausgeführt - ein Syntaxfehler in der Vorlage fiele erst dort auf,
// beim ersten privilegierten Aufruf. `sh -n` prüft ihn hier.
func TestHelperSkriptIstGueltigesShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	path := filepath.Join(t.TempDir(), "lcm-helper")
	if err := os.WriteFile(path, []byte(lcmHelperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(sh, "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
	for _, sub := range []string{"host-install)", "host-apt-cacher)", "host-crowdsec-lapi)", "host-self-update)"} {
		if !strings.Contains(lcmHelperScript, sub) {
			t.Errorf("Helper kennt %s nicht", sub)
		}
	}
	if strings.Contains(lcmHelperScript, "@@HOST_") {
		t.Error("Host-Platzhalter nicht ersetzt")
	}
}

// TestHostAktionenImEingeschraenktenModusUeberDenHelper: Auf einem
// eingeschränkten LCM-Host läuft jede Host-Aktion über ein Helper-
// Unterkommando - nie über ein Skript, das eine Root-Shell bräuchte.
func TestHostAktionenImEingeschraenktenModusUeberDenHelper(t *testing.T) {
	restricted := &domain.Server{RestrictedSudo: true}
	if got := hostScript(restricted, trivyInstallScript, "host-install", "trivy"); got != "lcm-helper host-install 'trivy'" {
		t.Errorf("eingeschränkt: %q", got)
	}
	full := &domain.Server{}
	if got := hostScript(full, trivyInstallScript, "host-install", "trivy"); got != trivyInstallScript {
		t.Error("Voll-Modus muss das Skript selbst liefern")
	}
	if got := selfUpdateScript(true); got != "lcm-helper host-self-update" {
		t.Errorf("Selbst-Update eingeschränkt: %q", got)
	}
}
