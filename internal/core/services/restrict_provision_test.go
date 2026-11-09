package services

import (
	"os"
	"strings"
	"testing"
)

// Der Helper des LCM-Hosts wird beim Paket-Update erneuert (lcm restricted-provision).

// TestRestrictedProvisionScript: das Skript für den LCM-Host nennt Helper und
// Whitelist, prüft per visudo und nimmt nur gültige Benutzernamen.
func TestRestrictedProvisionScript(t *testing.T) {
	script, err := RestrictedProvisionScript("lcm-svc")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{lcmHelperPath + ".tmp", "visudo -cf /etc/sudoers.d/lcm-svc.tmp", "mv /etc/sudoers.d/lcm-svc.tmp /etc/sudoers.d/lcm-svc", "/home/lcm-svc/.lcm/sudo-bin"} {
		if !strings.Contains(script, want) {
			t.Errorf("Skript enthält %q nicht", want)
		}
	}
	if strings.Contains(script, "NOPASSWD:ALL") {
		t.Error("das Skript darf dem Benutzer nie volle Rechte geben")
	}
	if len(strings.Split(strings.TrimSpace(script), "\n")) < 5 {
		t.Error("ein Schritt je Zeile erwartet")
	}
	for _, bad := range []string{"", "Root", "a;b", "a b", "-x", strings.Repeat("a", 40), "$(id)"} {
		if _, err := RestrictedProvisionScript(bad); err == nil {
			t.Errorf("Benutzername %q wurde angenommen", bad)
		}
	}
}

// TestPostinstallErneuertNurEingeschraenktenHelper: Das Paket erneuert den
// Helper nur, wenn der Host wirklich eingeschränkt ist (Helper da, keine
// NOPASSWD:ALL-Zeile), und ein Fehlschlag bricht die Installation nicht ab.
func TestPostinstallErneuertNurEingeschraenktenHelper(t *testing.T) {
	b, err := os.ReadFile("../../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	i := strings.Index(script, "restricted-provision")
	if i < 0 {
		t.Fatal("postinstall.sh ruft restricted-provision nicht auf")
	}
	block := script[strings.LastIndex(script[:i], "SUDOERS_FILE="):]
	block = block[:strings.Index(block, "# 4. Register")]
	for _, want := range []string{"-x /usr/local/sbin/lcm-helper", "! grep -Eq 'NOPASSWD:[[:space:]]*ALL'", `"$SUDOERS_FILE"`, "sh -e"} {
		if !strings.Contains(block, want) {
			t.Errorf("Block enthält %q nicht:\n%s", want, block)
		}
	}
	// Unter `set -e` darf der Aufruf nur in einer if-Bedingung stehen.
	if !strings.Contains(block, "if PROVISION=$(mktemp) && /usr/bin/lcm restricted-provision") {
		t.Error("der Aufruf steht nicht in einer if-Bedingung - ein Fehlschlag bräche die Installation ab")
	}
	if !strings.Contains(script, `"${HELPER_REFRESH:-}" = "failed"`) {
		t.Error("ein Fehlschlag wird nicht gemeldet")
	}
}
