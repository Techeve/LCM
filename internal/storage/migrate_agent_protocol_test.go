package storage

import "testing"

// TestMigrateErgaenztDieProtokollversionDesAgents: Eine Datenbank aus der Zeit
// vor Paket E hat die Spalte noch nicht. AutoMigrate legt sie an, bestehende
// Zeilen erhalten 0 („noch nie gemeldet“) - der Agent meldet sie beim nächsten
// Connect, bis dahin gilt er als Protokoll 1.
func TestMigrateErgaenztDieProtokollversionDesAgents(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE servers DROP COLUMN agent_protocol").Error; err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn("servers", "agent_protocol") {
		t.Fatal("die Spalte ist noch da")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("zweite Migration: %v", err)
	}
	if !db.Migrator().HasColumn("servers", "agent_protocol") {
		t.Fatal("agent_protocol fehlt nach der Migration")
	}
}
