package services

import (
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// TestJobPanicCleanupReleasesLock deckt den zweiten Weg ab, auf dem eine
// Server-Sperre hängen bleiben könnte: ein Programmierfehler (Panic) im
// Job-Runner.
//
// Ohne Schutz hätte ein solcher Panic früher den GESAMTEN Dienst beendet.
// Jetzt fängt safego ihn ab - dann MUSS der Job aber sauber als fehlgeschlagen
// abgeschlossen werden, sonst bliebe der betroffene Server bis zum nächsten
// Neustart für alle Aktionen blockiert.
func TestJobPanicCleanupReleasesLock(t *testing.T) {
	safego.Reset()
	// DB direkt aufbauen: storage importiert services, ein storage-Import in
	// einem internen services-Test wäre ein Import-Zyklus.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	// Eine einzige Verbindung: Bei :memory: bekäme sonst jede Verbindung ihre
	// EIGENE leere Datenbank (dasselbe macht storage.Open).
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.Job{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	serverRepo := repositories.NewServerRepository(db)
	server := &domain.Server{Name: "web01", Host: "10.0.0.1", SSHPort: 22, ServiceUser: "lcm"}
	if err := serverRepo.Create(server); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	jobs := NewJobService(repositories.NewJobRepository(db))

	job, err := jobs.Start(&server.ID, nil, "update", "Update @ web01", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}
	// Die Sperre greift: ein zweiter Job wird abgewiesen.
	if _, err := jobs.Start(&server.ID, nil, "update", "Update 2 @ web01", "admin"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("zweiter job sollte blockiert sein, bekam: %v", err)
	}

	// Ein panickender Runner - exakt so gestartet wie von den Server-Aktionen.
	done := make(chan struct{})
	safego.GoCleanup("test:job-panic", jobPanicCleanup(jobs, job), func() {
		close(done)
		panic("simulierter programmierfehler im job-runner")
	})
	<-done
	// Das Aufräumen läuft im defer, also kurz darauf warten.
	deadline := time.Now().Add(2 * time.Second)
	var reloaded domain.Job
	for time.Now().Before(deadline) {
		if err := db.First(&reloaded, "id = ?", job.ID).Error; err == nil &&
			reloaded.Status == domain.JobStatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if reloaded.Status != domain.JobStatusFailed {
		t.Fatalf("job nach panic sollte failed sein, ist %q", reloaded.Status)
	}
	// Der entscheidende Punkt: die Sperre ist frei, der Server wieder bedienbar.
	if _, err := jobs.Start(&server.ID, nil, "update", "Update 3 @ web01", "admin"); err != nil {
		t.Errorf("server nach abgefangenem panic weiterhin gesperrt: %v", err)
	}
	// Und der Panic wurde für die Instabilitäts-Erkennung verbucht.
	if safego.Total() != 1 {
		t.Errorf("Total() = %d, erwartet 1", safego.Total())
	}
}
