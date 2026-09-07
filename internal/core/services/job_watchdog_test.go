package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
)

// nopCloser steht für die SSH-Verbindung eines laufenden Jobs: Erst mit ihr
// gilt ein Job als überwacht - vorher gibt es kein Kommando, das hängen könnte.
type nopCloser struct{ closed bool }

func (c *nopCloser) Close() error { c.closed = true; return nil }

// startWatchedJob legt einen laufenden Job mit Verbindung an.
func startWatchedJob(t *testing.T, env *testEnv, serverID uint) *domain.Job {
	t.Helper()
	job, err := env.Jobs.Start(&serverID, nil, "update", "Update @ pi", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}
	env.Jobs.AttachCloser(job.ID, &nopCloser{})
	return job
}

func jobStatus(t *testing.T, env *testEnv, id string) domain.Job {
	t.Helper()
	var reloaded domain.Job
	if err := env.DB().First(&reloaded, "id = ?", id).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	return reloaded
}

// TestWatchdogLaesstArbeitendenJobLaufen: Ein Lauf, der noch Ausgabe erzeugt,
// darf beliebig lange dauern - genau das ist der Kern der Umstellung von der
// festen Maximaldauer auf die erlaubte Stille. Der Job läuft hier seit
// Stunden, hat sich aber gerade eben gemeldet.
func TestWatchdogLaesstArbeitendenJobLaufen(t *testing.T) {
	env := newTestEnv(t)
	job := startWatchedJob(t, env, joinTestServer(t, env, "pi"))

	env.Jobs.SetLastActivityForTest(job.ID, time.Now().Add(-4*time.Hour))
	env.Jobs.MarkActivity(job.ID) // gerade ist wieder Ausgabe gekommen
	env.Jobs.AbortIfStalledForTest(job, 5*time.Minute)

	if got := jobStatus(t, env, job.ID); got.Status != domain.JobStatusRunning {
		t.Errorf("arbeitender job wurde abgebrochen: %s / %s", got.Status, got.Output)
	}
}

// TestWatchdogBrichtStillenJobAb: Kommt über die erlaubte Stille hinaus nichts
// mehr, gilt der Lauf als hängend - Verbindung schließen, Sperre freigeben.
func TestWatchdogBrichtStillenJobAb(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "pi")
	job, err := env.Jobs.Start(&serverID, nil, "update", "Update @ pi", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}
	conn := &nopCloser{}
	env.Jobs.AttachCloser(job.ID, conn)

	env.Jobs.SetLastActivityForTest(job.ID, time.Now().Add(-10*time.Minute))
	env.Jobs.AbortIfStalledForTest(job, 5*time.Minute)

	got := jobStatus(t, env, job.ID)
	if got.Status != domain.JobStatusAborted {
		t.Fatalf("stiller job sollte abgebrochen sein, ist %q", got.Status)
	}
	if !conn.closed {
		t.Error("verbindung des hängenden jobs wurde nicht geschlossen")
	}
	// Die Sperre muss frei sein - genau dafür gibt es den Watchdog.
	if _, err := env.Jobs.Start(&serverID, nil, "update", "Update 2 @ pi", "admin"); err != nil {
		t.Errorf("server-sperre nach abbruch nicht frei: %v", err)
	}
}

// TestWatchdogIgnoriertJobsOhneLebenszeichenquelle: Ein reiner System-Job
// (Backup, CVE-Scan) lässt kein Kommando auf einem fremden System laufen -
// er kann dort nicht hängen und darf nicht wegen „Stille" sterben.
func TestWatchdogIgnoriertJobsOhneLebenszeichenquelle(t *testing.T) {
	env := newTestEnv(t)
	job, err := env.Jobs.Start(nil, nil, "backup", "Backup", "system")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	env.Jobs.AbortIfStalledForTest(job, time.Nanosecond)

	if got := jobStatus(t, env, job.ID); got.Status != domain.JobStatusRunning {
		t.Errorf("system-job ohne verbindung wurde abgebrochen: %s", got.Status)
	}
}

// TestWatchdogAusgeschaltet: Frist 0 bedeutet „kein Watchdog" - dann bleibt
// auch ein seit Tagen stiller Job stehen.
func TestWatchdogAusgeschaltet(t *testing.T) {
	env := newTestEnv(t)
	job := startWatchedJob(t, env, joinTestServer(t, env, "pi"))

	env.Jobs.SetLastActivityForTest(job.ID, time.Now().Add(-48*time.Hour))
	env.Jobs.AbortIfStalledForTest(job, 0)

	if got := jobStatus(t, env, job.ID); got.Status != domain.JobStatusRunning {
		t.Errorf("bei abgeschaltetem watchdog darf nichts abgebrochen werden: %s", got.Status)
	}
}
