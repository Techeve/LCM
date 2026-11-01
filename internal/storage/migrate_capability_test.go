package storage

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// TestBackfillPackagesCapability: Server mit erfolgreichem Paket-Job bekommen
// eine Zeile packages (ok) mit dem jüngsten Erfolg; alle anderen keine.
func TestBackfillPackagesCapability(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	newServer := func(name string) *domain.Server {
		s := &domain.Server{Name: name, Host: name, ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e"}
		if err := db.Create(s).Error; err != nil {
			t.Fatal(err)
		}
		return s
	}
	base := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	addJob := func(s *domain.Server, jobType, status string, finished time.Time) {
		t.Helper()
		j := &domain.Job{ServerID: &s.ID, Type: jobType, Name: jobType, Status: status, FinishedAt: &finished}
		if err := db.Create(j).Error; err != nil {
			t.Fatal(err)
		}
	}

	mitErfolg := newServer("a")
	addJob(mitErfolg, domain.RuleTypeUpdate, domain.JobStatusSuccess, base)
	addJob(mitErfolg, domain.RuleTypePackageScan, domain.JobStatusSuccess, base.Add(48*time.Hour))
	addJob(mitErfolg, domain.RuleTypeSecurity, domain.JobStatusFailed, base.Add(72*time.Hour))
	addJob(mitErfolg, domain.RuleTypeHealth, domain.JobStatusSuccess, base.Add(96*time.Hour))

	nurFehlschlag := newServer("b")
	addJob(nurFehlschlag, domain.RuleTypeUpdate, domain.JobStatusFailed, base)

	nurHealth := newServer("c")
	addJob(nurHealth, domain.RuleTypeHealth, domain.JobStatusSuccess, base)

	bereitsGefuehrt := newServer("d")
	addJob(bereitsGefuehrt, domain.RuleTypeUpdate, domain.JobStatusSuccess, base)
	vorhanden := base.Add(200 * time.Hour)
	if err := db.Create(&domain.ServerCapability{
		ServerID: bereitsGefuehrt.ID, Category: domain.CapabilityPackages,
		State: domain.CapabilityFailed, ConsecutiveFailures: 3, LastSuccessAt: &vorhanden,
	}).Error; err != nil {
		t.Fatal(err)
	}

	runMigrationByName(t, db, "1.49.0-capability-packages-backfill")

	find := func(s *domain.Server) *domain.ServerCapability {
		var rows []domain.ServerCapability
		if err := db.Where("server_id = ?", s.ID).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			return nil
		}
		if len(rows) > 1 {
			t.Fatalf("server %s: %d Zeilen", s.Name, len(rows))
		}
		return &rows[0]
	}

	got := find(mitErfolg)
	if got == nil || got.Category != domain.CapabilityPackages || got.State != domain.CapabilityOK {
		t.Fatalf("server a: %+v", got)
	}
	want := base.Add(48 * time.Hour)
	if got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(want) {
		t.Errorf("server a: LastSuccessAt = %v, erwartet %v", got.LastSuccessAt, want)
	}
	if row := find(nurFehlschlag); row != nil {
		t.Errorf("server b ohne Erfolg bekam eine Zeile: %+v", row)
	}
	if row := find(nurHealth); row != nil {
		t.Errorf("server c mit nur einem Health-Ping bekam eine Zeile: %+v", row)
	}
	if row := find(bereitsGefuehrt); row == nil || row.State != domain.CapabilityFailed || row.ConsecutiveFailures != 3 {
		t.Errorf("server d: vorhandene Zeile wurde verändert: %+v", row)
	}

	// Idempotent: ein zweiter Lauf ändert nichts mehr.
	runMigrationByName(t, db, "1.49.0-capability-packages-backfill")
	var count int64
	db.Model(&domain.ServerCapability{}).Count(&count)
	if count != 2 {
		t.Errorf("nach dem zweiten Lauf %d Zeilen, erwartet 2", count)
	}
}
