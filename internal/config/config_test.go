package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/config"
)

func TestLoadFromCreatesSecureDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.JWTSecret) < 32 {
		t.Errorf("generiertes JWT-Secret zu kurz: %d Zeichen", len(cfg.JWTSecret))
	}

	// Datei existiert mit restriktiven Rechten (enthält Secrets).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.json sollte 0600 sein, ist %o", perm)
	}

	// Zweites Laden liefert dieselben Werte (kein Neu-Generieren).
	cfg2, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.JWTSecret != cfg.JWTSecret {
		t.Error("JWT-Secret hat sich beim erneuten Laden geändert")
	}
}

func TestLoadFromRejectsWeakSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"jwt_secret":"kurz","port":8080,"access_token_ttl_minutes":60}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFrom(path); err == nil {
		t.Fatal("schwaches jwt_secret wurde akzeptiert")
	}
}

func TestRandomSecretIsUnique(t *testing.T) {
	// Zwei getrennte Aufrufe müssen (kryptografisch zufällig) verschieden sein.
	first, second := config.RandomSecret(32), config.RandomSecret(32)
	if first == second {
		t.Fatal("RandomSecret liefert identische Werte")
	}
}

func TestLoadFromRejectsInvalidAllowedIPs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	secret := config.RandomSecret(48)
	body := `{"jwt_secret":"` + secret + `","port":8080,"access_token_ttl_minutes":60,` +
		`"log_level":"info","allowed_ips":["localhost","nicht-eine-ip"]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFrom(path); err == nil {
		t.Fatal("ungültiger allowed_ips-Eintrag wurde akzeptiert")
	}
}

func TestLoadFromAcceptsAllowedIPs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	secret := config.RandomSecret(48)
	body := `{"jwt_secret":"` + secret + `","port":8080,"access_token_ttl_minutes":60,` +
		`"log_level":"info","allowed_ips":["localhost","192.168.0.0/16"],"trust_proxy_header":true}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("gültige allowed_ips abgelehnt: %v", err)
	}
	list, err := cfg.IPAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	if list.IsEmpty() {
		t.Fatal("Allowlist sollte nicht leer sein")
	}
	if !cfg.TrustProxyHeader {
		t.Error("trust_proxy_header sollte true sein")
	}
}

// TestAgentListenerDefaults: eine frisch erzeugte config.json aktiviert den
// dedizierten Agent-Listener (LCM Remote) auf 0.0.0.0:9320.
func TestAgentListenerDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentPort != 9320 {
		t.Errorf("agent_port-Default = %d, erwartet 9320", cfg.AgentPort)
	}
	if cfg.AgentHost != "0.0.0.0" {
		t.Errorf("agent_host-Default = %q, erwartet 0.0.0.0", cfg.AgentHost)
	}
	if got := cfg.AgentAddress(); got != "0.0.0.0:9320" {
		t.Errorf("AgentAddress() = %q, erwartet 0.0.0.0:9320", got)
	}
	if !cfg.AgentListenerEnabled() {
		t.Error("AgentListenerEnabled() sollte bei Default true sein")
	}
}

// TestAgentAddressFallsBackToAllInterfaces: ohne agent_host bindet der
// Agent-Listener an 0.0.0.0 (er muss von außen erreichbar sein).
func TestAgentAddressFallsBackToAllInterfaces(t *testing.T) {
	cfg := &config.Config{AgentPort: 9320}
	if got := cfg.AgentAddress(); got != "0.0.0.0:9320" {
		t.Errorf("AgentAddress() = %q, erwartet 0.0.0.0:9320", got)
	}
}

// TestAgentPortZeroDisablesListener: agent_port = 0 schaltet den Agent-
// Listener ab (kein Remote-Transport) und ist eine gültige Konfiguration.
func TestAgentPortZeroDisablesListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	secret := config.RandomSecret(48)
	body := `{"jwt_secret":"` + secret + `","port":8080,"access_token_ttl_minutes":60,` +
		`"log_level":"info","agent_port":0}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("agent_port:0 sollte gültig sein: %v", err)
	}
	if cfg.AgentListenerEnabled() {
		t.Error("AgentListenerEnabled() sollte bei agent_port:0 false sein")
	}
}

// TestAgentPortCollisionRejected: der Agent-Port darf nicht mit dem UI/REST-
// Port kollidieren - sonst können die beiden Listener nicht gleichzeitig binden.
func TestAgentPortCollisionRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	secret := config.RandomSecret(48)
	body := `{"jwt_secret":"` + secret + `","port":8080,"access_token_ttl_minutes":60,` +
		`"log_level":"info","agent_port":8080}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFrom(path); err == nil {
		t.Fatal("agent_port == port wurde akzeptiert")
	}
}

// TestLoadFromAddsNewKeysToExistingFile: Beim Upgrade tauchen neu
// hinzugekommene Optionen in einer BESTEHENDEN config.json nicht auf - der
// Anwender müsste ihren Namen aus der Doku raten, statt ihn beim Öffnen der
// Datei zu sehen (BUG-003). Genau so fehlte auf dem Testsystem log_file.
func TestLoadFromAddsNewKeysToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// Eine "alte" Datei: gültig, aber ohne die später ergänzten Schlüssel.
	old := `{
  "host": "0.0.0.0",
  "port": 9090,
  "database_path": "app.db",
  "jwt_secret": "` + config.RandomSecret(48) + `",
  "access_token_ttl_minutes": 60,
  "log_level": "info"
}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	// Bestehende Werte bleiben unangetastet.
	if cfg.Host != "0.0.0.0" || cfg.Port != 9090 {
		t.Errorf("bestehende Werte überschrieben: host=%q port=%d", cfg.Host, cfg.Port)
	}

	// Und die Datei nennt die neuen Optionen jetzt beim Namen.
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"log_file", "allowed_ips", "trust_proxy_header", "trivy_path"} {
		if !strings.Contains(string(written), `"`+key+`"`) {
			t.Errorf("Schlüssel %q wurde nicht nachgetragen:\n%s", key, written)
		}
	}
	if strings.Contains(string(written), `"host": "127.0.0.1"`) {
		t.Error("der eigene host-Wert wurde durch den Default ersetzt")
	}
}
