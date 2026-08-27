// Package config verwaltet die Anwendungskonfiguration (config.json).
//
// Beim Start wird nach einer config.json im Verzeichnis des Binaries gesucht.
// Fehlt sie, wird sie mit sicheren, randomisierten Standardwerten erzeugt
// (inkl. kryptografisch starkem JWT-Secret).
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"LCM/internal/i18n"
	"LCM/internal/netfilter"
)

// Config enthält alle Laufzeit-Einstellungen der Anwendung.
type Config struct {
	// Host und Port des HTTP-Servers (UI + REST-API).
	Host string `json:"host"`
	Port int    `json:"port"`

	// AgentHost/AgentPort ist der DEDIZIERTE Listener für die lcm-agent-
	// Kommunikation (LCM Remote, MQTT-über-WebSocket). Bewusst getrennt vom
	// UI/REST-Port: dort läuft ausschließlich der /mqtt-Endpunkt, keine UI
	// und keine REST-API - und umgekehrt bietet der UI/REST-Port keine
	// Agent-Schnittstelle. Da sich die Agents von außen (hinter NAT) eingehend
	// verbinden, bindet dieser Listener standardmäßig an alle Interfaces
	// (0.0.0.0), während die UI auf ihrer eigenen Bind-Adresse bleibt.
	// AgentPort = 0 schaltet den Agent-Listener ab (kein Remote-Transport).
	AgentHost string `json:"agent_host"`
	AgentPort int    `json:"agent_port"`

	// DatabasePath ist der Pfad zur SQLite-Datei (relativ zum Binary oder absolut).
	DatabasePath string `json:"database_path"`

	// JWTSecret signiert die Access-Tokens (HS256). Wird bei Erstellung
	// der Datei kryptografisch zufällig generiert. Niemals einchecken!
	JWTSecret string `json:"jwt_secret"`

	// AccessTokenTTLMinutes bestimmt die Lebensdauer eines JWT in Minuten.
	AccessTokenTTLMinutes int `json:"access_token_ttl_minutes"`

	// AdminInitialPassword wird nur beim allerersten Seeding verwendet.
	// Leerer String => es wird ein zufälliges Passwort generiert und
	// einmalig auf stdout ausgegeben.
	AdminInitialPassword string `json:"admin_initial_password"`

	// DemoMode aktiviert die Demo-Testdaten (Server, Pakete, Job-Historien)
	// beim ERSTEN Seeding einer frischen Datenbank. Ausschließlich über das
	// CLI-Flag --demo steuerbar und bewusst NICHT in der config.json
	// persistiert (json:"-") - sonst würde eine alte oder aus einem Backup
	// wiederhergestellte Config mit `demo_mode: true` eine frische
	// Datenbank unbeabsichtigt mit Demodaten befüllen.
	DemoMode bool `json:"-"`

	// DemoPublic härtet den Demo-Modus für eine ÖFFENTLICHE Demo-Instanz
	// (CLI-Flag --demo-public, impliziert --demo): Die Login-Seite zeigt die
	// Demo-Zugänge an, und Endpunkte, die Zugänge verändern, Daten nach außen
	// tragen oder ausgehende Verbindungen zu Besucher-Zielen aufbauen könnten,
	// sind gesperrt (siehe middlewares.DemoGuard). Wie DemoMode bewusst nicht
	// persistiert.
	DemoPublic bool `json:"-"`

	// DevMode ist der Entwicklungsmodus (CLI-Flag --dev): erlaubt
	// unverschlüsseltes HTTP und schaltet die 2FA-Pflicht für Administratoren
	// beim Erst-Seeding ab. Wie DemoMode bewusst NICHT persistiert (json:"-"),
	// damit eine alte oder wiederhergestellte config.json den Produktivbetrieb
	// nicht unbemerkt abschwächen kann.
	DevMode bool `json:"-"`

	// LogLevel: "debug", "info", "warn" oder "error". Das CLI-Flag -debug
	// überschreibt diesen Wert zur Laufzeit mit "debug".
	LogLevel string `json:"log_level"`

	// LogFile: Pfad der persistenten, rotierenden Logdatei (zusätzlich zu
	// stdout). Leer = Standard <Datenverzeichnis>/logs/lcm.log. Hier werden
	// Dienst-Starts/-Stopps, Backups und andere Aktionen mitgeschrieben - für
	// die Überwachung von Neustarts/Abstürzen im Service-Betrieb.
	LogFile string `json:"log_file"`

	// AccessLog protokolliert jeden API-Request (Methode, Pfad, Status,
	// Dauer, User) über den Log-Service.
	AccessLog bool `json:"access_log"`

	// APIKeyRateLimitPerMinute begrenzt Requests pro API-Key und Minute.
	// 0 deaktiviert das Rate-Limiting. Gilt nur für API-Key-Requests,
	// nicht für Browser-Sessions.
	APIKeyRateLimitPerMinute int `json:"api_key_rate_limit_per_minute"`

	// TrivyPath ist das Trivy-Binary für den CVE-Scan (Name via PATH oder
	// absoluter Pfad). Fehlt Trivy, deaktiviert sich der CVE-Scan sauber.
	TrivyPath string `json:"trivy_path"`

	// TrivyURL schaltet den CVE-Scan auf einen Trivy-Sidecar um (Adresse des
	// Dienstes, z.B. http://trivy:9330). Leer = das lokale Binary aus
	// TrivyPath, also das bisherige Verhalten.
	//
	// Gebraucht wird das im Container-Betrieb: LCMs Runtime-Image ist ein
	// Scratch-Image ohne Shell und ohne Trivy. Env: LCM_TRIVY_URL.
	TrivyURL string `json:"trivy_url"`
	// TrivyToken ist das Bearer-Token für den Sidecar. Env: LCM_TRIVY_TOKEN -
	// in einem Compose-Setup der übliche Weg, damit das Geheimnis nicht in
	// der config.json steht.
	TrivyToken string `json:"trivy_token"`

	// BackupDir gibt das Verzeichnis für System-Backups fest vor (absolut).
	// Leer = die UI-Einstellung greift, sonst <data>/backups. Nützlich, um
	// Backups per Deployment auf ein persistentes/externes Volume zu legen,
	// ohne die DB-Einstellung zu setzen.
	BackupDir string `json:"backup_dir"`

	// AllowedIPs schränkt den Netzwerkzugriff auf Weboberfläche und API auf
	// bestimmte Client-Adressen ein. Leere Liste (Default) = keine
	// Einschränkung (jede IP darf zugreifen - bisheriges Verhalten). Einträge
	// sind entweder Schlüsselwörter ("localhost"/"private") oder IP-Adressen
	// bzw. CIDR-Bereiche (z.B. "192.168.1.10", "10.0.0.0/8", "::1",
	// "fd00::/8"). Nicht passende Clients erhalten 403. Gefiltert wird auf der
	// direkten TCP-Verbindung; hinter einem Reverse-Proxy siehe
	// TrustProxyHeader.
	AllowedIPs []string `json:"allowed_ips"`

	// TrustProxyHeader steuert, woher der IP-Filter (AllowedIPs) die Client-IP
	// nimmt: false (Default) = die direkte TCP-Verbindung (fälschungssicher);
	// true = die erste Adresse aus dem X-Forwarded-For-Header. NUR aktivieren,
	// wenn LCM hinter einem vertrauenswürdigen Reverse-Proxy läuft, der diesen
	// Header selbst setzt bzw. überschreibt - sonst ließe sich der Filter durch
	// einen gefälschten Header umgehen.
	TrustProxyHeader bool `json:"trust_proxy_header"`
}

