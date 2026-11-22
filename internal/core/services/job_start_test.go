package services

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestStartJobGibtKopieZurueck sichert, dass der Aufrufer eine eigene
// Momentaufnahme des Jobs bekommt: Der Runner schreibt Status und Ausgabe in
// das Original, der Aufrufer liest seine Kopie - unter -race ohne Wettlauf.
func TestStartJobGibtKopieZurueck(t *testing.T) {
	// DB direkt aufbauen: storage importiert services (Import-Zyklus).
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.Job{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	jobs := NewJobService(repositories.NewJobRepository(db))
	job, err := jobs.Start(nil, nil, "update", "Update", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	release := make(chan struct{})
	finished := make(chan struct{})
	snapshot := startJob(jobs, "test:job-start", job, func() {
		<-release
		jobs.Complete(job, "fertig", ptrInt(0), nil)
		close(finished)
	})

	if snapshot == job {
		t.Fatal("startJob gab den laufenden Zeiger zurück, erwartet eine Kopie")
	}
	if snapshot.ID != job.ID || snapshot.Status != domain.JobStatusRunning {
		t.Fatalf("Kopie weicht ab: id %q status %q", snapshot.ID, snapshot.Status)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("runner endete nicht")
	}
	// Das Original ist abgeschlossen, die Momentaufnahme bleibt beim Start.
	if job.Status != domain.JobStatusSuccess {
		t.Errorf("original: status %q, erwartet success", job.Status)
	}
	if snapshot.Status != domain.JobStatusRunning || snapshot.Output != "" {
		t.Errorf("kopie wurde vom runner verändert: status %q output %q", snapshot.Status, snapshot.Output)
	}
}
