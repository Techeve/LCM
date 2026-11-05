package storage

import "gorm.io/gorm"

// ensureJobTimeIndex legt den Ausdrucks-Index an, über den die Job-Liste nach
// Zeit sortiert und filtert (siehe repositories.jobTimeExpr). GORM-Tags kennen
// keine Ausdrucks-Indizes; der Ausdruck muss wörtlich dem der Abfrage
// entsprechen, sonst greift SQLite ihn nicht auf. Idempotent.
func ensureJobTimeIndex(db *gorm.DB) error {
	return db.Exec("CREATE INDEX IF NOT EXISTS idx_jobs_time ON jobs(COALESCE(started_at, created_at))").Error
}
