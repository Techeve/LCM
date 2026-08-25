// Package domain enthält die Entitäten (GORM-Models) der Anwendung.
// Die Schicht ist frei von HTTP- und Datenbank-Logik - nur Datenstrukturen
// und domänennahe Hilfsmethoden.
package domain

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Reservierte System-Usernamen, die beim Seeding angelegt werden.
const (
	SystemUsername = "system" // für Prozesse ohne User-Kontext
	AdminUsername  = "admin"  // voller Zugriff
)

// UserBlindIndex berechnet den deterministischen Blindindex für die
// verschlüsselten, aber eindeutig/suchbar gehaltenen Benutzer-Felder
// (Username, E-Mail). Standard: normalisierter Klartext (Tests/ohne Cipher);
// die repositories-Ebene ersetzt die Funktion beim Start durch die
// HMAC-Variante (aus dem Master-Key). So bleibt domain krypto-frei.
var UserBlindIndex = func(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// User ist ein Anwendungsbenutzer. Das Passwort wird ausschließlich
// als argon2id-Hash gespeichert.
//
// At-Rest: Username, E-Mail, Vor-/Nachname und der Passwort-Hash liegen
// AES-256-GCM-verschlüsselt in der DB (transparent über den `aesgcm`-GORM-
// Serializer). Da GCM pro Schreibvorgang zufällig ist, sichern deterministische
// Blindindizes (UsernameBIdx/EmailBIdx) Eindeutigkeit und Login-/E-Mail-Suche.
type User struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Username  string    `gorm:"not null;serializer:aesgcm" json:"username"`
	// UsernameBIdx ist der HMAC-Blindindex des Usernamens (Unique-Index in
	// storage.Migrate, nicht im Tag - erst befüllen, dann eindeutig indexieren).
	UsernameBIdx string `gorm:"column:username_bidx" json:"-"`
	// Email ist optional; Eindeutigkeit erzwingt ein partieller Unique-Index
	// auf EmailBIdx (nur nicht-leere Werte) in storage.Migrate.
	Email string `gorm:"serializer:aesgcm" json:"email"`
	// EmailBIdx ist der HMAC-Blindindex der (klein geschriebenen) E-Mail.
	EmailBIdx    string     `gorm:"column:email_bidx" json:"-"`
	PasswordHash string     `gorm:"not null;serializer:aesgcm" json:"-"` // niemals serialisieren
	FirstName    string     `gorm:"serializer:aesgcm" json:"first_name"`
	LastName     string     `gorm:"serializer:aesgcm" json:"last_name"`
	IsSystem     bool       `gorm:"default:false" json:"is_system"` // System-User können sich nicht einloggen
	Active       bool       `gorm:"default:true" json:"active"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	// PasswordChangedAt markiert die letzte Passwortänderung. Tokens, die
	// davor ausgestellt wurden, gelten als ungültig - so werden nach einem
	// Passwort-Reset alle bestehenden Sessions serverseitig entwertet.
	PasswordChangedAt *time.Time `json:"-"`

	// MustChangePassword erzwingt beim nächsten Login das Ändern des
	// Passworts (gesetzt beim initialen Admin-User).
	MustChangePassword bool `gorm:"default:false" json:"must_change_password"`

	// Zwei-Faktor-Authentifizierung (TOTP). Das Secret liegt AES-GCM-
	// verschlüsselt in der DB; Enabled wird erst nach erfolgreicher
	// Code-Bestätigung gesetzt.
	TOTPSecretEnc string `json:"-"`
	TOTPEnabled   bool   `gorm:"default:false" json:"totp_enabled"`

	Roles []Role `gorm:"many2many:user_roles" json:"roles"`
}

// BeforeSave hält die Blindindizes synchron zu den (verschlüsselten) Feldern -
// auf ALLEN GORM-Schreibpfaden. Guard über Username (NOT NULL, bei jedem
// Voll-Save gesetzt): So bleiben die Indizes bei feldweisen Map-Updates ohne
// Username unangetastet, während ein Voll-Save auch das Leeren der E-Mail
// korrekt nachzieht.
func (u *User) BeforeSave(*gorm.DB) error {
	if u.Username != "" {
		u.UsernameBIdx = UserBlindIndex(u.Username)
		// Leere E-Mail MUSS Blindindex "" ergeben (nicht HMAC("")), sonst
		// kollidierten alle Benutzer ohne E-Mail am partiellen Unique-Index.
		if strings.TrimSpace(u.Email) == "" {
			u.EmailBIdx = ""
		} else {
			u.EmailBIdx = UserBlindIndex(u.Email)
		}
	}
	return nil
}

// FullName liefert "Vorname Nachname" (oder das nicht-leere Feld).
func (u *User) FullName() string {
	switch {
	case u.FirstName != "" && u.LastName != "":
		return u.FirstName + " " + u.LastName
	case u.FirstName != "":
		return u.FirstName
	default:
		return u.LastName
	}
}

// HasPermission prüft, ob der User über eine seiner Rollen die
// angegebene Permission besitzt. Erwartet vorgeladene Roles.Permissions.
func (u *User) HasPermission(code string) bool {
	for _, role := range u.Roles {
		for _, p := range role.Permissions {
			if p.Code == code {
				return true
			}
		}
	}
	return false
}

// PermissionCodes liefert alle Permission-Codes des Users (dedupliziert).
func (u *User) PermissionCodes() []string {
	seen := map[string]bool{}
	var codes []string
	for _, role := range u.Roles {
		for _, p := range role.Permissions {
			if !seen[p.Code] {
				seen[p.Code] = true
				codes = append(codes, p.Code)
			}
		}
	}
	return codes
}

// RoleNames liefert die Namen aller Rollen des Users.
func (u *User) RoleNames() []string {
	names := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		names = append(names, r.Name)
	}
	return names
}

// HasRole meldet, ob dem Benutzer die genannte Rolle zugewiesen ist.
func (u *User) HasRole(name string) bool {
	for _, r := range u.Roles {
		if r.Name == name {
			return true
		}
	}
	return false
}
