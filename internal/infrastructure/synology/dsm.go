// Package synology spricht die Web-API von Synology DSM (SYNO.*).
//
// Warum API statt SSH: DSM ist zwar Linux-basiert, aber kein verwaltbarer
// Linux-Server - es gibt kein /etc/os-release, der Kernel ist ein alter
// Synology-Fork (der CVE-Scan liefe in Falschalarme), Pakete verwaltet
// synopkg statt apt, und Benutzer/Dienste verwaltet DSM selbst. Ein
// LCM-Service-User mit sudo würde mit DSMs eigener Konfigurationsverwaltung
// kollidieren. Die dokumentierte Web-API liefert dagegen genau das, was LCM
// für die Überwachung braucht: Version, Updates, Pakete, Speicher, Zeit/NTP,
// die Befunde des DSM-eigenen Security Advisors und die Sicherheitseinstellungen.
package synology

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultPort ist der HTTPS-Port der DSM-Oberfläche.
const DefaultPort = 5001

// requestTimeout begrenzt jeden einzelnen API-Aufruf. DSM antwortet lokal in
// Millisekunden; ein hängendes Gerät darf einen Scan-Lauf nicht aufhalten.
const requestTimeout = 20 * time.Second

var (
	// ErrAuth: Anmeldung abgelehnt (falsche Zugangsdaten, gesperrtes Konto
	// oder erzwungene 2FA - DSM verlangt dann einen OTP-Code, den ein
	// unbeaufsichtigter Scan nicht liefern kann).
	ErrAuth = errors.New("DSM-Anmeldung fehlgeschlagen")
	// ErrCertMismatch: das TLS-Zertifikat weicht vom bestätigten Fingerprint
	// ab - möglicher MitM-Angriff, Verbindung abgebrochen.
	ErrCertMismatch = errors.New("TLS-Zertifikat des DSM stimmt nicht mit dem bestätigten Fingerprint überein - möglicher MitM-Angriff")
)

// dsmErrors übersetzt die dokumentierten Fehlercodes der Auth-API. Ohne diese
// Übersetzung stünde in der Oberfläche nur eine nackte Zahl.
var dsmErrors = map[int]string{
	400: "falscher Benutzername oder falsches Passwort",
	401: "Konto deaktiviert",
	402: "Berechtigung verweigert - das Konto braucht Administrator-Rechte",
	403: "Zwei-Faktor-Code erforderlich - für LCM ein Konto ohne erzwungene 2FA verwenden (z.B. per IP-Beschränkung absichern)",
	404: "Zwei-Faktor-Code ungültig",
	406: "Zwei-Faktor-Pflicht: DSM verlangt die Einrichtung von 2FA für dieses Konto",
	407: "Zugriff von dieser IP-Adresse gesperrt (DSM-Blockliste)",
	408: "Passwort abgelaufen - in DSM erneuern",
	409: "Passwort abgelaufen",
	410: "Passwort muss geändert werden",
	119: "Sitzung abgelaufen oder ungültig",
}

// Client spricht die DSM-Web-API eines Geräts.
type Client struct {
	base string
	http *http.Client
	sid  string
	// apis ist das Verzeichnis aus SYNO.API.Info (nil = nicht abfragbar).
	apis map[string]apiInfo
}

// Config sind die Verbindungsdaten eines DSM-Geräts.
type Config struct {
	Host string
	Port int
	// CertFingerprint ist der bestätigte SHA-256-Fingerprint (Hex, ohne
	// Trennzeichen). Leer = jedes Zertifikat wird akzeptiert; das ist NUR
	// beim allerersten Kontakt (Probe) vorgesehen, danach ist der Pin
	// gesetzt - DSM liefert ab Werk ein selbstsigniertes Zertifikat, eine
	// Kettenprüfung gibt es hier also nicht.
	CertFingerprint string
}

// New baut einen Client für ein DSM-Gerät.
func New(cfg Config) *Client {
	port := cfg.Port
	if port <= 0 {
		port = DefaultPort
	}
	pin := normalizeFingerprint(cfg.CertFingerprint)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			// Selbstsigniert ist bei DSM der Normalfall - statt der Kette
			// prüfen wir den gepinnten Fingerprint (siehe verifyPin).
			InsecureSkipVerify:    true, //nolint:gosec // Pinning statt Kettenprüfung, siehe unten
			VerifyPeerCertificate: verifyPin(pin),
		},
	}
	return &Client{
		base: fmt.Sprintf("https://%s:%d/webapi/", cfg.Host, port),
		http: &http.Client{Transport: tr, Timeout: requestTimeout},
	}
}

// verifyPin prüft das Server-Zertifikat gegen den Pin. Zählt nur das erste
// Zertifikat der Kette, das des Servers selbst: Alles dahinter kann ein
// Angreifer im Netz frei anhängen, auch das echte DSM-Zertifikat, das er
// vorher einmal abgerufen hat. Den Schlüssel dazu hat er nicht, den TLS-
// Handshake führt er aber mit dem ersten Zertifikat - mit seinem eigenen.
func verifyPin(pin string) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if pin == "" {
			return nil
		}
		if len(rawCerts) == 0 || fingerprintOf(rawCerts[0]) != pin {
			return ErrCertMismatch
		}
		return nil
	}
}

