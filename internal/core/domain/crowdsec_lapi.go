package domain

import "time"

// CrowdSecLapi ist eine zentrale CrowdSec-LAPI, an die verwaltete Server
// ihre Agents im Remote-Modus anbinden. Es kann mehrere geben - etwa eine je
// Standort oder Kunde („LAPI Techeve", „LAPI Service 2000"); bei der
// Einrichtung von CrowdSec auf einem Server wird eine davon gewählt.
// Verwaltet unter Einstellungen → CrowdSec.
type CrowdSecLapi struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Name identifiziert die LAPI in der Auswahl (eindeutig).
	Name string `gorm:"uniqueIndex;not null" json:"name"`
	// URL ist die Basis-Adresse, z.B. http://192.168.1.10:8080. Server, die
	// laut ihrer Credentials-Datei an diese URL melden, gelten als angebunden.
	URL   string `gorm:"not null" json:"url"`
	Login string `gorm:"not null" json:"login"` // Maschinen-Login
	// PasswordEnc: Maschinen-Passwort, AES-GCM - write-only, nie ausgegeben.
	PasswordEnc string `json:"-"`
}

// TableName pinnt den Tabellennamen (gorm trennte sonst crowd_sec_lapis).
func (CrowdSecLapi) TableName() string { return "crowdsec_lapis" }
