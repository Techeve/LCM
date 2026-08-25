package services_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestFailInterruptedOnStartupReleasesLock deckt den gemeldeten Kernfehler ab:
// Ein beim Dienst-Neustart unterbrochener Job blieb "running" und blockierte
// den Server für immer. Die Startup-Recovery muss solche Einträge als failed
// abschließen und die Sperre freigeben.
func TestFailInterruptedOnStartupReleasesLock(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")

	// Job läuft - ein zweiter wird blockiert (Sperre aktiv).
	job, err := env.Jobs.Start(&serverID, nil, "update", "Update @ web01", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}
	if _, err := env.Jobs.Start(&serverID, nil, "update", "Update 2 @ web01", "admin"); !errors.Is(err, services.ErrServerBusy) {
		t.Fatalf("zweiter job sollte blockiert sein, bekam: %v", err)
	}

	// Neustart simuliert: Recovery räumt den verwaisten running-Job auf.
	// Ohne Versionswechsel (nil) gilt der Regelfall - der Job ist ein Fehler.
	env.Jobs.FailInterruptedOnStartup(nil)

	var reloaded domain.Job
	if err := env.DB().First(&reloaded, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	if reloaded.Status != domain.JobStatusFailed {
		t.Errorf("unterbrochener job sollte failed sein, ist %q", reloaded.Status)
	}
	if reloaded.FinishedAt == nil {
		t.Error("unterbrochener job hat kein finished_at")
	}

	// Sperre ist frei: ein neuer Job startet wieder.
	if _, err := env.Jobs.Start(&serverID, nil, "update", "Update 3 @ web01", "admin"); err != nil {
		t.Fatalf("nach recovery sollte ein neuer job starten: %v", err)
	}
}

// TestAbortReleasesLockAndIgnoresLateComplete: Der manuelle Abbruch gibt die
// Server-Sperre frei; ein späteres Complete der (noch laufenden) Goroutine
// darf den abgebrochenen Job nicht mehr überschreiben.
func TestAbortReleasesLockAndIgnoresLateComplete(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web02")

	job, err := env.Jobs.Start(&serverID, nil, "update", "Update @ web02", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	aborted, err := env.Jobs.Abort(repositories.ScopeAll(), job.ID, "admin")
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if aborted.Status != domain.JobStatusAborted {
		t.Errorf("abgebrochener job sollte 'aborted' sein (R2-068), ist %q", aborted.Status)
	}
	if !strings.Contains(aborted.Output, "ABGEBROCHEN") {
		t.Errorf("abbruch-vermerk fehlt im output: %q", aborted.Output)
	}

	// Sperre ist frei.
	if _, err := env.Jobs.Start(&serverID, nil, "update", "Update 2 @ web02", "admin"); err != nil {
		t.Fatalf("nach abort sollte ein neuer job starten: %v", err)
	}

	// Die Goroutine des abgebrochenen Jobs meldet sich verspätet: darf den
	// finalen Zustand nicht mehr ändern.
	exit := 0
	env.Jobs.Complete(job, "später output", &exit, nil)
	var reloaded domain.Job
	if err := env.DB().First(&reloaded, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	if reloaded.Status != domain.JobStatusAborted {
		t.Errorf("late complete hat den abgebrochenen job überschrieben: %q", reloaded.Status)
	}

	// Doppelter Abbruch wird sauber abgewiesen.
	if _, err := env.Jobs.Abort(repositories.ScopeAll(), job.ID, "admin"); !errors.Is(err, services.ErrJobNotRunning) {
		t.Errorf("zweiter abort sollte ErrJobNotRunning liefern, bekam: %v", err)
	}
}

// TestFailedJobHatExitCode (R2-065): Ein fehlgeschlagener Job darf im
// strukturierten exit_code-Feld nie null oder 0 tragen - sonst liest ein
// Aufrufer, der auf exit_code==0 prüft, einen Erfolg.
func TestFailedJobHatExitCode(t *testing.T) {
	env := newTestEnv(t)
	sid := joinTestServer(t, env, "exit01")

	// a) Fehlschlag ohne mitgegebenen Code → Sentinel, nicht null.
	j1, _ := env.Jobs.Start(&sid, nil, "script", "s1", "admin")
	env.Jobs.Complete(j1, "boom", nil, errTest)
	r1 := reloadJob(t, env, j1.ID)
	if r1.Status != domain.JobStatusFailed || r1.ExitCode == nil || *r1.ExitCode == 0 {
		t.Errorf("failed ohne Code: exit_code muss !=0 und !=null sein, ist %v", r1.ExitCode)
	}

	// b) Fehlschlag mit exit_code 0 (widersprüchlich) → korrigiert auf !=0.
	j2, _ := env.Jobs.Start(&sid, nil, "backup", "s2", "admin")
	zero := 0
	env.Jobs.Complete(j2, "backup kaputt", &zero, errTest)
	r2 := reloadJob(t, env, j2.ID)
	if r2.ExitCode == nil || *r2.ExitCode == 0 {
		t.Errorf("failed mit Code 0: muss korrigiert werden, ist %v", r2.ExitCode)
	}

	// c) Echter Code bleibt erhalten.
	j3, _ := env.Jobs.Start(&sid, nil, "custom", "s3", "admin")
	three := 3
	env.Jobs.Complete(j3, "exit 3", &three, errTest)
	r3 := reloadJob(t, env, j3.ID)
	if r3.ExitCode == nil || *r3.ExitCode != 3 {
		t.Errorf("echter exit-code 3 muss erhalten bleiben, ist %v", r3.ExitCode)
	}

	// d) Erfolg bleibt 0.
	j4, _ := env.Jobs.Start(&sid, nil, "health", "s4", "admin")
	env.Jobs.Complete(j4, "ok", &zero, nil)
	r4 := reloadJob(t, env, j4.ID)
	if r4.Status != domain.JobStatusSuccess || r4.ExitCode == nil || *r4.ExitCode != 0 {
		t.Errorf("erfolg muss exit_code 0 tragen, ist %v (%s)", r4.ExitCode, r4.Status)
	}
}

var errTest = fmt.Errorf("kommando fehlgeschlagen")

func reloadJob(t *testing.T, env *testEnv, id string) domain.Job {
	t.Helper()
	var j domain.Job
	if err := env.DB().First(&j, "id = ?", id).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	return j
}
