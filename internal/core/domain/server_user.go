package domain

import (
	"time"

	"gorm.io/gorm"
)

// Passwort-Status eines gescannten Linux-Kontos (ServerUser.PasswordStatus).
const (
	// PasswordStatusSet: ein nutzbares Passwort ist gesetzt.
	PasswordStatusSet = "set"
	// PasswordStatusLocked: ein Passwort existiert, ist aber gesperrt
	// (usermod -L) - Anmeldung nur noch per SSH-Key möglich.
	PasswordStatusLocked = "locked"
	// PasswordStatusNone: kein Passwort gesetzt - Anmeldung nur per SSH-Key.
	PasswordStatusNone = "none"
	// PasswordStatusUnknown: /etc/shadow war beim Scan nicht lesbar.
	PasswordStatusUnknown = "unknown"
)

// ServerUser ist ein beim Scan erfasstes anmeldefähiges Linux-Konto auf einem
// verwalteten Server - der IST-Zustand des Zielsystems, bewusst getrennt vom
// SOLL (LinuxUser, das von LCM verteilte Konto). Systemkonten (UID unterhalb
// von UID_MIN, nologin/false-Shell) filtert der Scan heraus; root erscheint,
// weil es das wichtigste anmeldefähige Konto ist.
type ServerUser struct {
	ID       uint `gorm:"primarykey" json:"id"`
	ServerID uint `gorm:"index" json:"server_id"`

	// Username liegt wie bei LinuxUser AES-256-GCM-verschlüsselt at rest -
	// die gescannten Namen sind dieselben Konten, die LCM verteilt; sie hier
	// im Klartext zu speichern höbe deren Verschlüsselung auf.
	Username string `gorm:"serializer:aesgcm" json:"username"`
	UID      int    `json:"uid"`
	Shell    string `json:"shell"`
	// PasswordStatus: set | locked | none (siehe Konstanten oben).
	PasswordStatus string `json:"password_status"`
	// SSHKeyCount: Einträge in ~/.ssh/authorized_keys.
	SSHKeyCount int `json:"ssh_key_count"`
	// TwoFactorEnrolled: ~/.google_authenticator vorhanden - der Benutzer hat
	// TOTP für den SSH-Login eingerichtet (siehe SSH-2FA-Option).
	TwoFactorEnrolled bool `json:"two_factor_enrolled"`
	// Disabled: das Konto ist abgelaufen (shadow-Feld expire) - kein Login
	// mehr möglich, auch nicht per SSH-Key. Das unterscheidet sich vom bloß
	// gesperrten Passwort: usermod -L lässt Key-Logins zu.
	Disabled bool `json:"disabled"`
	// LastLoginAt: letzter Login laut lastlog/lastlog2 (nil = nie oder auf
	// dem System nicht ermittelbar - best effort).
	LastLoginAt *time.Time `json:"last_login_at"`

	// Managed wird zur Laufzeit gesetzt: das Konto entspricht einem von LCM
	// verteilten LinuxUser dieses Servers.
	Managed bool `gorm:"-" json:"managed"`
	// Blocked: auf DIESEM Server gesperrt (siehe ServerUserBlock). Nur für
	// verteilte Konten bedeutsam - unverwaltete sind schlicht deaktiviert.
	Blocked bool `gorm:"-" json:"blocked"`
	// LoginCount ist die Zahl der erfassten Anmeldungen (siehe ServerUserLogin).
	LoginCount int `gorm:"-" json:"login_count"`
	// LoginsFromLCM: Die Anmeldungen dieses Kontos stammen aus LCMs eigenem
	// Sitzungsprotokoll statt aus wtmp. Das betrifft den LCM-Zugangsbenutzer:
	// LCM meldet sich ohne Terminal an, und dafür schreibt sshd weder wtmp
	// noch lastlog - ohne diese Quelle stünde das Konto ewig auf „nie
	// angemeldet", obwohl es das meistgenutzte des Servers ist.
	LoginsFromLCM bool `gorm:"-" json:"logins_from_lcm"`
}

