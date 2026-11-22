package services

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/sniroute"
)

// Validierung der globalen Einstellungen. Bewusst streng und an EINER Stelle:
// die Werte landen in Shell-Skripten, Dateipfaden, Mail-Headern und Links.

var (
	// ErrSettingInvalid ist der Sammelfehler für abgelehnte Einstellungswerte.
	// Die Controller antworten darauf mit 4xx statt mit einem Serverfehler.
	//
	// Jeder speziellere Validierungsfehler WICKELT ihn ein. Das ist keine
	// Kosmetik: Vorher stand er nur als einer von mehreren Werten in einer
	// Positivliste im Controller - wer einen neuen Validierungsfehler
	// einführte und die Liste nicht mitpflegte, bekam für eine schlichte
	// Fehleingabe „interner Serverfehler". Genau so geschehen beim
	// Backup-Verzeichnis: Der Betreiber sah einen internen Fehler statt der
	// Auskunft, dass ein absoluter Pfad erwartet wird.
	ErrSettingInvalid = errors.New("ungültiger einstellungswert")
	// ErrPublicBaseURLInvalid: die öffentliche Basis-Adresse ist unbrauchbar.
	ErrPublicBaseURLInvalid = fmt.Errorf("%w: ungültige öffentliche basis-adresse", ErrSettingInvalid)
	// ErrPublicAgentURLInvalid / ErrPublicMCPURLInvalid: die öffentliche
	// Adresse des Agent-Ports bzw. der MCP-Schnittstelle ist unbrauchbar.
	ErrPublicAgentURLInvalid = fmt.Errorf("%w: ungültige öffentliche agent-adresse", ErrSettingInvalid)
	ErrPublicMCPURLInvalid   = fmt.Errorf("%w: ungültige öffentliche mcp-adresse", ErrSettingInvalid)
	// ErrPublicHostConflict: zwei Dienste haben auf dem gemeinsamen Port
	// denselben Namen. Der Fehler kommt als *PublicHostConflict.
	ErrPublicHostConflict = fmt.Errorf("%w: zwei Dienste haben denselben namen", ErrSettingInvalid)
	// ErrBackupDirInvalid: das Backup-Verzeichnis ist unbrauchbar.
	ErrBackupDirInvalid = fmt.Errorf("%w: ungültiges backup-verzeichnis", ErrSettingInvalid)
)

// validatePort prüft einen TCP-Port auf den gültigen Bereich.
func validatePort(port int, field string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: %s muss zwischen 1 und 65535 liegen (war %d)", ErrSettingInvalid, field, port)
	}
	return nil
}

// validateRange prüft einen ganzzahligen Wert auf einen erlaubten Bereich.
func validateRange(v, min, max int, field string) error {
	if v < min || v > max {
		return fmt.Errorf("%w: %s muss zwischen %d und %d liegen (war %d)", ErrSettingInvalid, field, min, max, v)
	}
	return nil
}

// normalizePublicBaseURL prüft die von außen erreichbare Basis-Adresse.
// Erlaubt ist ausschließlich "http(s)://host[:port]" - kein Pfad, keine
// Query, kein Fragment und KEINE eingebetteten Zugangsdaten (ein
// "https://opfer.de@angreifer.tld" würde sonst beim Klick auf angreifer.tld
// landen). Leer = nicht gesetzt (zulässig, siehe LinkBaseURL).
func normalizePublicBaseURL(raw string) (string, error) {
	return normalizePublicURL(raw, ErrPublicBaseURLInvalid)
}

// normalizePublicURL prüft eine öffentliche Adresse wie normalizePublicBaseURL;
// invalid ist der Fehler, den der Aufrufer für sein Feld meldet.
func normalizePublicURL(raw string, invalid error) (string, error) {
	raw = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if raw == "" {
		return "", nil
	}
	if len(raw) > 255 {
		return "", fmt.Errorf("%w: höchstens 255 Zeichen", invalid)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", invalid, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: erwartet wird http:// oder https://", invalid)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: es fehlt der Hostname", invalid)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: Zugangsdaten in der Adresse sind nicht erlaubt", invalid)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: nur Schema, Host und Port - ohne Pfad, Query oder Anker", invalid)
	}
	return u.Scheme + "://" + u.Host, nil
}

