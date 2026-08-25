package services

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"
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
	raw = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if raw == "" {
		return "", nil
	}
	if len(raw) > 255 {
		return "", fmt.Errorf("%w: höchstens 255 Zeichen", ErrPublicBaseURLInvalid)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPublicBaseURLInvalid, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: erwartet wird http:// oder https://", ErrPublicBaseURLInvalid)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: es fehlt der Hostname", ErrPublicBaseURLInvalid)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: Zugangsdaten in der Adresse sind nicht erlaubt", ErrPublicBaseURLInvalid)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: nur Schema, Host und Port - ohne Pfad, Query oder Anker", ErrPublicBaseURLInvalid)
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

// LinkBaseURL liefert die Basis-Adresse für Links in versendeten Mails.
//
// Rangfolge: konfigurierte PublicBaseURL → Rückfall aus der lokalen
// Konfiguration. NIEMALS aus dem Request: der Host-Header ist frei fälschbar,
// und ein Angreifer könnte damit einen echten Passwort-Reset-Link mit
// gültigem Token auf seine eigene Domain ausstellen lassen. Ist nichts
// konfiguriert, ist der Link im Zweifel nicht von außen erreichbar - das ist
// ein Betriebs-, kein Sicherheitsproblem, und die UI weist darauf hin.
func (s *SettingsService) LinkBaseURL() string {
	if cfg, err := s.settings.Get(); err == nil && cfg.PublicBaseURL != "" {
		return cfg.PublicBaseURL
	}
	return s.fallbackBaseURL
}

// WithFallbackBaseURL hinterlegt die aus der lokalen Konfiguration
// abgeleitete Basis-Adresse (Schema + Host + Port des eigenen Listeners).
func (s *SettingsService) WithFallbackBaseURL(u string) *SettingsService {
	s.fallbackBaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
	return s
}

// isLoopbackHost meldet, ob ein Bind-Host ausschließlich lokal erreichbar ist.
// Grundlage der MCP-Bind-Prüfung (siehe SetMCP).
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(strings.TrimSpace(host), "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
