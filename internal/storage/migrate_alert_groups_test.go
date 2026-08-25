package storage

import (
	"testing"

	"LCM/internal/core/domain"
)

// TestMigrateAlertRuleGroups simuliert eine Alt-Datenbank, in der eine
// Alarm-Regel genau eine Gruppe trug (Spalte group_id): nach der Migration
// steht dieselbe Gruppe in der Mehrfachauswahl, und die Alt-Spalte ist leer.
func TestMigrateAlertRuleGroups(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	// Alt-Zustand nachbauen: die Spalte gibt es in neuen Datenbanken nicht mehr.
	if err := db.Exec(`ALTER TABLE alert_rules ADD COLUMN group_id integer`).Error; err != nil {
		t.Fatal(err)
	}
	group := domain.ServerGroup{Name: "Produktion"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	rule := domain.AlertRule{Name: "Platte voll", Type: domain.AlertTypeDiskCapacity, Enabled: true}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE alert_rules SET group_id = ? WHERE id = ?`, group.ID, rule.ID).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateAlertRuleGroups(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var migrated domain.AlertRule
	if err := db.Preload("Groups").First(&migrated, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(migrated.Groups) != 1 || migrated.Groups[0].ID != group.ID {
		t.Fatalf("gruppe nicht übernommen: %+v", migrated.Groups)
	}
	var leftover int64
	if err := db.Raw(`SELECT COUNT(*) FROM alert_rules WHERE group_id IS NOT NULL`).Scan(&leftover).Error; err != nil {
		t.Fatal(err)
	}
	if leftover != 0 {
		t.Errorf("alte spalte nicht geleert: %d zeilen", leftover)
	}

	// Zweiter Lauf darf nichts verdoppeln (idempotent).
	if err := migrateAlertRuleGroups(db); err != nil {
		t.Fatalf("zweiter lauf: %v", err)
	}
	var links int64
	if err := db.Raw(`SELECT COUNT(*) FROM alert_rule_groups`).Scan(&links).Error; err != nil {
		t.Fatal(err)
	}
	if links != 1 {
		t.Errorf("erwartet genau eine zuordnung, sind %d", links)
	}
}
