// Package agent implementiert den lcm-agent (LCM Remote): ein schlanker
// Dienst auf dem verwalteten Server, der sich AUSGEHEND per MQTT-über-
// WebSocket mit dem LCM-Server verbindet und dessen Shell-Kommandos
// ausführt - für Server hinter NAT, ohne feste IP oder unterwegs.
//
// Bewusst minimal gehalten: stdlib + paho-MQTT + das geteilte wire-Paket.
// Weder GORM noch Fiber noch mochi landen im Agent-Binary.
package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"LCM/internal/remote/wire"
)

// Ablageorte auf dem Zielsystem.
const (
	ConfigDir  = "/etc/lcm-agent"
	ConfigPath = ConfigDir + "/agent.json"
)

// Config ist die beim Enrollment geschriebene Agent-Konfiguration
// (0600, root - enthält das Verbindungs-Secret).
type Config struct {
	// URL ist die Basis-Adresse des LCM-Servers - dieselbe wie im Browser,
	// z.B. "https://lcm.example.com:8443".
	URL string `json:"url"`
	// AgentID/Secret sind die MQTT-Zugangsdaten aus dem Enrollment-Token.
	AgentID string `json:"agent_id"`
	Secret  string `json:"secret"`
	// CertFingerprint pinnt das Server-Zertifikat (SHA-256, hex; leer =
	// nur Systemroot-Prüfung, z.B. --dev oder CA-signiertes Zertifikat).
	CertFingerprint string `json:"cert_fingerprint,omitempty"`
}

// LoadConfig liest die Enrollment-Konfiguration.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("%s lesen: %w", path, err)
	}
	if cfg.URL == "" || cfg.AgentID == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("%s unvollständig (url/agent_id/secret)", path)
	}
	return &cfg, nil
}

// Save schreibt die Konfiguration (Verzeichnis 0700, Datei 0600 - sie
// enthält das Secret).
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// NormalizeServerURL ergänzt ein fehlendes Schema (https) und entfernt
// Pfad/Slash am Ende - akzeptiert also auch "lcm.example.com:8443".
func NormalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("leere server-adresse")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("server-adresse %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("server-adresse %q: nur http(s) erlaubt", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("server-adresse %q: host fehlt", raw)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return u.String(), nil
}

// BrokerURL leitet die MQTT-WebSocket-Adresse aus der Server-URL ab
// (https → wss, http → ws; Pfad /mqtt).
func (c *Config) BrokerURL() (string, error) {
	u, err := url.Parse(c.URL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unerwartetes schema %q", u.Scheme)
	}
	u.Path = wire.WSPath
	return u.String(), nil
}
