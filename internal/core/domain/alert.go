package domain

import (
	"time"

	"gorm.io/gorm"
)

// Alarm-Typen (Monitoring- & Trigger-Kriterien). Jeder Typ wertet eine
// bestimmte Metrik gegen die in der Regel hinterlegten Schwellenwerte aus.
const (
	// AlertTypeDiskCapacity: harte Kapazitätsgrenze - Alarm, wenn die
	// Festplattenbelegung den prozentualen Schwellenwert erreicht.
	AlertTypeDiskCapacity = "disk_capacity"
	// AlertTypeStorageForecast: prädiktive Speicheranalyse - Alarm, wenn die
	// lineare Hochrechnung ergibt, dass das Limit innerhalb der Frist
	// (ForecastDays) überschritten wird.
	AlertTypeStorageForecast = "storage_forecast"
	// AlertTypeSecurityCVE: Sicherheitslücken - Alarm bei CVE-Funden ab der
	// konfigurierten Mindest-Schwere (MinSeverity).
	AlertTypeSecurityCVE = "security_cve"
	// AlertTypeFailedUpdates: Patch-Management - Alarm, wenn die Zahl
	// überfälliger Paket-Updates den Schwellenwert (MaxOutdated) übersteigt.
	AlertTypeFailedUpdates = "failed_updates"
	// AlertTypeHeartbeat: Downtime-/Loss-of-Signal-Überwachung - Alarm, wenn
	// der letzte Server-Kontakt länger als HeartbeatHours zurückliegt.
	AlertTypeHeartbeat = "heartbeat"
	// AlertTypeRebootRequired: Das System fordert selbst einen Neustart an
	// (z. B. nach Kernel-Update; Ubuntu/Debian: /var/run/reboot-required).
	// Reines Boolean-Kriterium ohne Schwellenwert.
	AlertTypeRebootRequired = "reboot_required"
	// AlertTypeAptCacherDown: der zentrale apt-cacher-ng-Dienst antwortet nicht
	// auf seiner Report-Seite. Selbstbeobachtung (IsSelfAlert) - der Dienst
	// existiert einmal, also wird einmal geprüft. Reines Boolean-Kriterium
	// ohne Schwellenwert.
	AlertTypeAptCacherDown = "apt_cacher_down"
	// AlertTypeCrowdSecLapiDown: die zentrale CrowdSec-LAPI (Einstellungen →
	// CrowdSec) antwortet nicht oder lehnt den hinterlegten Maschinen-Login
	// ab. Selbstbeobachtung (IsSelfAlert). Reines Boolean-Kriterium ohne
	// Schwellenwert.
	AlertTypeCrowdSecLapiDown = "crowdsec_lapi_down"
	// AlertTypeDeepScan: der letzte Deep Scan hat Warnungen/kritische Befunde
	// (Härtung/Fehlkonfiguration oder Kernel-Reboot-Lücke) ergeben. Reines
	// Boolean-Kriterium ohne Schwellenwert.
	AlertTypeDeepScan = "deep_scan"
	// AlertTypeCVEDBStale: die Schwachstellen-Datenbank des CVE-Scanners ist
	// überaltert (siehe CVEDBStaleAfter) oder wurde nie geladen.
	// Selbstbeobachtung (IsSelfAlert) - Scanner und Datenbank liegen zentral,
	// also wird einmal geprüft. Reines Boolean-Kriterium ohne Schwellenwert.
	//
	// Warum als eigener Alarm: Eine alte Datenbank meldet keinen Fehler,
	// sondern liefert weiterhin Ergebnisse - nur eben veraltete. Nach außen
	// sieht das aus wie „keine Sicherheitslücken". Ohne Alarm fiele das erst
	// auf, wenn jemand nachsieht.
	AlertTypeCVEDBStale = "cve_db_stale"
	// AlertTypeBackupStale: automatische Backups sind aktiviert, aber das
	// jüngste Backup ist deutlich älter als das Intervall erlaubt (oder es
	// existiert gar keines). Selbstbeobachtung (IsSelfAlert). Reines
	// Boolean-Kriterium ohne Schwellenwert.
	//
	// Warum als Zustands-Alarm statt „Job fehlgeschlagen": Im Langzeittest
	// fiel das Backup wochenlang aus, ohne dass irgendein Kanal es meldete -
	// mal als stiller Fehlversuch, mal fiel der Lauf ersatzlos aus (R2-028,
	// R2-034, R2-027). Ein Alarm auf das ALTER des jüngsten Backups fängt
	// alle diese Wege zugleich: was auch immer schiefgeht, das Ergebnis ist
	// ein fehlendes Backup, und genau das wird gemessen.
	AlertTypeBackupStale = "backup_stale"
	// AlertTypeAdvisory: Die Fruehwarnung (OSV) meldet offene Befunde zum
	// installierten Paketbestand ab der konfigurierten Mindest-Schwere
	// (MinSeverity).
	//
	// Warum neben security_cve ein eigener Typ: security_cve bewertet das
	// Ergebnis des taeglichen Trivy-Scans - eine gruendliche, aber langsame
	// Quelle. Die Fruehwarnung meldet, was Minuten alt ist, und sie kennt
	// Schadpakete (MAL-), die es in der Trivy-Datenbank gar nicht gibt. Wer
	// beides in einen Alarm zwaenge, muesste fuer beide dieselbe Schwelle und
	// dieselbe Sperrfrist waehlen - obwohl das eine „plane das Update ein"
	// heisst und das andere „sieh jetzt nach".
	//
	// Schadpakete zaehlen dabei IMMER, unabhaengig von der Schwelle: Die
	// Quellen fuehren fuer sie meist keine Schwere, und ein Volltreffer
	// duerfte nicht daran vorbeirutschen, dass ein Feld leer ist.
	AlertTypeAdvisory = "advisory"
)

