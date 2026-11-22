// Package config verwaltet die Anwendungskonfiguration (config.json).
//
// Beim Start wird nach einer config.json im Verzeichnis des Binaries gesucht.
// Fehlt sie, wird sie mit Standardwerten erzeugt. Geheimnisse stehen nicht
// darin: Der Signaturschlüssel der 2FA-Challenge entsteht bei jedem Start neu im
// Speicher, der Master-Key liegt in lcm.key oder in einem systemd-Credential.
// Ein `jwt_secret` aus älteren Fassungen wird beim Laden ignoriert und beim
// nächsten Schreiben der Datei entfernt.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/i18n"
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

	// AccessTokenTTLMinutes ist die Leerlauf-Frist einer Browser-Sitzung in Minuten,
	// solange die Einstellung "Sitzungsdauer" nichts anderes sagt. Der Name stammt
	// aus der Zeit des JWT-Logins und bleibt, damit bestehende config.json laden.
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

	// ACMEHTTPListen ist die Adresse des Listeners für die Prüfung HTTP-01
	// einer ACME-CA, Standard ":80". Er ist nur offen, solange eine Prüfung
	// läuft, und beantwortet dort allein das Token der Prüfung. Ports unter
	// 1024 braucht die Capability CAP_NET_BIND_SERVICE (die mitgelieferte
	// lcm.service setzt sie); alternativ ein hoher Port hinter einer
	// Portweiterleitung.
	ACMEHTTPListen string `json:"acme_http_listen"`

	// ACMEDNSExec ist das Programm für den DNS-Anbieter "exec" bei DNS-01:
	// ein absoluter Pfad. LCM ruft es mit present oder cleanup auf, dazu der
	// Name des TXT-Eintrags und sein Wert. Der Anbieter ist nur wählbar, wenn
	// der Pfad hier steht, und über die API nicht änderbar: Ein Konto in der
	// Oberfläche darf keinen Befehl auf dem LCM-Host festlegen. Ob die Datei
	// da ist, prüft LCM beim Auswählen, nicht beim Start.
	ACMEDNSExec string `json:"acme_dns_exec"`

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

	// Die Netzbereiche, Proxys, Ports und Adressen der Einstellungen
	// (Mirror) stehen hier und in der Datenbank zugleich: Speichern in der
	// Oberfläche schreibt sie in die Datei, eine von Hand geänderte Datei
	// übernimmt LCM beim Start (cmd/app/config_sync.go).
	//
	// AllowedIPs schränkt den Netzwerkzugriff auf Weboberfläche und API auf
	// bestimmte Client-Adressen ein. Leere Liste (Default) = keine
	// Einschränkung (jede IP darf zugreifen - bisheriges Verhalten). Einträge
	// sind entweder Schlüsselwörter ("localhost"/"private") oder IP-Adressen
	// bzw. CIDR-Bereiche (z.B. "192.168.1.10", "10.0.0.0/8", "::1",
	// "fd00::/8"). Nicht passende Clients erhalten 403. Gefiltert wird auf der
	// direkten TCP-Verbindung; hinter einem Reverse-Proxy siehe
	// TrustProxyHeader.
	AllowedIPs []string `json:"allowed_ips"`

	// AgentAllowedIPs und MCPAllowedIPs sind dasselbe für den Agent-Endpunkt
	// und die MCP-Schnittstelle. Leer = alle Netze. Wer sich ausgesperrt hat,
	// leert die Liste hier und startet LCM neu.
	AgentAllowedIPs []string `json:"agent_allowed_ips"`
	MCPAllowedIPs   []string `json:"mcp_allowed_ips"`

	// TrustProxyHeader steuert, woher IP-Filter (AllowedIPs), Anmeldesperre
	// und Protokolle die Client-IP nehmen: false (Default) = die direkte
	// TCP-Verbindung (fälschungssicher); true = aus dem X-Forwarded-For-Header,
	// aber nur von den Gegenstellen in TrustedProxies. NUR aktivieren, wenn LCM
	// hinter einem Reverse-Proxy läuft, der diesen Header selbst setzt bzw.
	// ergänzt.
	TrustProxyHeader bool `json:"trust_proxy_header"`

	// TrustedProxies nennt die Gegenstellen, die wirklich der Reverse-Proxy
	// sind (IP-Adressen, CIDR-Bereiche oder die Schlüsselwörter
	// "localhost"/"private"). Nur wenn die direkte TCP-Verbindung von einer
	// dieser Adressen kommt, gilt X-Forwarded-For (und X-Forwarded-Proto);
	// maßgeblich ist der rechteste Eintrag, der selbst kein vertrauter Proxy
	// ist. Leer = niemandem vertrauen: Der Header wird ignoriert, auch wenn
	// TrustProxyHeader an ist. Früher hieß leer „jeder Peer" - ein Client, der
	// den Port direkt erreichte, gab sich mit einer gefälschten Kopfzeile jede
	// beliebige Adresse: in die IP-Allowlist hinein und aus der Anmeldesperre
	// heraus. Ein Bereich über alle Adressen (0.0.0.0/0, ::/0) wird nicht
	// übernommen.
	TrustedProxies []string `json:"trusted_proxies"`

	// TLSCACert/TLSCAKey nennen eine CA des Betreibers (PEM-Dateien,
	// absolute Pfade), etwa eine Zwischen-CA der eigenen PKI. Mit ihr stellt
	// LCM sein Server-Zertifikat aus und legt keine eigene CA an. In der
	// Zertifikatsdatei darf hinter der CA ihre Kette folgen; LCM liefert sie
	// mit aus. Beide leer (Default) = die eigene CA im Datenverzeichnis.
	// Die Dateien liegen außerhalb des Datenverzeichnisses und gehören nicht
	// zum System-Backup.
	TLSCACert string `json:"tls_ca_cert"`
	TLSCAKey  string `json:"tls_ca_key"`

	// TLSCertificates sind eigene Zertifikate des Betreibers (PEM-Dateien,
	// absolute Pfade). UI- und Agent-Listener liefern das Zertifikat aus,
	// dessen Namen zum angefragten Namen passen; ohne Treffer und bei
	// Aufruf per IP bleibt es beim Zertifikat der LCM-CA. Ein Zertifikat,
	// das beim Start nicht lädt, hindert LCM nicht am Start (Warnung im Log).
	TLSCertificates []TLSCertificateFile `json:"tls_certificates"`

	// SharedTLSListen ist die Adresse (z.B. ":443") eines gemeinsamen
	// TLS-Listeners für Weboberfläche, Agent und MCP. Welcher Dienst die
	// Verbindung bekommt, entscheidet der angefragte Name (SNI): die Namen der
	// öffentlichen Adressen aus den Einstellungen. Die Einzelports bleiben
	// daneben aktiv. Leer (Default) = kein gemeinsamer Port. Gesetzt ist der
	// Adressmodus „ein Port"; der Host-Teil legt fest, an welche Adresse LCM
	// bindet, der Port gilt in den Einstellungen. Unter 1024 braucht der Dienst
	// die Capability CAP_NET_BIND_SERVICE (die mitgelieferte lcm.service setzt
	// sie).
	SharedTLSListen string `json:"shared_tls_listen"`
}

