package storage

import (
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// TestMigrateGroupPriorities simuliert eine Alt-Datenbank, in der es den
// Vorrang noch nicht gab (Spalte leer/0): Nach der Migration trägt jede
// normale Gruppe den Standardwert und die System-Gruppe den schwächsten
// Vorrang - sonst würde ausgerechnet die Grundlinie, die für ALLE Server gilt,
// die spezifischeren Gruppen überstimmen.
func TestMigrateGroupPriorities(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	// Alt-Zustand nachbauen: ohne die Spalte gab es keinen Wert.
	groups := []domain.ServerGroup{
		{Name: "System", IsSystem: true},
		{Name: "Web-Prod"},
	}
	for i := range groups {
		if err := db.Create(&groups[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`UPDATE server_groups SET priority = 0`).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateGroupPriorities(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	want := map[string]int{"System": domain.SystemGroupPriority, "Web-Prod": domain.DefaultGroupPriority}
	assertPriorities(t, db, want)

	// Idempotent: Ein zweiter Lauf darf nichts mehr verschieben - insbesondere
	// darf die System-Gruppe nicht erneut „nachgezogen" werden.
	if err := migrateGroupPriorities(db); err != nil {
		t.Fatalf("zweiter lauf: %v", err)
	}
	assertPriorities(t, db, want)

	// Ein ausdrücklich gesetzter Vorrang überlebt die Migration.
	if err := db.Exec(`UPDATE server_groups SET priority = 10 WHERE name = 'Web-Prod'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateGroupPriorities(db); err != nil {
		t.Fatalf("dritter lauf: %v", err)
	}
	want["Web-Prod"] = 10
	assertPriorities(t, db, want)
}

// assertPriorities prüft den Vorrang je Gruppenname.
func assertPriorities(t *testing.T, db *gorm.DB, want map[string]int) {
	t.Helper()
	var groups []domain.ServerGroup
	if err := db.Find(&groups).Error; err != nil {
		t.Fatal(err)
	}
	if len(groups) != len(want) {
		t.Fatalf("erwartet %d gruppen, bekam %d", len(want), len(groups))
	}
	for _, g := range groups {
		if got := g.Priority; got != want[g.Name] {
			t.Errorf("gruppe %q: vorrang %d, erwartet %d", g.Name, got, want[g.Name])
		}
	}
}