// AccessTokenTTL liefert die Token-Lebensdauer als time.Duration.
func (c *Config) AccessTokenTTL() time.Duration {
	return time.Duration(c.AccessTokenTTLMinutes) * time.Minute
}

// Address liefert die Listen-Adresse für den HTTP-Server (UI + REST-API).
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// AgentAddress liefert die Listen-Adresse des dedizierten Agent-Listeners
// (LCM Remote). Fällt für den Host auf 0.0.0.0 zurück, wenn keiner gesetzt
// ist - der Agent-Port muss von außen erreichbar sein.
func (c *Config) AgentAddress() string {
	host := c.AgentHost
	if host == "" {
		host = "0.0.0.0"
	}
	return fmt.Sprintf("%s:%d", host, c.AgentPort)
}

// AgentListenerEnabled meldet, ob der dedizierte Agent-Listener laufen soll.
// AgentPort = 0 (oder negativ) schaltet ihn bewusst ab.
func (c *Config) AgentListenerEnabled() bool {
	return c.AgentPort > 0
}

// DefaultFileName ist der Name der Konfigurationsdatei im Datenverzeichnis.
const DefaultFileName = "config.json"

// LoadFrom lädt die Konfiguration von einem expliziten Pfad.
// Existiert die Datei nicht, wird sie dort mit Defaults angelegt.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := generateDefault()
		if writeErr := cfg.save(path); writeErr != nil {
			return nil, fmt.Errorf("config.json erzeugen: %w", writeErr)
		}
		fmt.Println(i18n.Tf(
			"config: created %s with generated secrets",
			"config: neue %s mit generierten Secrets erstellt",
			path))
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config.json lesen: %w", err)
	}

	cfg := generateDefault()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config.json parsen: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	// Neue Optionen in eine bestehende Datei nachtragen.
	//
	// Die Werte stehen bereits richtig im Struct (die Datei wird über die
	// Defaults gelegt), aber Schlüssel, die es beim Anlegen noch nicht gab,
	// fehlen in der DATEI - der Anwender muss ihren Namen dann aus der Doku
	// raten, statt ihn beim Öffnen zu sehen (BUG-003). Geschrieben wird nur,
	// wenn sich dadurch wirklich etwas ändert; bestehende Werte bleiben
	// unangetastet, da sie zuvor eingelesen wurden.
	if merged, err := json.MarshalIndent(cfg, "", "  "); err == nil && !bytes.Equal(bytes.TrimSpace(data), merged) {
		if err := cfg.save(path); err != nil {
			// Nicht schreibbar (read-only Mount, fremder Eigentümer) ist kein
			// Grund, den Start abzubrechen - die Konfiguration ist ja gültig.
			fmt.Println(i18n.Tf(
				"config: could not add new options to %s: %v",
				"config: %s konnte nicht um neue Optionen ergänzt werden: %v",
				path, err))
		}
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("jwt_secret ist zu kurz (min. 32 Zeichen) - bitte config.json löschen und neu generieren lassen")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("ungültiger Port: %d", c.Port)
	}
	// AgentPort = 0 ist erlaubt (Listener aus); ansonsten gültiger Port und
	// nicht identisch mit dem UI/REST-Port (sonst kollidieren die Listener).
	if c.AgentPort != 0 {
		if c.AgentPort < 0 || c.AgentPort > 65535 {
			return fmt.Errorf("ungültiger agent_port: %d", c.AgentPort)
		}
		if c.AgentPort == c.Port {
			return fmt.Errorf("agent_port darf nicht mit port übereinstimmen (%d) - der Agent-Listener braucht einen eigenen Port", c.Port)
		}
	}
	if c.AccessTokenTTLMinutes <= 0 {
		return fmt.Errorf("access_token_ttl_minutes muss > 0 sein")
	}
	if err := c.ValidateTrivy(); err != nil {
		return err
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("ungültiges log_level %q (erlaubt: debug, info, warn, error)", c.LogLevel)
	}
	if c.APIKeyRateLimitPerMinute < 0 {
		return fmt.Errorf("api_key_rate_limit_per_minute darf nicht negativ sein")
	}
	// Die IP-Allowlist bereits beim Laden validieren (Fail-Fast bei Tippfehlern
	// wie einem ungültigen CIDR), damit ein Fehler nicht erst zur Laufzeit auffällt.
	if _, err := netfilter.Parse(c.AllowedIPs); err != nil {
		return fmt.Errorf("allowed_ips: %w", err)
	}
	return nil
}

