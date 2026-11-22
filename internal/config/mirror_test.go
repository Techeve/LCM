package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/config"
)

func writeConfig(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// MirrorKeys und die Felder von Mirror nennen dieselben Schlüssel.
func TestMirrorKeysMatchFields(t *testing.T) {
	values := config.Mirror{}.Values()
	if len(values) != len(config.MirrorKeys) {
		t.Fatalf("%d Felder, %d Schlüssel", len(values), len(config.MirrorKeys))
	}
	for _, key := range config.MirrorKeys {
		if _, ok := values[key]; !ok {
			t.Errorf("Schlüssel %q ist kein Feld von Mirror", key)
		}
	}
	if string(values["trusted_proxies"]) != "[]" {
		t.Errorf("fehlende Liste = %s, erwartet []", values["trusted_proxies"])
	}
}

// Beim Schreiben bleiben alle anderen Schlüssel mit Wert und Reihenfolge, auch
// unbekannte und die Geheimnisse; neue kommen ans Ende; die Rechte bleiben.
func TestWriteMirrorKeepsOtherKeys(t *testing.T) {
	path := writeConfig(t, `{
  "zeta_unbekannt": {"a": [1, 2]},
  "port": 9310,
  "tls_ca_key": "/etc/lcm/ca-key.pem",
  "admin_initial_password": "geheim",
  "agent_port": 9320
}`, 0o640)

	err := config.WriteMirror(path, map[string]json.RawMessage{
		"port":         json.RawMessage(`9400`),
		"address_mode": json.RawMessage(`"one_name"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := `{
  "zeta_unbekannt": {
    "a": [
      1,
      2
    ]
  },
  "port": 9400,
  "tls_ca_key": "/etc/lcm/ca-key.pem",
  "admin_initial_password": "geheim",
  "agent_port": 9320,
  "address_mode": "one_name"
}`
	if string(got) != want {
		t.Errorf("Datei:\n%s\nerwartet:\n%s", got, want)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("Rechte = %v, erwartet 0640", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("Hilfsdatei blieb liegen: %v", entries)
	}
}

// Ohne Änderung bleibt die Datei unberührt.
func TestWriteMirrorWithoutChangeLeavesFile(t *testing.T) {
	path := writeConfig(t, "{\n  \"port\": 9310\n}", 0o600)
	before, _ := os.Stat(path)
	if err := config.WriteMirror(path, map[string]json.RawMessage{"port": json.RawMessage(`9310`)}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("Datei wurde ohne Änderung neu geschrieben")
	}
}

func TestReadMirrorReportsPresentKeys(t *testing.T) {
	path := writeConfig(t, `{"port": 9400, "agent_enabled": false, "trusted_proxies": ["10.0.0.5"]}`, 0o600)
	m, present, err := config.ReadMirror(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Port != 9400 || m.AgentEnabled || len(m.TrustedProxies) != 1 {
		t.Errorf("Werte = %+v", m)
	}
	if !present["agent_enabled"] || present["mcp_enabled"] {
		t.Errorf("present = %v: false muss vorhanden, ein fehlender Schlüssel fehlen", present)
	}
}

// LoadFrom trägt nur fehlende Schlüssel nach und lässt die gespiegelten
// Schlüssel, die Config nicht kennt, in der Datei stehen.
func TestLoadFromKeepsMirrorKeysAndUnknownKeys(t *testing.T) {
	path := writeConfig(t, `{
  "host": "0.0.0.0",
  "port": 9090,
  "access_token_ttl_minutes": 60,
  "log_level": "info",
  "address_mode": "shared_port",
  "mcp_enabled": true,
  "notiz": "bleibt"
}`, 0o600)
	if _, err := config.LoadFrom(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{`"address_mode": "shared_port"`, `"mcp_enabled": true`, `"notiz": "bleibt"`, `"trusted_proxies": []`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%s fehlt in:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), `"agent_enabled"`) {
		t.Errorf("agent_enabled gehört Mirror, nicht Config - LoadFrom darf ihn nicht erfinden:\n%s", got)
	}
}
