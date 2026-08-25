package storage

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// migrateGroupPriorities füllt den neu eingeführten Vorrang der Servergruppen
// im Altbestand: normale Gruppen auf den Standardwert, die System-Gruppe auf
// den schwächsten Vorrang.
//
// AutoMigrate legt die Spalte zwar mit DEFAULT an (SQLite füllt bestehende
// Zeilen damit), aber die System-Gruppe braucht ihren abweichenden Wert - und
// auf einen stillen Nullwert wollen wir uns bei einer Größe, die über die
// Durchsetzung von Firewall-Regeln entscheidet, nicht verlassen.
//
// Idempotent: Nach dem ersten Lauf trifft keine der beiden Bedingungen mehr
// zu. Ein bewusst auf 100 gesetzter Vorrang der System-Gruppe ist über die
// Oberfläche nicht herstellbar (sie ist gegen Änderungen geschützt).
func migrateGroupPriorities(db *gorm.DB) error {
	if err := db.Exec(
		`UPDATE server_groups SET priority = ? WHERE priority IS NULL OR priority <= 0`,
		domain.DefaultGroupPriority).Error; err != nil {
		return err
	}
	return db.Exec(
		`UPDATE server_groups SET priority = ? WHERE is_system = 1 AND priority = ?`,
		domain.SystemGroupPriority, domain.DefaultGroupPriority).Error
}