// IPAllowlist baut den Matcher für die konfigurierte Allowlist. Wurde die
// Config über LoadFrom geladen, ist sie bereits validiert; der Fehler kann
// hier nur bei programmatisch erzeugten Configs auftreten.
func (c *Config) IPAllowlist() (netfilter.Allowlist, error) {
	return netfilter.Parse(c.AllowedIPs)
}

func (c *Config) save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// 0600: Die Datei enthält Secrets und gehört nur dem Service-User.
	return os.WriteFile(path, data, 0o600)
}

func generateDefault() *Config {
	return &Config{
		Host: "127.0.0.1",
		// 9310/9320 (+ 9330 für MCP in den DB-Einstellungen): eigener
		// LCM-Portblock. Bewusst NICHT 8080 - dort lauscht die CrowdSec-LAPI,
		// wenn sie auf demselben Host installiert wird (bevorzugtes Setup).
		Port:                     9310,
		AgentHost:                "0.0.0.0",
		AgentPort:                9320,
		DatabasePath:             "app.db",
		JWTSecret:                RandomSecret(48),
		AccessTokenTTLMinutes:    60,
		AdminInitialPassword:     "",
		LogLevel:                 "info",
		AccessLog:                true,
		APIKeyRateLimitPerMinute: 120,
		TrivyPath:                "trivy",
		// Leere Allowlist = keine Netzwerk-Einschränkung. Als [] statt nil,
		// damit das Feld in einer neu erzeugten config.json sichtbar auftaucht
		// (Auffindbarkeit des Sicherheits-Schalters).
		AllowedIPs: []string{},
	}
}

// ValidateTrivy prüft die Sidecar-Angaben.
//
// Exportiert, weil sie ZWEIMAL gebraucht wird: beim Laden der config.json
// und noch einmal, nachdem die Umgebungsvariablen sie übersteuert haben
// (LCM_TRIVY_URL/LCM_TRIVY_TOKEN). Ohne den zweiten Aufruf bliebe ein per
// Umgebung gesetzter Sidecar ungeprüft.
//
// Der wichtige Teil ist die Token-Pflicht: Ohne sie liefe LCM anstandslos
// weiter, bekäme vom Sidecar aber auf jede Anfrage eine 401 - der CVE-Scan
// wäre also tot, ohne dass beim Start etwas auffällt. Ein Fehler beim Start
// ist unmissverständlich; ein stiller Dauerfehlschlag ist es nicht.
func (c *Config) ValidateTrivy() error {
	url := strings.TrimSpace(c.TrivyURL)
	if url == "" {
		return nil
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("ungültige trivy_url %q - erwartet wird eine Adresse mit http:// oder https://", c.TrivyURL)
	}
	if strings.TrimSpace(c.TrivyToken) == "" {
		return fmt.Errorf("trivy_url ist gesetzt, aber kein trivy_token - der Sidecar lehnt Anfragen ohne Token ab (Env: LCM_TRIVY_TOKEN)")
	}
	return nil
}

// RandomSecret erzeugt n zufällige Bytes aus crypto/rand, base64-kodiert.
func RandomSecret(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand darf auf unterstützten Plattformen nicht fehlschlagen.
		panic(fmt.Sprintf("crypto/rand nicht verfügbar: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
