package repositories

import "gorm.io/gorm"

// DatabaseRepository bündelt Vorgänge, die die Datenbank als Ganzes betreffen
// statt einer Tabelle.
type DatabaseRepository struct {
	db *gorm.DB
}

func NewDatabaseRepository(db *gorm.DB) *DatabaseRepository {
	return &DatabaseRepository{db: db}
}

// SnapshotInto schreibt eine konsistente Kopie der Datenbank nach path
// (SQLite VACUUM INTO). Die Datei darf schon existieren, muss dann aber leer
// sein - so kann der Aufrufer sie vorher mit den richtigen Rechten anlegen.
func (r *DatabaseRepository) SnapshotInto(path string) error {
	return r.db.Exec("VACUUM INTO ?", path).Error
}
