package migrations

import "gorm.io/gorm"

// v0.5.0 führt den Speicher-Verlauf ein: die neue Tabelle storage_history und
// die Settings-Spalte storage_history_retention_days legt AutoMigrate an (mit
// gorm-Default 90, der aber nur für NEUE Zeilen greift).
//
// Diese Migration zieht die bestehende (einzige) Settings-Zeile nach: Sie setzt
// die Aufbewahrung auf den Standard von 90 Tagen, falls noch kein gültiger Wert
// (mindestens 90) hinterlegt ist. Idempotent - bereits gesetzte Werte im
// erlaubten Bereich bleiben unangetastet.
func init() {
	Register(Migration{
		Version: "0.5.0",
		Name:    "0.5.0-storage-history-retention",
		Run: func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE global_settings
				SET storage_history_retention_days = 90
				WHERE storage_history_retention_days IS NULL
				   OR storage_history_retention_days < 90`).Error
		},
	})
}
