package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/config"
	"LCM/internal/netfilter"
)

func TestLoadFromCreatesSecureDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9310 || cfg.AccessTokenTTLMinutes != 60 {
		t.Errorf("unerwartete Vorgaben: port=%d ttl=%d", cfg.Port, cfg.AccessTokenTTLMinutes)
	}

	// Die Datei enthält kein Geheimnis mehr - und bleibt trotzdem restriktiv,
	// weil ein Betreiber dort welche eintragen kann (Trivy-Token).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "jwt_secret") {
		t.Error("eine neue config.json darf kein jwt_secret mehr tragen")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.json sollte 0600 sein, ist %o", perm)
	}

	// Zweites Laden liefert dieselben Werte.
	cfg2, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Port != cfg.Port {
		t.Error("Konfiguration hat sich beim erneuten Laden geändert")
	}
}

// TestLoadFromDropsLegacyJWTSecret: Ältere Dateien tragen ein jwt_secret.
// Es hat keine Funktion mehr - der Signaturschlüssel entsteht pro Start im
// Speicher - und darf weder das Laden stören noch stehen bleiben.
func TestLoadFromDropsLegacyJWTSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"jwt_secret":"kurz","port":8080,"access_token_ttl_minutes":60}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("altes jwt_secret darf das Laden nicht stören: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("port = %d", cfg.Port)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "jwt_secret") {
		t.Error("jwt_secret muss beim Nachtragen der Optionen aus der Datei verschwinden")
	}
}

func TestRandomSecretIsUnique(t *testing.T) {
	// Zwei getrennte Aufrufe müssen (kryptografisch zufällig) verschieden sein.
	first, second := config.RandomSecret(32), config.RandomSecret(32)
	if first == second {
		t.Fatal("RandomSecret liefert identische Werte")
	}
}

// Ein Tippfehler in einem gespiegelten Wert hält LCM nicht vom Start ab: Die
// Übernahme in die Einstellungen prüft ihn (cmd/app/config_sync.go) und lässt
// den Wert der Datenbank gelten.
func TestLoadFromToleratesInvalidMirroredValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"port":8080,"access_token_ttl_minutes":60,"log_level":"info",` +
		`"allowed_ips":["localhost","nicht-eine-ip"],"trusted_proxies":["nicht-ip"],` +
		`"agent_port":8080,"shared_tls_listen":"lcm.example.org"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFrom(path); err != nil {
		t.Fatalf("Start wegen eines gespiegelten Werts abgebrochen: %v", err)
	}
}

func TestLoadFromAcceptsAllowedIPs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"port":8080,"access_token_ttl_minutes":60,` +
		`"log_level":"info","allowed_ips":["localhost","192.168.0.0/16"],"trust_proxy_header":true}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("gültige allowed_ips abgelehnt: %v", err)
	}
	list, err := netfilter.Parse(cfg.AllowedIPs)
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

// agent_allowed_ips und mcp_allowed_ips stehen je in ihrem Feld.
func TestLoadFromAgentAndMCPNets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"port":8080,"access_token_ttl_minutes":60,"log_level":"info",` +
		`"agent_allowed_ips":["10.0.0.0/8"],"mcp_allowed_ips":["127.0.0.1"]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedIPs) != 0 || strings.Join(cfg.AgentAllowedIPs, ",") != "10.0.0.0/8" ||
		strings.Join(cfg.MCPAllowedIPs, ",") != "127.0.0.1" {
		t.Errorf("Listen: ui=%v agent=%v mcp=%v", cfg.AllowedIPs, cfg.AgentAllowedIPs, cfg.MCPAllowedIPs)
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
	body := `{"port":8080,"access_token_ttl_minutes":60,` +
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
	for _, key := range []string{"log_file", "allowed_ips", "trust_proxy_header", "trivy_path", "tls_certificates"} {
		if !strings.Contains(string(written), `"`+key+`"`) {
			t.Errorf("Schlüssel %q wurde nicht nachgetragen:\n%s", key, written)
		}
	}
	if strings.Contains(string(written), `"host": "127.0.0.1"`) {
		t.Error("der eigene host-Wert wurde durch den Default ersetzt")
	}
}

