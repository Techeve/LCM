package main

import "testing"

// TestCredentialPathsStandard: Ohne Angabe gilt die Paket-Installation - nicht
// das Verzeichnis der Programmdatei.
func TestCredentialPathsStandard(t *testing.T) {
	t.Setenv("LCM_DATA", "")
	cfg, data := credentialPaths("", "")
	if cfg != "/etc/lcm/config.json" || data != "/var/lib/lcm" {
		t.Errorf("standard: config=%q data=%q", cfg, data)
	}
}

// TestCredentialPathsExplicit: Angaben des Benutzers gewinnen.
func TestCredentialPathsExplicit(t *testing.T) {
	t.Setenv("LCM_DATA", "")
	cfg, data := credentialPaths("/tmp/c.json", "/tmp/d")
	if cfg != "/tmp/c.json" || data != "/tmp/d" {
		t.Errorf("explizit: config=%q data=%q", cfg, data)
	}
	t.Setenv("LCM_DATA", "/srv/lcm")
	cfg, data = credentialPaths("", "")
	if cfg != "" || data != "" {
		t.Errorf("LCM_DATA muss der Standard bleiben: config=%q data=%q", cfg, data)
	}
}
