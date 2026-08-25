package storage

import (
	"testing"

	"LCM/internal/core/domain"
)

// TestMigrateRulesToSchedules simuliert eine Alt-Datenbank, in der Rules
// noch ihren eigenen Cron-Ausdruck trugen: nach der Migration hängen sie
// an einem Schedule; alte Firewall-Rules werden zu Grundsatz-Regeln.
func TestMigrateRulesToSchedules(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}

	// Alt-Zustand nachbauen: cron_expr-Spalte + zwei Legacy-Rules.
	if err := db.Exec(`ALTER TABLE rules ADD COLUMN cron_expr text NOT NULL DEFAULT ''`).Error; err != nil {
		t.Fatal(err)
	}
	group := domain.ServerGroup{Name: "Produktion"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO rules (group_id, name, type, command, cron_expr, enabled, is_system, enforce)
		VALUES (?, 'Tägliche Updates', 'update', '', '0 2 * * *', 1, 0, 0),
		       (?, 'web-fw', 'firewall', '80,443', '0 3 * * *', 1, 0, 0)`, group.ID, group.ID).Error; err != nil {
		t.Fatal(err)
	}

	if err := migrateRulesToSchedules(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Update-Rule hängt jetzt an einem Schedule mit dem alten Cron.
	var updateRule domain.Rule
	if err := db.Where("type = 'update'").First(&updateRule).Error; err != nil {
		t.Fatal(err)
	}
	if updateRule.ScheduleID == nil || updateRule.Enforce {
		t.Fatalf("update-rule nicht an schedule migriert: %+v", updateRule)
	}
	var sched domain.Schedule
	if err := db.First(&sched, *updateRule.ScheduleID).Error; err != nil {
		t.Fatal(err)
	}
	if sched.CronExpr != "0 2 * * *" || sched.GroupID != group.ID || !sched.Enabled {
		t.Errorf("schedule falsch migriert: %+v", sched)
	}

	// Firewall-Rule ist jetzt eine Grundsatz-Regel ohne Schedule.
	var fwRule domain.Rule
	if err := db.Where("type = 'firewall'").First(&fwRule).Error; err != nil {
		t.Fatal(err)
	}
	if !fwRule.Enforce || fwRule.ScheduleID != nil || fwRule.Command != "80,443" {
		t.Errorf("firewall-rule nicht zu grundsatz-regel migriert: %+v", fwRule)
	}

	// Idempotent: ein zweiter Lauf legt keine weiteren Schedules an.
	if err := migrateRulesToSchedules(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&domain.Schedule{}).Count(&n)
	if n != 1 {
		t.Errorf("migration nicht idempotent: %d schedules", n)
	}
}
