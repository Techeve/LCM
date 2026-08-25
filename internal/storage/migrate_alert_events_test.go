package storage

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestMigrateAlertEventsToUUIDPreservesData baut die Alarm-Historie im
// Alt-Schema (INTEGER-id) auf, führt die Migration aus und prüft: alle
// Events sind erhalten, tragen UUIDs und die Cooldown-relevanten Felder
// (rule_id/server_id/created_at) blieben unverändert.
func TestMigrateAlertEventsToUUIDPreservesData(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := autoMigrate(db); err != nil {
		t.Fatal(err)
	}
	// Tabelle ins Alt-Schema zurückversetzen.
	if err := db.Migrator().DropTable("alert_events"); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyAlertEvent{}); err != nil {
		t.Fatal(err)
	}
	if got := idColumnType(db, "alert_events"); got != "integer" {
		t.Fatalf("vorbedingung: alert_events.id sollte INTEGER sein, ist %q", got)
	}

	srvID := uint(7)
	created := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	must(t, db.Create(&legacyAlertEvent{ID: 1, CreatedAt: created, RuleID: 3, ServerID: &srvID,
		RuleName: "Festplatte fast voll", ServerName: "web01", Type: domain.AlertTypeDiskCapacity,
		Severity: "warning", Code: "disk>90", Description: "92% belegt", Notified: true}).Error)
	must(t, db.Create(&legacyAlertEvent{ID: 2, CreatedAt: created.Add(time.Hour), RuleID: 4,
		RuleName: "Kritische CVEs", Type: domain.AlertTypeSecurityCVE,
		Severity: "critical", NotifyError: "smtp down"}).Error)

	if err := migrateAlertEventsToUUID(db); err != nil {
		t.Fatalf("migration fehlgeschlagen: %v", err)
	}
	// Idempotenz: zweiter Lauf ist ein No-op.
	if err := migrateAlertEventsToUUID(db); err != nil {
		t.Fatalf("zweiter lauf: %v", err)
	}

	if got := idColumnType(db, "alert_events"); got != "text" {
		t.Fatalf("alert_events.id sollte TEXT sein, ist %q", got)
	}
	var events []domain.AlertEvent
	if err := db.Order("created_at").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("erwartet 2 events, gefunden %d", len(events))
	}
	first := events[0]
	if len(first.ID) != 36 {
		t.Errorf("id ist keine UUID: %q", first.ID)
	}
	if first.RuleID != 3 || first.ServerID == nil || *first.ServerID != srvID ||
		!first.CreatedAt.Equal(created) || !first.Notified || first.ServerName != "web01" {
		t.Errorf("event-felder nicht erhalten: %+v", first)
	}
	if events[1].NotifyError != "smtp down" || events[1].ServerID != nil {
		t.Errorf("zweites event nicht erhalten: %+v", events[1])
	}
	if events[0].ID == events[1].ID {
		t.Error("events haben identische UUIDs")
	}
}