// TLSCertificateFile ist ein Zertifikat samt Schlüssel in der config.json.
type TLSCertificateFile struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

// AccessTokenTTL liefert die Leerlauf-Frist einer Browser-Sitzung als time.Duration.
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
	// raten, statt ihn beim Öffnen zu sehen (BUG-003). Es kommen nur fehlende
	// Schlüssel dazu; bestehende Werte und unbekannte Schlüssel (etwa die
	// gespiegelten Einstellungen, siehe Mirror) bleiben, wie sie sind.
	if err := cfg.addNewOptions(path, data); err != nil {
		// Nicht schreibbar (read-only Mount, fremder Eigentümer) ist kein
		// Grund, den Start abzubrechen - die Konfiguration ist ja gültig.
		fmt.Println(i18n.Tf(
			"config: could not add new options to %s: %v",
			"config: %s konnte nicht um neue Optionen ergänzt werden: %v",
			path, err))
	}
	return cfg, nil
}

// retiredKeys sind Schlüssel früherer Fassungen ohne Funktion: Sie stehen nicht
// mehr in Config und werden beim Nachtragen aus der Datei genommen. Alle
// anderen unbekannten Schlüssel bleiben.
var retiredKeys = map[string]bool{"jwt_secret": true}

// addNewOptions trägt die Schlüssel in die Datei data ein, die ihr fehlen,
// mit dem Wert, der jetzt gilt, und nimmt die stillgelegten heraus. Ohne beides
// bleibt die Datei unberührt.
func (c *Config) addNewOptions(path string, data []byte) error {
	all, err := readMembers(data)
	if err != nil {
		return err
	}
	var existing []member
	for _, m := range all {
		if !retiredKeys[m.key] {
			existing = append(existing, m)
		}
	}
	full, err := json.Marshal(c)
	if err != nil {
		return err
	}
	wanted, err := readMembers(full)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, m := range existing {
		have[m.key] = true
	}
	missing := map[string]json.RawMessage{}
	var order []string
	for _, m := range wanted {
		if !have[m.key] {
			missing[m.key] = m.value
			order = append(order, m.key)
		}
	}
	if len(missing) == 0 && len(existing) == len(all) {
		return nil
	}
	merged, err := mergeMembers(existing, missing, order)
	if err != nil {
		return err
	}
	return replaceFile(path, merged)
}