// normalizeBackupDir prüft das Zielverzeichnis der Backups. Ein Backup
// enthält die vollständige Datenbank samt Master-Key - es darf nicht in ein
// vom Webserver ausgeliefertes oder anderweitig geteiltes Verzeichnis
// wandern. Erzwungen wird ein absoluter, bereinigter Pfad ohne
// Sonderzeichen; leer = Standard (siehe BackupService.backupDir).
func normalizeBackupDir(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 4096 {
		return "", fmt.Errorf("%w: Pfad ist zu lang", ErrBackupDirInvalid)
	}
	if strings.ContainsAny(raw, "\x00\n\r\t\"'`$;|&<>*?") {
		return "", fmt.Errorf("%w: der Pfad enthält unzulässige Zeichen", ErrBackupDirInvalid)
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%w: es wird ein absoluter Pfad erwartet (z.B. /var/lib/lcm/backups)", ErrBackupDirInvalid)
	}
	clean := filepath.Clean(raw)
	// Nach dem Bereinigen darf kein ".."-Segment übrig sein.
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		if seg == ".." {
			return "", fmt.Errorf("%w: \"..\" ist im Pfad nicht erlaubt", ErrBackupDirInvalid)
		}
	}
	return clean, nil
}

// normalizeBackupTime prüft die Anker-Uhrzeit des Backup-Zeitplans (HH:MM).
// Leer ist erlaubt (= Vorgabe DefaultBackupTime); alles andere muss eine
// gültige Uhrzeit sein - der Wert landet in einem Cron-Ausdruck, ein
// Tippfehler verschöbe den Zeitplan sonst still auf den @every-Fallback.
func normalizeBackupTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return "", fmt.Errorf("%w: backup_time %q - erwartet HH:MM (24-Stunden-Format, z.B. 03:30)", ErrSettingInvalid, raw)
	}
	return t.Format("15:04"), nil
}

// normalizeRequire2FARoles bereinigt die Rollenliste der 2FA-Pflicht und
// prüft JEDEN Namen gegen die tatsächlich vorhandenen Rollen. Ohne diese
// Prüfung machte ein Tippfehler („Admin", „admins") die 2FA-Pflicht lautlos
// wirkungslos - der gefährlichste Fehlerfall, weil er wie „aktiv" aussieht.
func (s *SettingsService) normalizeRequire2FARoles(raw string) (string, error) {
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.ToLower(strings.TrimSpace(p))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return "", nil
	}
	if s.roles != nil {
		known, err := s.roles.FindAll()
		if err != nil {
			return "", err
		}
		valid := map[string]bool{}
		for _, r := range known {
			valid[strings.ToLower(r.Name)] = true
		}
		for _, name := range out {
			if !valid[name] {
				return "", fmt.Errorf("%w: unbekannte Rolle %q in require_2fa_roles", ErrSettingInvalid, name)
			}
		}
	}
	return strings.Join(out, ","), nil
}

// PublicHostConflict sagt, welche zwei Dienste sich auf dem gemeinsamen Port
// einen Namen teilen.
type PublicHostConflict struct {
	Host          string
	First, Second string // UI, Agent oder MCP
}

func (c *PublicHostConflict) Error() string {
	return fmt.Sprintf("%v: %s steht für %s und %s - auf dem gemeinsamen Port braucht jeder Dienst einen eigenen Namen",
		ErrPublicHostConflict, c.Host, c.First, c.Second)
}

func (c *PublicHostConflict) Unwrap() error { return ErrPublicHostConflict }

// FindPublicHostConflict nennt die ersten zwei Dienste, deren öffentliche
// Adressen auf demselben Namen liegen, in der Rangfolge UI, Agent, MCP (so
// entscheidet auch die Namensweiche des gemeinsamen Ports). MCP zählt nur,
// solange es eingeschaltet ist; leere Adressen zählen nicht.
func FindPublicHostConflict(uiURL, agentURL, mcpURL string, mcpEnabled bool) *PublicHostConflict {
	endpoints := []struct {
		name, url string
		active    bool
	}{
		{"UI", uiURL, true},
		{"Agent", agentURL, true},
		{"MCP", mcpURL, mcpEnabled},
	}
	owners := map[string]string{}
	for _, ep := range endpoints {
		host := sniroute.HostOf(ep.url)
		if host == "" || !ep.active {
			continue
		}
		if first, taken := owners[host]; taken {
			return &PublicHostConflict{Host: host, First: first, Second: ep.name}
		}
		owners[host] = ep.name
	}
	return nil
}

