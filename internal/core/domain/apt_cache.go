package domain

import "time"

// AptCache ist ein zentraler APT-Cache (apt-cacher-ng), über den verwaltete
// Server ihre Pakete laden. Es kann mehrere geben - etwa einen je Standort;
// die Regel „APT-Cache anbinden" und die Server-Aktion wählen einen davon.
// Verwaltet unter Einstellungen → APT-Cache.
type AptCache struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Name identifiziert den Cache in der Auswahl (eindeutig).
	Name string `gorm:"uniqueIndex;not null" json:"name"`
	// URL ist die Basis-Adresse, z.B. http://192.168.1.10:3142. Sie landet
	// wörtlich im apt-Drop-in der Server.
	URL string `gorm:"not null" json:"url"`
}
