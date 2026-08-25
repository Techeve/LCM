package domain

import "time"

// Dringlichkeitsstufen (Severity) eines Alarms bzw. eines
// Benachrichtigungs-Events. Sie steuern u.a. den Betreff-Präfix der
// E-Mail und lassen sich in Alarmregeln als Mindest-Schwere filtern.
// Reihenfolge = Schwere: critical > warning > info.
const (
	AlertSeverityInfo     = "info"
	AlertSeverityWarning  = "warning"
	AlertSeverityCritical = "critical"
)

// Kanal-Typen des Notification-Service. Das generische Provider-Modell
// erlaubt weitere Typen (SMS, …).
const (
	ChannelTypeEmail = "email"
	// ChannelTypeWebhook versendet Events per HTTPS-POST (generisches JSON
	// oder Microsoft-Teams-Format). Die Ziel-URL ist das Credential und
	// liegt verschlüsselt in SecretEnc.
	ChannelTypeWebhook = "webhook"
	// ChannelTypeSystemEmail ist der VERWALTETE Kanal des Standard-E-Mail-
	// Versands: keine eigene Konfiguration - der Provider liest die SMTP-
	// Einstellungen zur Sendezeit aus den GlobalSettings. Angelegt/geschaltet
	// über die Checkbox in Einstellungen → Allgemein, nicht manuell.
	ChannelTypeSystemEmail = "system_email"
)

// SystemEmailChannelName ist der feste Name des verwalteten Kanals.
const SystemEmailChannelName = "Standard-E-Mail (System)"

// NotificationChannel ist eine konfigurierte Instanz eines
// Benachrichtigungs-Providers (z.B. ein SMTP-Postausgang). Die
// nicht-sensible Konfiguration liegt als JSON in Config; sensible Werte
// (SMTP-Passwort) sind AES-GCM-verschlüsselt in SecretEnc abgelegt.
type NotificationChannel struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name string `gorm:"uniqueIndex;not null" json:"name"`
	Type string `gorm:"not null" json:"type"` // ChannelType*
	// Enabled ohne DB-Default: sonst verwirft GORM ein bewusst gesetztes
	// enabled=false beim Insert (Zero-Value-Falle).
	Enabled bool `json:"enabled"`

	// Config ist die provider-spezifische, nicht-sensible Konfiguration
	// als JSON (z.B. SMTP-Host, Port, Absender, Empfänger).
	Config string `gorm:"type:text" json:"config"`
	// SecretEnc hält den AES-GCM-verschlüsselten sensiblen Teil der
	// Konfiguration (beim E-Mail-Provider das SMTP-Passwort).
	SecretEnc string `json:"-"`
}

// NotificationEvent ist das standardisierte Ereignis-Objekt, das die
// Alerting-Engine an den Notification-Service übergibt. Der jeweilige
// Provider (hier E-Mail) übersetzt es in sein Zielformat.
type NotificationEvent struct {
	// Timestamp ist der Zeitpunkt der Auslösung.
	Timestamp time.Time `json:"timestamp"`
	// Severity ist die Dringlichkeitsstufe (AlertSeverity*).
	Severity string `json:"severity"`
	// ServerGroup ist die betroffene Servergruppe ("" = global/keine).
	ServerGroup string `json:"server_group"`
	// ServerName ist der betroffene Server ("" = nicht serverbezogen).
	ServerName string `json:"server_name"`
	// Code ist der maschinenlesbare Fehler-/Alarmcode (z.B. der Alarmtyp).
	Code string `json:"code"`
	// Description ist die Klartext-Beschreibung des Ereignisses.
	Description string `json:"description"`
	// Details enthält optionale Zusatzinformationen (mehrzeilig).
	Details string `json:"details"`
}
