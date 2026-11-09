package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// eventsEnv ist ein Job-Dienst mit Ereignisbus. Er teilt sich die Datenbank mit
// dem Test-Server, hat aber seine eigene Warteschlange.
func eventsEnv(t *testing.T) (*services.JobService, *events.Subscription, uint) {
	t.Helper()
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	bus := events.NewBus()
	jobs := services.NewJobService(repositories.NewJobRepository(env.DB())).WithEvents(bus)
	sub := bus.Subscribe()
	t.Cleanup(sub.Close)
	return jobs, sub, serverID
}

// next liest das nächste Ereignis oder lässt den Test scheitern.
func next(t *testing.T, sub *events.Subscription) events.Event {
	t.Helper()
	select {
	case e := <-sub.Events():
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("kein Ereignis angekommen")
		return events.Event{}
	}
}

func busyOf(t *testing.T, e events.Event) events.ServerBusyData {
	t.Helper()
	if e.Type != events.TypeServerBusy {
		t.Fatalf("erwartet server.busy, bekam %s", e.Type)
	}
	return e.Data.(events.ServerBusyData)
}

func TestJobLebenszyklusWirdGemeldet(t *testing.T) {
	jobs, sub, serverID := eventsEnv(t)

	job, err := jobs.Start(&serverID, nil, domain.RuleTypeSecurity, "Updates", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if b := busyOf(t, next(t, sub)); !b.Busy || b.JobID != job.ID || b.Kind != domain.RuleTypeSecurity || b.Queued != 0 {
		t.Errorf("Start: %+v", b)
	}

	jobs.Complete(job, "fertig", nil, nil)
	fin := next(t, sub)
	if fin.Type != events.TypeJobFinished || fin.JobID != job.ID || fin.ServerID != serverID {
		t.Fatalf("Ende: %+v", fin)
	}
	if d := fin.Data.(events.JobFinishedData); d.Status != domain.JobStatusSuccess {
		t.Errorf("Status = %s", d.Status)
	}
	if b := busyOf(t, next(t, sub)); b.Busy {
		t.Errorf("nach dem Ende darf der Server nicht mehr belegt sein: %+v", b)
	}
}

func TestSystemJobOhneServerMeldetNurDasEnde(t *testing.T) {
	jobs, sub, _ := eventsEnv(t)

	job, err := jobs.Start(nil, nil, domain.RuleTypeBackup, "Backup", "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	jobs.Complete(job, "", nil, nil)

	fin := next(t, sub)
	if fin.Type != events.TypeJobFinished || fin.ServerID != 0 {
		t.Errorf("erwartet job.finished ohne Server, bekam %+v", fin)
	}
	select {
	case e := <-sub.Events():
		t.Errorf("unerwartetes Ereignis für einen serverlosen Job: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWartenderJobMeldetWarteschlange(t *testing.T) {
	jobs, sub, serverID := eventsEnv(t)
	running, err := jobs.Start(&serverID, nil, domain.RuleTypeHealth, "läuft", "admin")
	if err != nil {
		t.Fatal(err)
	}
	next(t, sub) // server.busy des ersten Jobs

	ruleID := uint(3)
	started := make(chan *domain.Job, 1)
	go func() {
		job, _ := jobs.StartOrQueue(services.QueuedStart{
			ServerID: serverID, RuleID: &ruleID, Type: domain.RuleTypeSync, Name: "Sync",
			TriggeredBy: "scheduler", Priority: domain.DefaultGroupPriority, MaxWait: 5 * time.Second,
		})
		started <- job
	}()
	if b := busyOf(t, next(t, sub)); !b.Busy || b.Queued != 1 || b.Kind != domain.RuleTypeSync {
		t.Errorf("Einreihen: %+v", b)
	}

	jobs.Complete(running, "", nil, nil)
	// Ende des ersten, Freigabe, Start des Wartenden (Zähler wieder 0).
	if e := next(t, sub); e.Type != events.TypeJobFinished || e.JobID != running.ID {
		t.Fatalf("Ende: %+v", e)
	}
	if b := busyOf(t, next(t, sub)); b.Busy || b.Queued != 1 {
		t.Errorf("Freigabe: %+v", b)
	}
	if b := busyOf(t, next(t, sub)); !b.Busy || b.Queued != 0 || b.Kind != domain.RuleTypeSync {
		t.Errorf("Start des Wartenden: %+v", b)
	}
	if job := <-started; job == nil || job.Status != domain.JobStatusRunning {
		t.Errorf("der Wartende lief nicht an: %+v", job)
	}
}

func TestAbbruchMeldetEndeDesJobs(t *testing.T) {
	jobs, sub, serverID := eventsEnv(t)
	job, err := jobs.Start(&serverID, nil, domain.RuleTypeSecurity, "Updates", "admin")
	if err != nil {
		t.Fatal(err)
	}
	next(t, sub)

	if _, err := jobs.Abort(repositories.ScopeAll(), job.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	fin := next(t, sub)
	if fin.Type != events.TypeJobFinished || fin.Data.(events.JobFinishedData).Status != domain.JobStatusAborted {
		t.Errorf("Abbruch: %+v", fin)
	}
	if b := busyOf(t, next(t, sub)); b.Busy {
		t.Errorf("nach dem Abbruch ist der Server frei: %+v", b)
	}
	// Das spätere Complete der Goroutine darf nicht ein zweites Mal melden.
	jobs.Complete(job, "", nil, nil)
	select {
	case e := <-sub.Events():
		t.Errorf("doppelte Meldung: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStatusVerwerfenWirdGemeldet(t *testing.T) {
	env := newTestEnv(t)
	bus := events.NewBus()
	env.Servers.WithEvents(bus)
	sub := bus.Subscribe()
	defer sub.Close()

	id := uint(5)
	env.Servers.ForgetStatus(&id)
	if e := next(t, sub); e.Type != events.TypeServerStatus || e.ServerID != 5 || e.All {
		t.Errorf("ein Server: %+v", e)
	}
	env.Servers.ForgetStatus(nil)
	if e := next(t, sub); !e.All {
		t.Errorf("alle Server: %+v", e)
	}
}