func (c *Config) validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("ungültiger Port: %d", c.Port)
	}
	// AgentPort = 0 ist erlaubt: In alten Dateien hieß das „Agent aus", LCM
	// übernimmt es als agent_enabled=false (siehe cmd/app/config_sync.go).
	if c.AgentPort < 0 || c.AgentPort > 65535 {
		return fmt.Errorf("ungültiger agent_port: %d", c.AgentPort)
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
	if err := validateListenAddress("acme_http_listen", c.ACMEHTTPListen); err != nil {
		return err
	}
	if c.ACMEDNSExec != "" && !filepath.IsAbs(c.ACMEDNSExec) {
		return fmt.Errorf("acme_dns_exec: %q ist kein absoluter Pfad", c.ACMEDNSExec)
	}
	// Die gespiegelten Werte (Netzbereiche, Proxys, Ports, Modus) prüft LCM erst
	// beim Übernehmen in die Einstellungen, wie bei einem PATCH: Ein Tippfehler
	// in einer von Hand geänderten Datei hält LCM nicht vom Start ab, der Wert
	// aus der Datenbank gilt weiter, und das Log nennt den Fehler.
	if err := c.validateTLSCA(); err != nil {
		return err
	}
	if err := validateTLSCertificates(c.TLSCertificates); err != nil {
		return err
	}
	return nil
}

// validateTLSCA: tls_ca_cert und tls_ca_key nur zusammen und als absolute
// Pfade - ein relativer hinge vom Arbeitsverzeichnis des Dienstes ab.
func (c *Config) validateTLSCA() error {
	if (c.TLSCACert == "") != (c.TLSCAKey == "") {
		return fmt.Errorf("tls_ca_cert und tls_ca_key gehören zusammen")
	}
	for _, p := range []string{c.TLSCACert, c.TLSCAKey} {
		if p != "" && !filepath.IsAbs(p) {
			return fmt.Errorf("tls_ca_cert/tls_ca_key: %q ist kein absoluter Pfad", p)
		}
	}
	return nil
}

// validateTLSCertificates: Zertifikat und Schlüssel je Eintrag, absolute
// Pfade, kein Zertifikat doppelt.
func validateTLSCertificates(files []TLSCertificateFile) error {
	seen := map[string]bool{}
	for i, f := range files {
		if f.Cert == "" || f.Key == "" {
			return fmt.Errorf("tls_certificates[%d]: cert und key gehören zusammen", i)
		}
		for _, p := range []string{f.Cert, f.Key} {
			if !filepath.IsAbs(p) {
				return fmt.Errorf("tls_certificates[%d]: %q ist kein absoluter Pfad", i, p)
			}
		}
		cert := filepath.Clean(f.Cert)
		if seen[cert] {
			return fmt.Errorf("tls_certificates[%d]: %q kommt doppelt vor", i, f.Cert)
		}
		seen[cert] = true
	}
	return nil
}

// LoadTLSCertificates liest nur tls_certificates aus der config.json, ohne
// den Rest zu prüfen und ohne die Datei je zu schreiben. Für das Neuladen der
// Zertifikate zur Laufzeit: Ein Fehler in einem anderen Abschnitt soll es
// nicht blockieren, und der Dienst darf die Datei nicht anfassen.
func LoadTLSCertificates(path string) ([]TLSCertificateFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config.json lesen: %w", err)
	}
	var section struct {
		TLSCertificates []TLSCertificateFile `json:"tls_certificates"`
	}
	if err := json.Unmarshal(data, &section); err != nil {
		return nil, fmt.Errorf("config.json parsen: %w", err)
	}
	if err := validateTLSCertificates(section.TLSCertificates); err != nil {
		return nil, err
	}
	return section.TLSCertificates, nil
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
		AccessTokenTTLMinutes:    60,
		AdminInitialPassword:     "",
		LogLevel:                 "info",
		AccessLog:                true,
		APIKeyRateLimitPerMinute: 120,
		ACMEHTTPListen:           ":80",
		TrivyPath:                "trivy",
		// Leere Allowlist = keine Netzwerk-Einschränkung. Als [] statt nil,
		// damit das Feld in einer neu erzeugten config.json sichtbar auftaucht
		// (Auffindbarkeit des Sicherheits-Schalters).
		AllowedIPs:      []string{},
		AgentAllowedIPs: []string{},
		MCPAllowedIPs:   []string{},
		// Ebenfalls sichtbar leer: Wer trust_proxy_header einschaltet, soll
		// den Schlüssel daneben stehen sehen.
		TrustedProxies: []string{},
		// Sichtbar leer, damit der Schlüssel in der config.json zu finden ist.
		TLSCertificates: []TLSCertificateFile{},
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