// checkSharedPortHosts lehnt öffentliche Adressen ab, die sich auf dem
// gemeinsamen Port einen Namen teilen: Der Name entscheidet dort, welcher
// Dienst die Verbindung bekommt. Im Modus C ist nur der Name der Oberfläche
// Pflicht; Agent und MCP dürfen ohne Namen bleiben, sind dann aber aus (siehe
// settleServiceNames). Gesetzte Namen müssen verschieden sein. Ohne gemeinsamen
// Port ist derselbe Name erlaubt.
func (s *SettingsService) checkSharedPortHosts(st *domain.GlobalSettings) error {
	if !s.AddressesOf(st).SharedPortActive() {
		return nil
	}
	if sniHost(st.PublicBaseURL) == "" {
		return fmt.Errorf("%w: auf dem gemeinsamen Port braucht die Oberfläche einen Namen - die Adresse für die Oberfläche fehlt",
			ErrSettingInvalid)
	}
	if conflict := FindPublicHostConflict(st.PublicBaseURL, st.PublicAgentURL, st.PublicMCPURL, true); conflict != nil {
		return conflict
	}
	return nil
}

// errServiceNeedsName: Im Modus C hat der Dienst keinen Namen, unter dem er
// erreichbar wäre, also lässt er sich nicht einschalten.
func errServiceNeedsName(service string) error {
	return fmt.Errorf("%w: für %s ist keine Domain eingerichtet - im Modus „ein Port“ lässt er sich erst mit einem eigenen Namen einschalten",
		ErrSettingInvalid, service)
}

// LinkBaseURL liefert die Basis-Adresse für Links in versendeten Mails.
//
// Rangfolge: die wirksame Adresse der Oberfläche (Addresses: im Modus A die
// PublicBaseURL, sonst aus Name und Port abgeleitet) → Rückfall aus der lokalen
// Konfiguration, auch wenn die Ableitung nichts ergibt (Modus C ohne Namen,
// Modus B ohne Host): Ein Mail-Link ist nie relativ. NIEMALS aus dem Request:
// der Host-Header ist frei fälschbar, und ein Angreifer könnte damit einen
// echten Passwort-Reset-Link mit gültigem Token auf seine eigene Domain
// ausstellen lassen. Ist nichts konfiguriert, ist der Link im Zweifel nicht von
// außen erreichbar - das ist ein Betriebs-, kein Sicherheitsproblem, und die UI
// weist darauf hin.
func (s *SettingsService) LinkBaseURL() string {
	a, err := s.Addresses()
	if err != nil {
		return urlFor(s.scheme(), s.addr.FallbackHost, s.fixedOrDefaultUIPort())
	}
	if a.URLs.UI != "" {
		return a.URLs.UI
	}
	return urlFor(s.scheme(), s.addr.FallbackHost, a.UIPort)
}

// fixedOrDefaultUIPort ist der Port der Oberfläche, wenn die Einstellungen
// nicht lesbar sind: der aus config.json oder Umgebung, sonst die Vorgabe.
func (s *SettingsService) fixedOrDefaultUIPort() int {
	return pickPort(s.addr.UI, 0, domain.DefaultUIPort)
}

// PublicAgentURL liefert die Adresse des Agent-Ports, wenn sie feststeht: im
// Modus A die eingestellte öffentliche Adresse, in B und C die abgeleitete.
// Leer heißt: keine bekannt - die Oberfläche nimmt dann den Host, unter dem sie
// selbst aufgerufen wird, mit dem Agent-Port. Einstellungen nicht lesbar: leer.
func (s *SettingsService) PublicAgentURL() string {
	st, err := s.settings.Get()
	if err != nil {
		return ""
	}
	a := s.AddressesOf(st)
	if a.Mode == domain.AddressModeSeparate && st.PublicAgentURL == "" {
		return ""
	}
	return a.URLs.Agent
}

// AgentURLNeedsProxy meldet, ob die Agent-Adresse ohne Port auf 443 zeigt,
// obwohl dort kein gemeinsamer Port von LCM lauscht (Modus A mit eigenem
// Namen ohne Port). Das geht nur mit einem Reverse Proxy auf 443, der an den
// Agent-Port weiterreicht; sonst läuft der Enroll-Befehl ins Leere.
func (s *SettingsService) AgentURLNeedsProxy() bool {
	st, err := s.settings.Get()
	if err != nil || st.PublicAgentURL == "" {
		return false
	}
	a := s.AddressesOf(st)
	if a.Mode != domain.AddressModeSeparate || a.AgentPort == 0 || a.AgentPort == 443 {
		return false
	}
	u, err := url.Parse(st.PublicAgentURL)
	return err == nil && u.Scheme == "https" && u.Port() == ""
}

// AgentPort liefert den wirksamen Port des Agent-Listeners (0 = abgeschaltet,
// ausgeschaltet oder von LCM_AGENT_PORT=0 gesperrt).
func (s *SettingsService) AgentPort() int {
	if a, err := s.Addresses(); err == nil {
		return a.AgentListenPort()
	}
	return 0
}
