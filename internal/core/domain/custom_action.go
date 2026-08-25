package domain

import "time"

// CustomAction ist eine benutzerdefinierte, wiederverwendbare Aktion: eine
// Liste von Shell-Kommandos, die auf einem Zielserver der Reihe nach
// ausgeführt werden. Custom-Aktionen werden unter den Einstellungen gepflegt
// und lassen sich in Servergruppen als Rule-Typ "custom" auswählen.
type CustomAction struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Name        string `gorm:"uniqueIndex;not null" json:"name"`
	Description string `json:"description"`
	// Commands: ein Kommando pro Zeile, in Ausführungsreihenfolge. Leere
	// Zeilen und mit '#' beginnende Kommentarzeilen werden übersprungen.
	Commands string `gorm:"not null" json:"commands"`
}