// TestTLSCAFromConfig: tls_ca_cert und tls_ca_key nur zusammen und absolut.
func TestTLSCAFromConfig(t *testing.T) {
	cases := map[string]bool{
		`{"tls_ca_cert":"/etc/lcm/ca.pem","tls_ca_key":"/etc/lcm/ca-key.pem"}`: true,
		`{"tls_ca_cert":"/etc/lcm/ca.pem"}`:                                    false,
		`{"tls_ca_cert":"ca.pem","tls_ca_key":"ca-key.pem"}`:                   false,
	}
	for body, ok := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.LoadFrom(path)
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, erwartet ok=%v", body, err, ok)
		}
	}
}

// TestTLSCertificatesFromConfig: je Eintrag Zertifikat und Schlüssel, absolute
// Pfade, kein Zertifikat doppelt.
func TestTLSCertificatesFromConfig(t *testing.T) {
	cases := map[string]bool{
		`{}`:                      true,
		`{"tls_certificates":[]}`: true,
		`{"tls_certificates":[{"cert":"/etc/lcm/a.pem","key":"/etc/lcm/a.key"},{"cert":"/etc/lcm/b.pem","key":"/etc/lcm/b.key"}]}`:        true,
		`{"tls_certificates":[{"cert":"/etc/lcm/a.pem"}]}`:                                                                                false,
		`{"tls_certificates":[{"key":"/etc/lcm/a.key"}]}`:                                                                                 false,
		`{"tls_certificates":[{"cert":"a.pem","key":"/etc/lcm/a.key"}]}`:                                                                  false,
		`{"tls_certificates":[{"cert":"/etc/lcm/a.pem","key":"a.key"}]}`:                                                                  false,
		`{"tls_certificates":[{"cert":"/etc/lcm/a.pem","key":"/etc/lcm/a.key"},{"cert":"/etc/lcm/../lcm/a.pem","key":"/etc/lcm/b.key"}]}`: false,
	}
	for body, ok := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.LoadFrom(path)
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, erwartet ok=%v", body, err, ok)
		}
		if err != nil && !strings.Contains(err.Error(), "tls_certificates") {
			t.Errorf("%s: Fehler nennt den Schlüssel nicht: %v", body, err)
		}
	}
}

// TestLoadTLSCertificatesLiestNurDenAbschnitt: Ein Fehler anderswo in der
// Datei stört nicht, ein ungültiger Abschnitt schon, und die Datei bleibt
// unangetastet (LoadFrom würde sie um neue Optionen ergänzen).
func TestLoadTLSCertificatesLiestNurDenAbschnitt(t *testing.T) {
	cases := []struct {
		body string
		want int // erwartete Einträge, -1 = Fehler
	}{
		{`{"port":-1,"tls_certificates":[{"cert":"/a.pem","key":"/a.key"}]}`, 1},
		{`{}`, 0},
		{`{"tls_certificates":[{"cert":"a.pem","key":"/a.key"}]}`, -1},
		{`{"tls_certificates":`, -1},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		files, err := config.LoadTLSCertificates(path)
		if (c.want < 0) != (err != nil) || len(files) != max(c.want, 0) {
			t.Errorf("%s: %d Einträge, err=%v, erwartet %d", c.body, len(files), err, c.want)
		}
		if got, _ := os.ReadFile(path); string(got) != c.body {
			t.Errorf("%s: Datei wurde verändert: %s", c.body, got)
		}
	}
	if _, err := config.LoadTLSCertificates(filepath.Join(t.TempDir(), "fehlt.json")); err == nil {
		t.Error("fehlende Datei: erwartet einen Fehler")
	}
}

// TestTLSCertificatesDefaultIsEmptyListInFile: Ohne den Schlüssel steht
// danach "tls_certificates": [] in der Datei, nicht null.
func TestTLSCertificatesDefaultIsEmptyListInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"port":9090}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSCertificates == nil || len(cfg.TLSCertificates) != 0 {
		t.Errorf("TLSCertificates = %#v, erwartet leere Liste", cfg.TLSCertificates)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"tls_certificates": []`) {
		t.Errorf("Schlüssel nicht als leere Liste nachgetragen:\n%s", written)
	}
}

// TestSharedTLSListenDefaultStehtInDerDatei: Der Schlüssel ist in der Datei zu
// finden, auch wenn er leer ist.
func TestSharedTLSListenDefaultStehtInDerDatei(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := config.LoadFrom(path); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"shared_tls_listen": ""`) {
		t.Errorf("Schlüssel fehlt in der neuen config.json:\n%s", written)
	}
}