// ProbeFingerprint liest den SHA-256-Fingerprint des TLS-Zertifikats, ohne
// ihn zu prüfen - die Grundlage für die Bestätigung im Onboarding-Dialog
// (Trust-on-First-Use, wie beim SSH-Host-Key).
func ProbeFingerprint(host string, port int) (string, error) {
	if port <= 0 {
		port = DefaultPort
	}
	dialer := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // genau dieser Aufruf HOLT den Fingerprint
	conn, err := dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return "", fmt.Errorf("verbindung zu %s:%d: %w", host, port, err)
	}
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return "", errors.New("keine TLS-Verbindung")
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("das Gerät hat kein TLS-Zertifikat vorgelegt")
	}
	return fingerprintOf(certs[0].Raw), nil
}

func fingerprintOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// normalizeFingerprint macht Doppelpunkt-Schreibweise und Groß-/Kleinschreibung
// vergleichbar ("FF:0D:…" == "ff0d…").
func normalizeFingerprint(fp string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(fp), ":", ""))
}

// envelope ist der einheitliche Antwortrahmen der DSM-API.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// call führt einen API-Aufruf aus und liefert das data-Feld. Die Session-ID
// wird angehängt, sobald eine besteht.
//
// Die Parameter gehen als Formular im Rumpf eines POST, nicht in der URL:
// Dort stünden Passwort und Session-ID in jedem Fehlertext eines
// Netzfehlers (net/http nennt die volle URL) und damit im Log.
func (c *Client) call(api, version, method string, params map[string]string, out any) error {
	return c.callAt("entry.cgi", api, version, method, params, out)
}