// KeyOnly meldet, ob sich dieses Konto nur noch per SSH-Key anmelden kann
// (kein nutzbares Passwort, aber mindestens ein Key hinterlegt).
func (u *ServerUser) KeyOnly() bool {
	return u.PasswordStatus != PasswordStatusSet && u.SSHKeyCount > 0
}

// ServerUserBlock hält fest, dass ein von LCM verwaltetes Konto auf EINEM
// bestimmten Server gesperrt ist.
//
// Warum eine eigene Tabelle und kein Feld an der Zuordnung: Ein Benutzer kann
// direkt ODER über eine Servergruppe an einen Server kommen - im zweiten Fall
// gibt es gar keine Zuordnungszeile, an die sich etwas hängen ließe. Die
// Sperre gilt unabhängig vom Weg.
//
// Ohne diese Merkung wäre die Sperre wirkungslos: Der nächste Benutzer-Sync
// provisioniert das Konto wieder und entsperrt es dabei - der Betreiber hätte
// gesperrt, LCM hätte es stillschweigend rückgängig gemacht.
type ServerUserBlock struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	ServerID uint `gorm:"index:idx_server_user_block,unique" json:"server_id"`
	// Username verschlüsselt wie überall; der Blindindex macht ihn suchbar.
	Username     string `gorm:"serializer:aesgcm" json:"username"`
	UsernameBIdx string `gorm:"column:username_bidx;index:idx_server_user_block,unique" json:"-"`

	// Actor hält fest, wer gesperrt hat (fürs Protokoll).
	Actor string `json:"actor"`
}

// BeforeSave hält den Blindindex synchron (wie bei LinuxUser).
func (b *ServerUserBlock) BeforeSave(*gorm.DB) error {
	if b.Username != "" {
		b.UsernameBIdx = UserBlindIndex(b.Username)
	}
	return nil
}

// ServerUserLogin ist eine einzelne Anmeldung am Server, erhoben aus wtmp
// (`last`). Damit lässt sich nicht nur „zuletzt angemeldet" zeigen, sondern
// auch, wie oft und von wo jemand da war.
//
// Reichweite: wtmp wird vom System rotiert (üblicherweise monatlich). Die
// Historie reicht deshalb nur so weit zurück, wie die aktuelle Datei reicht -
// das ist eine Eigenschaft des Systems, kein Fehler von LCM, und die
// Oberfläche sagt es auch.
type ServerUserLogin struct {
	ID       uint `gorm:"primarykey" json:"id"`
	ServerID uint `gorm:"index" json:"server_id"`

	Username     string `gorm:"serializer:aesgcm" json:"username"`
	UsernameBIdx string `gorm:"column:username_bidx;index" json:"-"`

	// FromHost ist die Herkunft (IP oder Hostname); leer bei lokaler Konsole.
	FromHost string `gorm:"serializer:aesgcm" json:"from_host"`
	// TTY unterscheidet SSH (pts/…) von der Konsole (tty…).
	TTY string `json:"tty"`

	StartedAt time.Time `gorm:"index" json:"started_at"`
	// EndedAt ist nil, solange die Sitzung läuft.
	EndedAt *time.Time `json:"ended_at"`
	// StillActive: die Sitzung lief zum Zeitpunkt der Erhebung noch.
	StillActive bool `json:"still_active"`
}

// BeforeSave hält den Blindindex synchron.
func (l *ServerUserLogin) BeforeSave(*gorm.DB) error {
	if l.Username != "" {
		l.UsernameBIdx = UserBlindIndex(l.Username)
	}
	return nil
}

// DurationMinutes liefert die Sitzungsdauer in Minuten (0 = unbekannt/laufend).
func (l *ServerUserLogin) DurationMinutes() int {
	if l.EndedAt == nil {
		return 0
	}
	return int(l.EndedAt.Sub(l.StartedAt).Minutes())
}
