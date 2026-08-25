package domain

import (
	"time"

	"gorm.io/gorm"
)

// PendingUserSync ist ein offener Benutzer-Abgleich für einen Server -
// der Rückstand, wenn ein Server im Moment der Änderung nicht erreichbar war.
//
// Warum das nötig ist: Ändert sich etwas an einem Linux-Benutzer (neuer
// Schlüssel, sudo, Gruppen-Zuordnung), versucht LCM den Abgleich sofort. Ein
// Server, der gerade aus ist, bekäme die Änderung sonst erst beim nächsten
// geplanten Sync - und ein ENTZOGENER Zugang bliebe bis dahin nutzbar. Der
// Eintrag hier überlebt auch einen Neustart von LCM und wird beim nächsten
// erfolgreichen Kontakt (Health-Check) abgearbeitet.
type PendingUserSync struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID uint `gorm:"not null;index" json:"server_id"`

	// Username leer = vollständiger Abgleich (alle zugeordneten Konten
	// verteilen). Gesetzt = dieses Konto ist zu ENTFERNEN; der Name steht
	// hier, weil die Zuordnung im Moment des Eintrags bereits gelöst ist und
	// sich nicht mehr nachschlagen ließe.
	//
	// Wie im LinuxUser liegt der Name verschlüsselt; der Blindindex macht ihn
	// trotzdem auffindbar (Doppel-Einträge vermeiden).
	Username     string `gorm:"serializer:aesgcm" json:"username"`
	UsernameBIdx string `gorm:"column:username_bidx;index" json:"-"`

	// Attempts/LastError machen einen dauerhaft scheiternden Eintrag sichtbar,
	// statt ihn stumm im Kreis laufen zu lassen.
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error"`
}

// Removal meldet, ob der Eintrag ein Konto entfernt (statt zu verteilen).
func (p *PendingUserSync) Removal() bool { return p.Username != "" }

// BeforeSave hält den Blindindex zum verschlüsselten Namen synchron.
func (p *PendingUserSync) BeforeSave(*gorm.DB) error {
	if p.Username != "" {
		p.UsernameBIdx = UserBlindIndex(p.Username)
	}
	return nil
}