// callAt ist call mit ausdrücklichem CGI-Pfad - SYNO.API.Info liegt unter
// query.cgi, und das Verzeichnis nennt für jede API ihren eigenen Pfad.
func (c *Client) callAt(path, api, version, method string, params map[string]string, out any) error {
	form := url.Values{}
	form.Set("api", api)
	form.Set("version", version)
	form.Set("method", method)
	for k, v := range params {
		form.Set(k, v)
	}
	if c.sid != "" {
		form.Set("_sid", c.sid)
	}
	resp, err := c.http.PostForm(c.base+path, form)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", api, method, withoutURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%s.%s: antwort lesen: %w", api, method, err)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("%s.%s: unerwartete antwort (kein DSM-JSON): %w", api, method, err)
	}
	if !env.Success {
		code := 0
		if env.Error != nil {
			code = env.Error.Code
		}
		if msg, ok := dsmErrors[code]; ok {
			return fmt.Errorf("%s.%s: %s (DSM-Code %d)", api, method, msg, code)
		}
		return fmt.Errorf("%s.%s: DSM-Fehlercode %d", api, method, code)
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// withoutURL nimmt die URL aus einem Fehler von net/http. Sie enthält zwar
// seit dem Umstieg auf POST nichts Geheimes mehr, gehört aber trotzdem nicht
// in Fehlertexte: Der Fehler wandert bis in Job-Protokoll und Log, und was
// dort einmal steht, bleibt dort stehen.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// Login meldet sich an und merkt sich die Sitzungs-ID.
func (c *Client) Login(account, password string) error {
	var data struct {
		SID string `json:"sid"`
	}
	err := c.call("SYNO.API.Auth", "7", "login", map[string]string{
		"account": account, "passwd": password, "format": "sid",
	}, &data)
	if err != nil {
		if errors.Is(err, ErrCertMismatch) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	if data.SID == "" {
		return fmt.Errorf("%w: keine Sitzungs-ID erhalten", ErrAuth)
	}
	c.sid = data.SID
	return nil
}

// Logout beendet die Sitzung (best effort - DSM räumt sie sonst selbst ab).
func (c *Client) Logout() {
	if c.sid == "" {
		return
	}
	_ = c.call("SYNO.API.Auth", "7", "logout", nil, nil)
	c.sid = ""
}

// Info ist der erhobene Zustand eines DSM-Geräts.
type Info struct {
	Model   string // z.B. "DS923+", "VirtualDSM"
	Version string // z.B. "DSM 7.3.2-86009"
	Serial  string
	// CPUModel setzt sich aus Hersteller, Familie und Serie zusammen
	// ("AMD Ryzen R1600"). VirtualDSM meldet nichts davon, nur die Zahl der
	// vCPUs - siehe Virtual.
	CPUModel   string
	CPUCores   int
	Virtual    bool // Modell VirtualDSM: CPU und Platten sind virtuell
	RAMSizeMB  int
	UptimeSec  int64
	Timezone   string // DSM-Bezeichnung, z.B. "Amsterdam"
	NTPEnabled bool
	NTPServer  string
	// Uhrzeit des Geräts (DSM liefert sie als lokale Zeichenkette) und die
	// Differenz zur LCM-Uhr in Sekunden.
	DeviceTime string

	UpdateAvailable bool
	LatestVersion   string

	Packages []Package

	VolumeTotalMB int
	VolumeUsedMB  int
	VolumeStatus  string // "normal" oder der schlechteste abweichende Zustand
	// Storage sind Pools, Volumes und Platten einzeln (nil = load_info nicht
	// lesbar; dann bleibt der zuletzt erfasste Stand stehen).
	Storage *Storage

	SecurityRisks   int
	SecuritySummary string

	// Settings sind Firewall und Sicherheitseinstellungen.
	Settings Settings
}

// Package ist ein installiertes DSM-Paket.
type Package struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Collect erhebt den vollständigen Zustand. Einzelne Teil-Abfragen dürfen
// fehlschlagen (je nach DSM-Version/Berechtigung ist nicht jede API da) -
// nur System.info ist Pflicht, denn ohne sie ist das Gerät nicht erkannt.
func (c *Client) Collect() (*Info, error) {
	var sys struct {
		Model      string  `json:"model"`
		FirmwareV  string  `json:"firmware_ver"`
		Serial     string  `json:"serial"`
		CPUCores   flexInt `json:"cpu_cores"`
		CPUVendor  string  `json:"cpu_vendor"`
		CPUFamily  string  `json:"cpu_family"`
		CPUSeries  string  `json:"cpu_series"`
		RAMSize    int     `json:"ram_size"`
		UpTime     string  `json:"up_time"`
		TimeZone   string  `json:"time_zone"`
		NTPEnabled bool    `json:"enabled_ntp"`
		NTPServer  string  `json:"ntp_server"`
		Time       string  `json:"time"`
	}
	if err := c.call("SYNO.Core.System", "3", "info", nil, &sys); err != nil {
		return nil, err
	}
	info := &Info{
		Model:      sys.Model,
		Version:    sys.FirmwareV,
		Serial:     sys.Serial,
		RAMSizeMB:  sys.RAMSize,
		Timezone:   sys.TimeZone,
		NTPEnabled: sys.NTPEnabled,
		NTPServer:  sys.NTPServer,
		DeviceTime: sys.Time,
		UptimeSec:  parseUptime(sys.UpTime),
		CPUCores:   int(sys.CPUCores),
		CPUModel:   joinNonEmpty(sys.CPUVendor, sys.CPUFamily, sys.CPUSeries),
		Virtual:    isVirtualModel(sys.Model),
	}

	// Ab hier best effort: was nicht antwortet, bleibt leer.
	var upg struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
		Update    struct {
			Version string `json:"version"`
		} `json:"update"`
	}
	if err := c.call("SYNO.Core.Upgrade.Server", "1", "check", nil, &upg); err == nil {
		info.UpdateAvailable = upg.Available
		info.LatestVersion = firstNonEmpty(upg.Version, upg.Update.Version)
	}

	var pkgs struct {
		Packages []Package `json:"packages"`
	}
	if err := c.call("SYNO.Core.Package", "1", "list", nil, &pkgs); err == nil {
		info.Packages = pkgs.Packages
	}

	c.collectStorage(info)
	c.collectSecurity(info)
	c.discoverAPIs()
	info.Settings = c.collectSettings()
	return info, nil
}

// collectSecurity übernimmt die Bewertung des DSM-eigenen Security Advisors:
// gezählt werden die Befunde der Stufen risk und danger je Kategorie.
func (c *Client) collectSecurity(info *Info) {
	var sec struct {
		Items map[string]struct {
			Category string `json:"category"`
			Fail     struct {
				Danger    int `json:"danger"`
				Risk      int `json:"risk"`
				Warning   int `json:"warning"`
				OutOfDate int `json:"outOfDate"`
			} `json:"fail"`
			FailSeverity string `json:"failSeverity"`
		} `json:"items"`
	}
	if err := c.call("SYNO.Core.SecurityScan.Status", "1", "system_get", nil, &sec); err != nil {
		return
	}
	total := 0
	var parts []string
	for name, it := range sec.Items {
		n := it.Fail.Danger + it.Fail.Risk
		if n == 0 {
			continue
		}
		total += n
		label := it.Category
		if label == "" {
			label = name
		}
		parts = append(parts, fmt.Sprintf("%s: %d", label, n))
	}
	info.SecurityRisks = total
	// Sortierung über die Kategorien-Namen wäre schöner, ist aber für eine
	// kurze Zusammenfassung entbehrlich - die Zahl trägt die Aussage.
	info.SecuritySummary = strings.Join(parts, ", ")
}

// parseUptime wandelt DSMs "48:52:9" (Stunden:Minuten:Sekunden) in Sekunden.
func parseUptime(s string) int64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0
	}
	var total int64
	for i, mult := range []int64{3600, 60, 1} {
		n, err := strconv.ParseInt(parts[i], 10, 64)
		if err != nil {
			return 0
		}
		total += n * mult
	}
	return total
}

// isVirtualModel erkennt VirtualDSM (Synologys DSM als virtuelle Maschine).
func isVirtualModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "virtualdsm")
}

// joinNonEmpty verbindet die belegten Teile mit Leerzeichen.
func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