// IsSelfAlert meldet, ob ein Alarm-Typ LCM SELBST bewertet statt eines
// verwalteten Servers: den Backup-Stand, die Schwachstellen-Datenbank des
// Scanners, den zentralen Paket-Cache und die CrowdSec-LAPI. Alle vier
// beschreiben genau eine Sache, die es genau einmal gibt.
//
// Warum das ein eigener Begriff sein muss: Bis v1.23 hingen diese vier an
// einem Servereintrag und begannen mit `if !server.IsLcmHost() { return }`.
// Das funktionierte nur, solange der eigene Rechner als Server aufgenommen
// war. Im Container nimmt LCM sich bewusst NICHT selbst auf (der localhost
// wäre der Container, nicht der Host) - dort lief die Bewertung für JEDEN
// Server ins Leere, und ein ausbleibendes Backup blieb dauerhaft unbemerkt.
// Wer den Eintrag von Hand löschte, erreichte dasselbe. Ein Alarm, den man
// durch Löschen eines unbeteiligten Datensatzes abschaltet, ist keiner.
//
// Servergruppen sind für diese Typen gegenstandslos: Es gibt nichts
// einzugrenzen, wenn das Geprüfte nur einmal existiert.
func IsSelfAlert(alertType string) bool {
	switch alertType {
	case AlertTypeBackupStale, AlertTypeCVEDBStale,
		AlertTypeAptCacherDown, AlertTypeCrowdSecLapiDown:
		return true
	default:
		return false
	}
}

// Standard-Schwellenwerte, falls in der Regel nichts (0) hinterlegt ist.
const (
	DefaultDiskCapacityPercent  = 90
	DefaultForecastDays         = 10
	DefaultMaxOutdated          = 0
	DefaultHeartbeatHours       = 24
	DefaultAlertCooldownMinutes = 360 // 6h - verhindert Alarm-Spam
)

