// Package migrations ist der Migrationsordner des Templates: EIN Skript
// (= eine Datei) pro Version, die Datenanpassungen braucht.
//
// Konvention:
//
//	migrations/
//	├── migrations.go            <- dieser Rahmen (Typ, Registry, Register)
//	├── v1_1_0_user_names.go     <- Migration, die v1.1.0 einführt
//	├── v1_2_0_xyz.go            <- nächstes Release usw.
//
// Jede Datei registriert ihre Migration in einer init()-Funktion über
// Register(). Der Update-Prozess (storage.RunUpdateMigrations) führt beim
// Anwendungsstart alle noch nicht gelaufenen Skripte in SemVer-Reihenfolge
// aus: erst 1.1.0, dann 1.2.0, dann 1.3.0 … bis zur installierten
// Binary-Version. Ist eine Installation bereits auf 1.3.0, sind die
// Skripte für 1.1.0/1.2.0 dort längst protokolliert und laufen nicht mehr.
//
// Anleitung mit Beispiel: docs/reference/database.md.
package migrations

import "gorm.io/gorm"

// Migration ist ein versionsgebundener Update-Schritt.
type Migration struct {
	// Version (SemVer) der Anwendung, die diese Migration einführt -
	// in der Regel der neue Inhalt der Datei VERSION beim Release.
	Version string
	// Name ist ein kurzer, eindeutiger, stabiler Bezeichner
	// (Dedup-Schlüssel im Protokoll - niemals ändern).
	Name string
	// Run enthält die Migrationslogik. Läuft in einer Transaktion und
	// muss idempotent sein.
	Run func(tx *gorm.DB) error
}

// Registry enthält alle bekannten Migrationen. Die Ausführungsreihenfolge
// bestimmt die SemVer-Sortierung, nicht die Registrierungsreihenfolge.
var Registry []Migration

// Register nimmt eine Migration in die Registry auf. Wird aus den
// init()-Funktionen der Versions-Dateien dieses Ordners aufgerufen.
func Register(m Migration) {
	for _, existing := range Registry {
		if existing.Name == m.Name {
			panic("migrations: doppelter Migrations-Name: " + m.Name)
		}
	}
	Registry = append(Registry, m)
}
