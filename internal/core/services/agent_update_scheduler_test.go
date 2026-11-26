package services_test

import (
	"sync/atomic"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestSchedulerStartetDasFensterEinerEingeschaltetenRegel: Eine eingeschaltete
// Regel bekommt einen Cron-Eintrag, der den Lauf ihrer Gruppe startet; eine
// ausgeschaltete nicht. Speichern lädt den Zeitplaner neu.
func TestSchedulerStartetDasFensterEinerEingeschaltetenRegel(t *testing.T) {
	env := newTestEnv(t)
	policies := repositories.NewAgentUpdateRepository(env.DB())
	var runs atomic.Int32
	var group atomic.Uint32
	env.Scheduler.WithAgentUpdates(policies.Enabled, func(id uint) {
		group.Store(uint32(id))
		runs.Add(1)
	})
	svc := services.NewAgentUpdateService(policies, repositories.NewGroupRepository(env.DB()),
		repositories.NewServerRepository(env.DB()), env.Servers, nil, env.Scheduler.Reload)
	if err := env.Scheduler.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Scheduler.Stop)

	// Aus: auch nach Sekunden kein Lauf.
	if _, err := svc.Save(7, services.AgentUpdateInput{Mode: domain.AgentUpdateOff, CronExpr: "@every 1s"}, "test"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if n := runs.Load(); n != 0 {
		t.Fatalf("ausgeschaltete regel lief %d-mal", n)
	}

	// An: Das Speichern lädt den Zeitplaner neu, das Fenster beginnt.
	if _, err := svc.Save(7, services.AgentUpdateInput{Mode: domain.AgentUpdateAuto, Staged: true, CronExpr: "@every 1s"}, "test"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if runs.Load() == 0 || group.Load() != 7 {
		t.Fatalf("das fenster startete keinen lauf für die gruppe (läufe %d, gruppe %d)", runs.Load(), group.Load())
	}

	// Wieder aus: Es kommen keine weiteren Läufe.
	if _, err := svc.Save(7, services.AgentUpdateInput{Mode: domain.AgentUpdateOff, CronExpr: "@every 1s"}, "test"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	settled := runs.Load()
	time.Sleep(1500 * time.Millisecond)
	if runs.Load() != settled {
		t.Errorf("nach dem Ausschalten liefen weitere Läufe: %d → %d", settled, runs.Load())
	}
}