// AlertRule ist eine Alarm-Regel: sie bindet ein Monitoring-Kriterium an
// (optional) Servergruppen und einen Benachrichtigungskanal. Ist keine
// Gruppe gesetzt, gilt die Regel für alle Server - so lassen sich Regeln
// granular je Infrastruktur-Gruppe konfigurieren (z.B. strenger für
// produktive Datenbanken, lockerer für Staging).
type AlertRule struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name string `gorm:"not null" json:"name"`
	Type string `gorm:"not null" json:"type"` // AlertType*
	// Enabled ohne DB-Default: sonst würde GORM ein bewusst gesetztes
	// enabled=false beim Insert verwerfen (Zero-Value-Falle).
	Enabled bool `json:"enabled"`

	// Groups grenzt die Regel auf Servergruppen ein (leer = alle Server).
	// Mehrere Gruppen, weil dieselbe Schwelle typischerweise für mehrere
	// Infrastruktur-Gruppen gilt - vorher war dafür je Gruppe eine eigene,
	// ansonsten identische Regel nötig.
	Groups []ServerGroup `gorm:"many2many:alert_rule_groups" json:"groups"`

	// ChannelID ist der Benachrichtigungskanal, über den ausgelöste Alarme
	// gemeldet werden (nil = nur protokollieren, nicht benachrichtigen).
	ChannelID *uint `gorm:"index" json:"channel_id"`

	// Severity ist die Dringlichkeitsstufe des ausgelösten Events
	// (AlertSeverity*); Default: warning.
	Severity string `gorm:"default:warning" json:"severity"`

	// --- Schwellenwert-Matrix (je nach Typ relevant) ---
	// ThresholdPercent: Belegungs-Obergrenze in Prozent (disk_capacity).
	ThresholdPercent int `json:"threshold_percent"`
	// ForecastDays: Frist in Tagen (storage_forecast).
	ForecastDays int `json:"forecast_days"`
	// MaxOutdated: erlaubte Zahl überfälliger Updates (failed_updates).
	MaxOutdated int `json:"max_outdated"`
	// MinSeverity: Mindest-CVE-Schwere (security_cve), Werte wie Severity*.
	MinSeverity string `json:"min_severity"`
	// HeartbeatHours: Timeout für Loss-of-Signal (heartbeat).
	HeartbeatHours int `json:"heartbeat_hours"`

	// CooldownMinutes verhindert wiederholte Benachrichtigungen für denselben
	// Server/dieselbe Regel innerhalb des Zeitfensters. Genau 0 heißt „KEINE
	// Sperre" (jede Auswertung meldet erneut) - vorher wurde 0 still als
	// 6-Stunden-Vorgabe ausgelegt, also das Gegenteil des naheliegenden
	// Sinns (R2-063). Die 6-Stunden-Vorgabe belegt die UI/der Konstruktor
	// beim ANLEGEN vor; sie ist damit ein sichtbarer Wert, keine versteckte
	// Umdeutung der Null.
	CooldownMinutes int `json:"cooldown_minutes"`
}

// Cooldown liefert die effektive Cooldown-Dauer. Genau 0 = keine Sperre.
// Negative Werte (nie über die API erreichbar) fallen auf die Vorgabe.
func (r *AlertRule) Cooldown() time.Duration {
	m := r.CooldownMinutes
	if m < 0 {
		m = DefaultAlertCooldownMinutes
	}
	return time.Duration(m) * time.Minute
}

// DiskThreshold liefert den effektiven Kapazitäts-Schwellenwert in Prozent.
func (r *AlertRule) DiskThreshold() int {
	if r.ThresholdPercent <= 0 {
		return DefaultDiskCapacityPercent
	}
	return r.ThresholdPercent
}

// ForecastThresholdDays liefert die effektive Prognose-Frist in Tagen.
func (r *AlertRule) ForecastThresholdDays() int {
	if r.ForecastDays <= 0 {
		return DefaultForecastDays
	}
	return r.ForecastDays
}

// HeartbeatTimeout liefert den effektiven Loss-of-Signal-Timeout.
func (r *AlertRule) HeartbeatTimeout() time.Duration {
	h := r.HeartbeatHours
	if h <= 0 {
		h = DefaultHeartbeatHours
	}
	return time.Duration(h) * time.Hour
}

// MinCVESeverity liefert die effektive Mindest-CVE-Schwere (Default: high).
func (r *AlertRule) MinCVESeverity() string {
	if r.MinSeverity == "" {
		return SeverityHigh
	}
	return r.MinSeverity
}

// AlertEvent ist ein ausgelöster Alarm (Historie). Er dient zugleich der
// Cooldown-Entprellung: der jüngste Event je (Regel, Server) bestimmt, ob
// erneut benachrichtigt wird.
//
// UUID-PK wie bei allen Tabellen mit unbegrenzt wachsendem Bestand
// (Jobs, Audit, SSH-Protokolle, …) - Ganzzahl-Sequenzen können hier
// prinzipiell nicht "ausgehen".
type AlertEvent struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID (seit v0.10.0)
	CreatedAt time.Time `gorm:"index" json:"created_at"`

	RuleID   uint  `gorm:"index:idx_alert_rule_server" json:"rule_id"`
	ServerID *uint `gorm:"index:idx_alert_rule_server" json:"server_id"`

	RuleName   string `json:"rule_name"`
	ServerName string `json:"server_name"`
	GroupName  string `json:"group_name"`

	Type        string `json:"type"`     // AlertType*
	Severity    string `json:"severity"` // AlertSeverity*
	Code        string `json:"code"`
	Description string `json:"description"`

	// Notified meldet, ob die Benachrichtigung erfolgreich versandt wurde;
	// NotifyError hält andernfalls den Fehlertext.
	Notified    bool   `json:"notified"`
	NotifyError string `json:"notify_error"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (e *AlertEvent) BeforeCreate(*gorm.DB) error {
	if e.ID == "" {
		e.ID = newUUID()
	}
	return nil
}
