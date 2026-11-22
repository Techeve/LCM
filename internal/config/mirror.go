package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Mirror sind die Werte, die die config.json und die Einstellungen in der
// Datenbank gemeinsam führen (siehe cmd/app/config_sync.go). Sie stehen hier
// getrennt von Config: Config legt über jede fehlende Angabe eine Vorgabe und
// schriebe sie in die Datei zurück - ob ein Schlüssel in der Datei fehlt, muss
// die Abgleichung aber unterscheiden können (fehlt er, gilt die Datenbank).
//
// Die Schlüssel Port, AgentPort, AllowedIPs, AgentAllowedIPs, MCPAllowedIPs,
// TrustProxyHeader, TrustedProxies und SharedTLSListen gibt es auch in Config,
// der Rest nur hier.
type Mirror struct {
	AddressMode      string   `json:"address_mode"`
	PublicHost       string   `json:"public_host"`
	PublicBaseURL    string   `json:"public_base_url"`
	PublicAgentURL   string   `json:"public_agent_url"`
	PublicMCPURL     string   `json:"public_mcp_url"`
	Port             int      `json:"port"`
	AgentPort        int      `json:"agent_port"`
	MCPPort          int      `json:"mcp_port"`
	SharedTLSListen  string   `json:"shared_tls_listen"`
	AgentEnabled     bool     `json:"agent_enabled"`
	MCPEnabled       bool     `json:"mcp_enabled"`
	TrustProxyHeader bool     `json:"trust_proxy_header"`
	TrustedProxies   []string `json:"trusted_proxies"`
	AllowedIPs       []string `json:"allowed_ips"`
	AgentAllowedIPs  []string `json:"agent_allowed_ips"`
	MCPAllowedIPs    []string `json:"mcp_allowed_ips"`
}

// Values liefert jeden Schlüssel als kompaktes JSON, damit Stände schlüsselweise
// vergleichbar sind. Eine fehlende Liste zählt als leere.
func (m Mirror) Values() map[string]json.RawMessage {
	for _, list := range []*[]string{&m.TrustedProxies, &m.AllowedIPs, &m.AgentAllowedIPs, &m.MCPAllowedIPs} {
		if *list == nil {
			*list = []string{}
		}
	}
	data, _ := json.Marshal(m) // Strings, Zahlen, Bool: kann nicht scheitern
	var values map[string]json.RawMessage
	_ = json.Unmarshal(data, &values)
	return values
}

// ReadMirror liest die gespiegelten Werte aus der Datei. present nennt die
// Schlüssel, die in der Datei stehen: Ein fehlender Schlüssel ist etwas
// anderes als einer mit dem Nullwert.
func ReadMirror(path string) (m Mirror, present map[string]bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return m, nil, fmt.Errorf("config.json lesen: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, nil, fmt.Errorf("config.json parsen: %w", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return m, nil, fmt.Errorf("config.json parsen: %w", err)
	}
	present = make(map[string]bool, len(keys))
	for key := range keys {
		present[key] = true
	}
	return m, present, nil
}

// WriteMirror setzt die Schlüssel set in der Datei. Alle übrigen Schlüssel
// bleiben mit Wert und Reihenfolge, wie sie sind (auch unbekannte und die
// Geheimnisse); neue Schlüssel kommen ans Ende. Die Datei wird atomar ersetzt
// und behält ihre Rechte. Ohne Änderung bleibt sie unberührt.
func WriteMirror(path string, set map[string]json.RawMessage) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config.json lesen: %w", err)
	}
	members, err := readMembers(data)
	if err != nil {
		return fmt.Errorf("config.json parsen: %w", err)
	}
	merged, err := mergeMembers(members, set, MirrorKeys)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), merged) {
		return nil
	}
	return replaceFile(path, merged)
}

// member ist ein Schlüssel der obersten Ebene mit seinem Wert.
type member struct {
	key   string
	value json.RawMessage
}

// readMembers zerlegt die oberste Ebene eines JSON-Objekts und behält dabei
// die Reihenfolge.
func readMembers(data []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("kein JSON-Objekt")
	}
	var members []member
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		members = append(members, member{key, value})
	}
	return members, nil
}

// mergeMembers setzt die Werte aus set und schreibt das Objekt eingerückt wie
// json.MarshalIndent. Schlüssel, die es noch nicht gibt, kommen in der
// Reihenfolge order ans Ende.
func mergeMembers(members []member, set map[string]json.RawMessage, order []string) ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	written := 0
	emit := func(key string, value json.RawMessage) {
		if written > 0 {
			out.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(key)
		out.Write(keyJSON)
		out.WriteByte(':')
		out.Write(value)
		written++
	}
	done := map[string]bool{}
	for _, m := range members {
		if value, ok := set[m.key]; ok {
			emit(m.key, value)
			done[m.key] = true
			continue
		}
		emit(m.key, m.value)
	}
	for _, key := range order {
		if value, ok := set[key]; ok && !done[key] {
			emit(key, value)
		}
	}
	out.WriteByte('}')
	var indented bytes.Buffer
	if err := json.Indent(&indented, out.Bytes(), "", "  "); err != nil {
		return nil, fmt.Errorf("config.json formatieren: %w", err)
	}
	return indented.Bytes(), nil
}

// MirrorKeys nennt die gespiegelten Schlüssel in der Reihenfolge der Felder von
// Mirror; neue Schlüssel kommen in dieser Reihenfolge ans Ende der Datei.
var MirrorKeys = []string{
	"address_mode", "public_host", "public_base_url", "public_agent_url", "public_mcp_url",
	"port", "agent_port", "mcp_port", "shared_tls_listen", "agent_enabled", "mcp_enabled",
	"trust_proxy_header", "trusted_proxies", "allowed_ips", "agent_allowed_ips", "mcp_allowed_ips",
}

// replaceFile ersetzt path atomar: Die neue Fassung entsteht neben der Datei und
// kommt per Umbenennen an ihren Platz; Rechte bleiben wie sie waren. Lässt sich
// im Verzeichnis nichts anlegen, wird die Datei an Ort und Stelle überschrieben.
func replaceFile(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return os.WriteFile(path, data, mode)
	}
	name := tmp.Name()
	err = fillTemp(tmp, data, mode)
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// fillTemp schreibt die neue Fassung in die Hilfsdatei, setzt ihre Rechte und
// schließt sie.
func fillTemp(f *os.File, data []byte, mode os.FileMode) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}
