package domain

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// LinuxUser ist ein Betriebssystem-Benutzer, den LCM auf den verwalteten
// Servern anlegt und pflegt - bewusst getrennt von den LCM-Login-Benutzern
// (siehe User). Ein LinuxUser wird Servern oder Servergruppen zugeordnet;
// der Sync-Prozess legt den Account auf den Zielsystemen an und verteilt
// dessen SSH-Public-Keys (authorized_keys), sodass sich der Benutzer direkt
// per Zertifikat auf den Linux-Servern anmelden kann.
type LinuxUser struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Username/FullName/Email liegen at rest AES-256-GCM-verschlüsselt
	// (`aesgcm`-Serializer). Die Eindeutigkeit/Suche des Account-Namens sichert
	// der deterministische UsernameBIdx (Unique-Index in storage.Migrate).
	Username     string `gorm:"not null;serializer:aesgcm" json:"username"` // Linux-Account-Name
	UsernameBIdx string `gorm:"column:username_bidx" json:"-"`
	FullName     string `gorm:"serializer:aesgcm" json:"full_name"` // GECOS/Kommentar
	Email        string `gorm:"serializer:aesgcm" json:"email"`     // für den Aktivierungslink
	Shell        string `gorm:"default:/bin/bash" json:"shell"`
	// Sudo richtet passwortloses sudo für den Account ein.
	//
	// Abgeleiteter Wert: Maßgeblich ist das Berechtigungsprofil. Sudo wird
	// beim Speichern aus ihm gesetzt und bleibt erhalten, damit bestehende
	// API-Clients weiterlaufen.
	Sudo   bool `gorm:"default:false" json:"sudo"`
	Active bool `gorm:"default:true" json:"active"`

	// DefaultProfileID ist das Berechtigungsprofil, das für diesen Benutzer
	// gilt, wenn an der einzelnen Zuweisung keines gesetzt ist. NULL heißt:
	// keine Root-Rechte (wie das mitgelieferte Standardprofil).
	DefaultProfileID *uint             `gorm:"index" json:"default_profile_id"`
	DefaultProfile   *PrivilegeProfile `gorm:"foreignKey:DefaultProfileID" json:"default_profile,omitempty"`

	// PasswordEnc ist das AES-GCM-verschlüsselte Login-Passwort des Linux-
	// Accounts (optional, vom Mitarbeiter per Aktivierungslink gesetzt). Es
	// wird beim Sync via chpasswd auf den Servern gesetzt. HasPassword
	// signalisiert dem Frontend, ob ein Passwort hinterlegt ist.
	PasswordEnc string `json:"-"`
	HasPassword bool   `gorm:"-" json:"has_password"`

	SSHKeys []LinuxUserSSHKey `gorm:"foreignKey:LinuxUserID" json:"ssh_keys,omitempty"`
	// Direkt zugeordnete Server + Zuordnung über Servergruppen.
	Servers []Server      `gorm:"many2many:server_linux_users" json:"servers,omitempty"`
	Groups  []ServerGroup `gorm:"many2many:server_group_linux_users" json:"groups,omitempty"`
}

// BeforeSave hält den Username-Blindindex synchron zum verschlüsselten Namen
// (Guard wie bei Server: nur bei gesetztem Username, damit feldweise Updates
// den Index nicht leeren). Nutzt UserBlindIndex - dieselbe HMAC-Ableitung.
func (u *LinuxUser) BeforeSave(*gorm.DB) error {
	if u.Username != "" {
		u.UsernameBIdx = UserBlindIndex(u.Username)
	}
	return nil
}

// LinuxUserActivation ist ein zeitlich begrenzter Einmal-Link, über den ein
// Linux-Benutzer selbst seine Credentials setzt (Passwort und/oder
// SSH-Public-Key). Gespeichert wird nur der SHA-256-Hash des Tokens.
type LinuxUserActivation struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	LinuxUserID uint       `gorm:"not null;index" json:"linux_user_id"`
	TokenHash   string     `gorm:"uniqueIndex;not null" json:"-"`
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	ConsumedAt  *time.Time `json:"consumed_at"`
}

// Usable meldet, ob der Aktivierungslink noch einlösbar ist.
func (a *LinuxUserActivation) Usable(now time.Time) bool {
	return a.ConsumedAt == nil && now.Before(a.ExpiresAt)
}

// LinuxUserSSHKey ist ein öffentlicher SSH-Schlüssel eines LinuxUsers.
type LinuxUserSSHKey struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	LinuxUserID uint   `gorm:"not null;index" json:"linux_user_id"`
	Name        string `gorm:"not null" json:"name"`       // z.B. "Laptop", "YubiKey"
	PublicKey   string `gorm:"not null" json:"public_key"` // OpenSSH-Format
}

// ReservedLinuxUsernames sind Namen, die LCM als Linux-Benutzer NICHT anlegt:
// UID-0-Konten, LCMs eigener Management-Benutzer (lcm-svc) und typische
// Systemkonten. Sie über LCM zu provisionieren würde bestehende Konten
// überschreiben oder LCMs eigenen Zugang gefährden (R2-043).
var ReservedLinuxUsernames = map[string]bool{
	"root": true, "lcm": true, DefaultServiceUser: true,
	"daemon": true, "bin": true, "sys": true, "sync": true, "games": true,
	"man": true, "lp": true, "mail": true, "news": true, "uucp": true,
	"proxy": true, "www-data": true, "backup": true, "list": true, "irc": true,
	"nobody": true, "systemd-network": true, "sshd": true, "messagebus": true,
}

// IsReservedLinuxUsername meldet, ob der Name für LCM tabu ist.
func IsReservedLinuxUsername(name string) bool {
	return ReservedLinuxUsernames[strings.ToLower(strings.TrimSpace(name))]
}

// ValidLinuxUsername prüft, ob der Name ein gültiger Linux-Account-Name ist
// (useradd-Konvention: beginnt mit Buchstabe/Unterstrich, danach
// Kleinbuchstaben, Ziffern, Unterstrich, Bindestrich; max. 32 Zeichen).
func ValidLinuxUsername(name string) bool {
	if len(name) == 0 || len(name) > 32 {
		return false
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c == '_':
			// immer erlaubt
		case (c >= '0' && c <= '9') || c == '-':
			if i == 0 {
				return false // darf nicht mit Ziffer/Bindestrich beginnen
			}
		default:
			return false
		}
	}
	return true
}

// ValidLinuxShell prüft, ob der Wert ein plausibler absoluter Shell-Pfad ist
// (beginnt mit "/", nur harmlose Pfad-Zeichen). Der Wert fließt beim
// Provisionieren in ein `useradd -s <shell>`-Kommando; ohne diese Prüfung
// ließe sich darüber `x$(befehl)` o.Ä. als root auf den Zielservern
// ausführen (Command Injection).
func ValidLinuxShell(shell string) bool {
	if len(shell) < 2 || len(shell) > 128 || shell[0] != '/' {
		return false
	}
	for _, c := range shell {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '/' || c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}
