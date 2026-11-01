package services

import (
	"errors"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// outcomeRecorder sammelt, was OnOutcome meldet.
type outcomeRecorder struct{ got []JobOutcome }

func (r *outcomeRecorder) record(o JobOutcome) { r.got = append(r.got, o) }

func newOutcomeJobs(t *testing.T) (*JobService, *outcomeRecorder, uint) {
	t.Helper()
	db := newCapabilityDB(t)
	server := &domain.Server{Name: "web01", Host: "10.0.0.1", SSHPort: 22, ServiceUser: "lcm"}
	if err := repositories.NewServerRepository(db).Create(server); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	jobs := NewJobService(repositories.NewJobRepository(db))
	rec := &outcomeRecorder{}
	jobs.OnOutcome(rec.record)
	return jobs, rec, server.ID
}

func TestCompleteMeldetErgebnisNachDemSpeichern(t *testing.T) {
	jobs, rec, serverID := newOutcomeJobs(t)
	job, err := jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Update @ web01", "admin")
	if err != nil {
		t.Fatal(err)
	}

	jobs.Complete(job, "out", ptrInt(100), errors.New("kaputt"))

	if len(rec.got) != 1 || rec.got[0].ByWatchdog || rec.got[0].Job.ID != job.ID {
		t.Fatalf("Meldungen = %+v", rec.got)
	}
	// Der Abonnent sieht den endgültigen Job: Status, Exit-Code, Fehlerzeile.
	got := rec.got[0].Job
	if got.Status != domain.JobStatusFailed || got.ExitCode == nil || *got.ExitCode != 100 ||
		got.Output != "out\nFEHLER: kaputt" {
		t.Errorf("Job beim Abonnenten: %+v", got)
	}
}

func TestWatchdogAbbruchWirdGemeldetHandAbbruchNicht(t *testing.T) {
	jobs, rec, serverID := newOutcomeJobs(t)

	manual, _ := jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Update @ web01", "admin")
	if _, err := jobs.Abort(repositories.AccessScope{All: true}, manual.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 0 {
		t.Fatalf("manueller Abbruch gemeldet: %+v", rec.got)
	}
	// Das nachlaufende Complete der abgebrochenen Goroutine meldet ebenfalls nichts.
	jobs.Complete(manual, "", nil, errors.New("verbindung geschlossen"))
	if len(rec.got) != 0 {
		t.Fatalf("Complete nach Abbruch gemeldet: %+v", rec.got)
	}

	stalled, _ := jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Update 2 @ web01", "admin")
	jobs.startWatching(stalled.ID)
	time.Sleep(5 * time.Millisecond)
	jobs.abortIfStalled(stalled, func(*domain.Job) time.Duration { return time.Millisecond })

	if len(rec.got) != 1 || !rec.got[0].ByWatchdog || rec.got[0].Job.Status != domain.JobStatusAborted {
		t.Fatalf("Watchdog-Abbruch nicht gemeldet: %+v", rec.got)
	}
}

func TestBlockierteJobsUndNeustartSindKeinErgebnis(t *testing.T) {
	jobs, rec, serverID := newOutcomeJobs(t)

	running, _ := jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Update @ web01", "admin")
	if _, err := jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Update 2 @ web01", "admin"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("zweiter Job sollte blockiert sein: %v", err)
	}
	if len(rec.got) != 0 {
		t.Fatalf("blockierter Job gemeldet: %+v", rec.got)
	}

	// Dienst-Neustart: der laufende Job wird failed, ist aber nicht Schuld des Servers.
	_ = running
	jobs.FailInterruptedOnStartup(nil)
	if len(rec.got) != 0 {
		t.Fatalf("Neustart-Aufräumen gemeldet: %+v", rec.got)
	}
}
