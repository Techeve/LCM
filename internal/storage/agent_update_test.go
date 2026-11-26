package storage

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

func openMigrated(t *testing.T) *repositories.AgentUpdateRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return repositories.NewAgentUpdateRepository(db)
}

// TestAgentUpdateRegelWirdGespeichertUndGelesen: Auch ein ausdrückliches
// „nicht gestaffelt“ bleibt erhalten (kein gorm-Default, der es überschriebe).
func TestAgentUpdateRegelWirdGespeichertUndGelesen(t *testing.T) {
	repo := openMigrated(t)
	def, err := repo.Get(5)
	if err != nil || def.Mode != domain.AgentUpdateOff || !def.Staged {
		t.Fatalf("vorgabe: %+v, %v", def, err)
	}
	in := &domain.AgentUpdatePolicy{GroupID: 5, Mode: domain.AgentUpdateAuto, Staged: false, CronExpr: "30 2 * * 1", WindowMinutes: 90}
	if err := repo.SaveSettings(in); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != domain.AgentUpdateAuto || got.Staged || got.CronExpr != "30 2 * * 1" || got.WindowMinutes != 90 {
		t.Errorf("gelesen: %+v", got)
	}
	enabled, _ := repo.Enabled()
	if len(enabled) != 1 || enabled[0].GroupID != 5 {
		t.Errorf("eingeschaltet: %+v", enabled)
	}
	// Speichern ändert die eine Zeile, es entsteht keine zweite.
	in.Mode = domain.AgentUpdateOff
	if err := repo.SaveSettings(in); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := repo.Enabled(); len(enabled) != 0 {
		t.Errorf("nach dem Ausschalten: %+v", enabled)
	}
}

// TestAgentUpdateStandUndEinstellungenStoerenSichNicht: Das Ergebnis eines
// Laufs überschreibt keine Einstellung, und neue Einstellungen heben die Sperre
// auf, lassen aber den geprüften Stand stehen.
func TestAgentUpdateStandUndEinstellungenStoerenSichNicht(t *testing.T) {
	repo := openMigrated(t)
	if err := repo.SaveSettings(&domain.AgentUpdatePolicy{GroupID: 1, Mode: domain.AgentUpdateAuto, Staged: true, CronExpr: "0 3 * * *", WindowMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	// Der Lauf hat die Regel VOR einer Änderung gelesen und schreibt danach seinen Stand.
	stale, _ := repo.Get(1)
	if err := repo.SaveSettings(&domain.AgentUpdatePolicy{GroupID: 1, Mode: domain.AgentUpdateAuto, Staged: false, CronExpr: "0 4 * * *", WindowMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stale.LastRunAt, stale.LastResult = &now, "1 aktualisiert"
	stale.ProvenFor, stale.ProvenVersion, stale.BlockedFor = "2.3.0", "2.3.0", "2.3.0"
	if err := repo.RecordRun(stale); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(1)
	if got.Staged || got.CronExpr != "0 4 * * *" || got.WindowMinutes != 30 {
		t.Errorf("der lauf überschrieb die einstellungen: %+v", got)
	}
	if got.LastResult != "1 aktualisiert" || got.ProvenVersion != "2.3.0" || got.BlockedFor != "2.3.0" {
		t.Errorf("stand nicht gespeichert: %+v", got)
	}
	// Bearbeiten der Regel hebt die Sperre auf, der geprüfte Stand bleibt.
	if err := repo.SaveSettings(&domain.AgentUpdatePolicy{GroupID: 1, Mode: domain.AgentUpdateAuto, CronExpr: "0 4 * * *"}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(1)
	if got.BlockedFor != "" || got.ProvenFor != "2.3.0" {
		t.Errorf("nach dem Bearbeiten: %+v", got)
	}
}

// TestAgentUpdateEinLaufOhneGespeicherteRegelLegtSieAn: Ein Lauf von Hand für
// eine Gruppe ohne Regel hinterlässt eine ausgeschaltete Zeile.
func TestAgentUpdateEinLaufOhneGespeicherteRegelLegtSieAn(t *testing.T) {
	repo := openMigrated(t)
	p, _ := repo.Get(9)
	now := time.Now()
	p.LastRunAt, p.LastResult = &now, "Nichts zu tun"
	if err := repo.RecordRun(p); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(9)
	if got.ID == 0 || got.Mode != domain.AgentUpdateOff || got.LastResult != "Nichts zu tun" {
		t.Errorf("gelesen: %+v", got)
	}
}

// TestAgentUpdateTabelleUndSpaltenEntstehenBeimMigrieren: Eine Datenbank aus der
// Zeit davor bekommt Tabelle und Spalten, bestehende Server behalten ihre Daten.
func TestAgentUpdateTabelleUndSpaltenEntstehenBeimMigrieren(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"DROP TABLE agent_update_policies",
		"ALTER TABLE servers DROP COLUMN agent_rollback",
		"ALTER TABLE servers DROP COLUMN agent_reported_at",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("zweite Migration: %v", err)
	}
	if !db.Migrator().HasTable(&domain.AgentUpdatePolicy{}) {
		t.Error("Tabelle der Agent-Update-Regeln fehlt")
	}
	for _, col := range []string{"agent_rollback", "agent_reported_at"} {
		if !db.Migrator().HasColumn("servers", col) {
			t.Errorf("servers.%s fehlt", col)
		}
	}
}

// TestGruppeAufloesenLoeschtDieAgentUpdateRegel.
func TestGruppeAufloesenLoeschtDieAgentUpdateRegel(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	groups := repositories.NewGroupRepository(db)
	g := &domain.ServerGroup{Name: "Produktion"}
	if err := groups.Create(g); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewAgentUpdateRepository(db)
	if err := repo.SaveSettings(&domain.AgentUpdatePolicy{GroupID: g.ID, Mode: domain.AgentUpdateAuto, CronExpr: "0 3 * * *"}); err != nil {
		t.Fatal(err)
	}
	if err := groups.Disband(g.ID); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := repo.Enabled(); len(enabled) != 0 {
		t.Errorf("die Regel überlebte die Gruppe: %+v", enabled)
	}
}
