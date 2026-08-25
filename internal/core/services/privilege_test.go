package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestWrapSudoModes: der Voll-Modus wrappt in `sudo sh -c`, der eingeschränkte
// Modus läuft als der Service-User über den Shim-PATH - ohne `sudo sh` (das
// wäre eine Root-Shell und würde die Whitelist aushebeln).
func TestWrapSudoModes(t *testing.T) {
	full := wrapSudo("lcm-svc", false, "apt-get update")
	if !strings.Contains(full, "sudo sh -c") {
		t.Errorf("voll-modus sollte über sudo sh -c laufen: %s", full)
	}

	restricted := wrapSudo("lcm-svc", true, "apt-get update")
	if strings.Contains(restricted, "sudo sh -c") || strings.Contains(restricted, "sudo -n sh") {
		t.Errorf("eingeschränkter modus darf keine root-shell öffnen: %s", restricted)
	}
	if !strings.Contains(restricted, sudoShimDir("lcm-svc")) {
		t.Errorf("eingeschränkter modus muss den shim-PATH nutzen: %s", restricted)
	}
	// Beide entkoppeln die Hintergrund-fds (temp-Datei), damit apt-Hooks den
	// SSH-Kanal nicht offenhalten.
	if !strings.Contains(full, "mktemp") || !strings.Contains(restricted, "mktemp") {
		t.Error("erwartete fd-Entkopplung über temp-Datei in beiden Modi")
	}
}

// TestRestrictedSudoersIsWhitelist: die eingeschränkte sudoers-Datei erlaubt nur
// die Whitelist-Binaries und niemals NOPASSWD:ALL oder eine Shell.
func TestRestrictedSudoersIsWhitelist(t *testing.T) {
	content := restrictedSudoersContent("lcm-svc")
	if strings.Contains(content, "NOPASSWD:ALL") || strings.Contains(content, "ALL=(ALL) NOPASSWD: ALL") {
		t.Errorf("eingeschränkte sudoers darf kein NOPASSWD:ALL enthalten:\n%s", content)
	}
	for _, want := range []string{"/usr/bin/apt-get", "/usr/bin/docker", "ufw", "NOPASSWD: LCM_ALLOWED"} {
		if !strings.Contains(content, want) {
			t.Errorf("erwartete %q in der whitelist:\n%s", want, content)
		}
	}
	// Keine Shell/tee/systemctl in der Whitelist.
	for _, forbidden := range []string{"/bin/sh", "/bin/bash", "/usr/bin/tee", "systemctl"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("whitelist darf %q NICHT enthalten:\n%s", forbidden, content)
		}
	}
}

// TestEnsureFullSudo: der Guard weist eingeschränkte Server ab, Voll-Server nicht.
func TestEnsureFullSudo(t *testing.T) {
	if err := ensureFullSudo(&domain.Server{RestrictedSudo: false}); err != nil {
		t.Errorf("voll-server sollte kein guard-fehler sein: %v", err)
	}
	if err := ensureFullSudo(&domain.Server{RestrictedSudo: true}); err != ErrRestrictedSudo {
		t.Errorf("eingeschränkter server sollte ErrRestrictedSudo liefern, bekam: %v", err)
	}
}

// TestRestrictedAllowsRule: Paketverwaltung/Docker/Firewall/Health sowie
// Sync und apt-proxy (beide über den LCM-Helper) sind erlaubt; beliebige
// Skripte, Custom-Aktionen und Reboot nicht.
func TestRestrictedAllowsRule(t *testing.T) {
	allowed := []string{domain.RuleTypeUpdate, domain.RuleTypePackages, domain.RuleTypeSecurity,
		domain.RuleTypeDockerPrune, domain.RuleTypeFirewall, domain.RuleTypeHealth,
		domain.RuleTypeSync, domain.RuleTypeAptProxy}
	for _, rt := range allowed {
		if !restrictedAllowsRule(rt) {
			t.Errorf("%q sollte im eingeschränkten modus erlaubt sein", rt)
		}
	}
	blocked := []string{domain.RuleTypeScript, domain.RuleTypeCustom, domain.RuleTypeReboot}
	for _, rt := range blocked {
		if restrictedAllowsRule(rt) {
			t.Errorf("%q sollte im eingeschränkten modus gesperrt sein", rt)
		}
	}
}
