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
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	// KeyPin pinnt den öffentlichen Schlüssel des Server-Zertifikats
	// (SHA-256 über die SubjectPublicKeyInfo, hex). Der Agent übernimmt ihn
	// selbst, sobald ein Zertifikat zum CertFingerprint passt, und nimmt
	// danach auch das erneuerte Zertifikat mit demselben Schlüssel an.
	KeyPin string `json:"key_pin,omitempty"`
	// CAFingerprint pinnt die CA von LCM (SHA-256 über das CA-Zertifikat,
	// hex). Er kommt aus dem Enrollment-Token oder wird von einer bereits
	// vertrauten Verbindung gelernt (Kette des Servers). Mit ihm nimmt der
	// Agent jedes Server-Zertifikat dieser CA an, auch mit neuem Schlüssel.
	CAFingerprint string `json:"ca_fingerprint,omitempty"`
	// CustomCAFingerprints pinnen die CAs eigener Zertifikate des Betreibers
	// (Heim-CA, SHA-256 über das CA-Zertifikat, hex). Anders als die LCM-CA
	// gelten sie nur mit passendem Namen der Server-Adresse. Sie stehen
	// getrennt von CAFingerprint, damit ein eigenes Zertifikat den Anker der
	// LCM-CA nicht überschreibt (siehe learnFrom). Höchstens
	// MaxCustomCAPins, die ältesten fallen heraus.
	CustomCAFingerprints []string `json:"custom_ca_fingerprints,omitempty"`
	// LogLevel ist die Stufe des Journals: debug, info oder warn (leer = info).
	// Der Schalter --log-level beim Start geht vor. Trennungen, Wiederverbindungen
	// und der Wechsel des Binarys stehen auch auf warn im Journal.
	LogLevel string `json:"log_level,omitempty"`
}

// ParseLogLevel liest eine Stufe des Journals: debug, info, warn (Groß- und
// Kleinschreibung egal, leer = info).
func ParseLogLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	}
	return slog.LevelInfo, fmt.Errorf("unknown log level %q (debug, info or warn)", name)
}

// ResolveLogLevel bestimmt die Stufe: Der Schalter geht vor der Konfiguration. Eine
// unbrauchbare Angabe bricht den Dienst nicht ab (er ist oft der einzige Weg auf
// den Server), sie fällt auf info zurück; problem sagt, was war.
func ResolveLogLevel(flagValue, configValue string) (level slog.Level, problem error) {
	name := configValue
	if flagValue != "" {
		name = flagValue
	}
	level, problem = ParseLogLevel(name)
	return level, problem
}

// MaxCustomCAPins ist die Obergrenze der gepinnten CAs eigener Zertifikate.
const MaxCustomCAPins = 4

// rememberCustomCA nimmt die CA in die Liste auf, am Ende; ist sie schon
// drin, ändert sich nichts. Gemeldet wird, ob sich die Liste geändert hat.
func (c *Config) rememberCustomCA(fingerprint string) bool {
	if slices.Contains(c.CustomCAFingerprints, fingerprint) {
		return false
	}
	c.CustomCAFingerprints = append(c.CustomCAFingerprints, fingerprint)
	if extra := len(c.CustomCAFingerprints) - MaxCustomCAPins; extra > 0 {
		c.CustomCAFingerprints = c.CustomCAFingerprints[extra:]
	}
	return true
}

// TerminalOffName ist die Datei im Konfigurationsverzeichnis, deren Dasein die
// Web-Konsole über diesen Agent abschaltet (lcm-agent terminal off). Der
// Betreiber des Servers entscheidet damit selbst, ob LCM hier eine root-Shell
// öffnen darf; Standard ist an.
//
// Bewusst eine eigene Datei statt eines Felds in agent.json: Der Agent schreibt
// agent.json selbst, wenn er neue Zertifikats-Pins lernt, und hätte dabei einen
// zwischenzeitlich gesetzten Schalter mit seinem alten Stand überschrieben.
// Anlegen und Löschen einer Datei geschehen außerdem in einem Schritt - es gibt
// keinen halb geschriebenen Zustand.
const TerminalOffName = "terminal-off"

// TerminalDisabled meldet, ob die Konsole im Verzeichnis dir abgeschaltet ist.
// Lässt sich das nicht feststellen, gilt sie als abgeschaltet: Ein Schalter,
// der eine root-Shell sperrt, darf im Zweifel nicht aufgehen.
func TerminalDisabled(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, TerminalOffName))
	return !errors.Is(err, os.ErrNotExist)
}

// SetTerminal schaltet die Web-Konsole an oder ab. Der laufende Agent prüft den
// Schalter bei jedem Öffnen und jeder Eingabe, die Änderung gilt also sofort,
// auch für offene Konsolen; LCM erfährt sie beim nächsten Verbindungsaufbau.
func SetTerminal(dir string, enabled bool) error {
	path := filepath.Join(dir, TerminalOffName)
	if enabled {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte("Web-Konsole über lcm-agent abgeschaltet (lcm-agent terminal on schaltet sie wieder ein)\n"), 0o600)
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
// enthält das Secret). Das Schreiben ist atomar (writeFileDurable): Bricht es
// ab, bleibt die alte Datei, nie eine halbe.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileDurable(path, append(raw, '\n'), 0o600)
}

// writeTemp schreibt den Inhalt in die Zwischendatei. Eine Variable, damit ein
// Test einen Abbruch mitten im Schreiben einspielen kann.
var writeTemp = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// writeFileDurable ersetzt die Datei in einem Schritt: Zwischendatei im selben
// Verzeichnis, schreiben, fsync, rename, fsync des Verzeichnisses. Fällt der
// Strom irgendwo aus, liegt dort entweder der alte oder der neue Inhalt. Die
// Rechte der bestehenden Datei bleiben, eine neue bekommt newMode.
func writeFileDurable(path string, data []byte, newMode os.FileMode) error {
	mode := newMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if err := fillTemp(tmp, data, mode); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return syncDir(dir)
}

// fillTemp setzt die Rechte, schreibt, sichert auf den Datenträger und schließt.
func fillTemp(tmp *os.File, data []byte, mode os.FileMode) error {
	err := tmp.Chmod(mode)
	if err == nil {
		err = writeTemp(tmp, data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	return err
}

// syncDir sichert den Verzeichniseintrag der umbenannten Datei.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	return err
}

// WarnLooseMode schreibt eine Warnung ins Journal, wenn Gruppe oder andere die
// Konfiguration lesen können: Sie enthält das Secret des Agents. Die Rechte
// werden nicht von selbst geändert - wer sie gesetzt hat, soll es wissen.
func WarnLooseMode(path string, log *slog.Logger) {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o077 == 0 {
		return
	}
	log.Warn("agent config is readable by group or others - it holds the connection secret, run chmod 600",
		"path", path, "mode", fmt.Sprintf("%04o", info.Mode().Perm()))
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
	if u.Hostname() == "" {
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
