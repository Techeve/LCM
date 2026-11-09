package services_test

import (
	"errors"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// TestWartungSperrtUnmittelbareAktionen: Während LCM sich aktualisiert, startet
// keine Aktion mehr. Der Versuch steht als blockiert in der Historie, damit
// klar ist, warum nichts passiert ist.
func TestWartungSperrtUnmittelbareAktionen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Jobs.BeginMaintenance()

	job, err := env.Jobs.Start(&id, nil, domain.RuleTypeUpdate, "Pakete aktualisieren", "admin")
	if !errors.Is(err, services.ErrMaintenance) {
		t.Fatalf("erwartet ErrMaintenance, bekam %v", err)
	}
	if job == nil || job.Status != domain.JobStatusBlocked {
		t.Fatalf("erwartet blockierten Eintrag, bekam %+v", job)
	}
	if _, err := env.Jobs.Start(nil, nil, domain.RuleTypeScript, "System-Job", "scheduler"); !errors.Is(err, services.ErrMaintenance) {
		t.Errorf("auch System-Jobs warten: bekam %v", err)
	}

	env.Jobs.EndMaintenance()
	if _, err := env.Jobs.Start(&id, nil, domain.RuleTypeUpdate, "Pakete aktualisieren", "admin"); err != nil {
		t.Errorf("nach der Wartung muss der Start gehen: %v", err)
	}
}

// TestWartungLaesstGenauDenUpdateLaufDurch: Der Passierschein gilt für einen
// Job dieser Art auf diesem Server - einmal.
func TestWartungLaesstGenauDenUpdateLaufDurch(t *testing.T) {
	env := newTestEnv(t)
	host := joinTestServer(t, env, "lcm-host")
	other := joinTestServer(t, env, "web01")
	env.Jobs.BeginMaintenance()
	env.Jobs.PermitDuringMaintenance(host, domain.RuleTypeUpdate, "LCM auf 9.9.9 aktualisieren")

	if _, err := env.Jobs.Start(&other, nil, domain.RuleTypeUpdate, "fremder Server", "admin"); !errors.Is(err, services.ErrMaintenance) {
		t.Errorf("anderer Server darf nicht durch: %v", err)
	}
	if _, err := env.Jobs.Start(&host, nil, domain.RuleTypeReboot, "andere Art", "admin"); !errors.Is(err, services.ErrMaintenance) {
		t.Errorf("andere Job-Art darf nicht durch: %v", err)
	}
	if _, err := env.Jobs.Start(&host, nil, domain.RuleTypeUpdate, "Alle Pakete aktualisieren @ lcm-host", "admin"); !errors.Is(err, services.ErrMaintenance) {
		t.Errorf("ein anderes Update auf dem Host darf den Schein nicht nehmen: %v", err)
	}
	job, err := env.Jobs.Start(&host, nil, domain.RuleTypeUpdate, "LCM auf 9.9.9 aktualisieren @ lcm-host", "admin")
	if err != nil {
		t.Fatalf("der Update-Lauf muss durch: %v", err)
	}
	env.Jobs.Complete(job, "", ptrIntTest(0), nil)
	if _, err := env.Jobs.Start(&host, nil, domain.RuleTypeUpdate, "zweiter Lauf", "admin"); !errors.Is(err, services.ErrMaintenance) {
		t.Errorf("der Schein gilt nur einmal: %v", err)
	}
}

// TestZeitplanLaufWartetBisNachDerWartung: Ein Zeitplan-Lauf wird nicht
// verworfen, sondern wartet - auch auf einem freien Server - und startet,
// sobald die Sperre fällt.
func TestZeitplanLaufWartetBisNachDerWartung(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Jobs.BeginMaintenance()

	done := warteschlangeStart(env, id, 7, "Sync @ web01", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)

	env.Jobs.EndMaintenance()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("nach der Wartung hätte er laufen müssen: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("der wartende Lauf startete nach der Wartung nicht")
	}
}

// TestWartungHaeltDieSchlangeAn: Endet während der Wartung ein Job, rückt der
// nächste Wartende NICHT nach - er liefe sonst genau in den Neustart.
func TestWartungHaeltDieSchlangeAn(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	laufend := laufendenJobStarten(t, env, id)
	done := warteschlangeStart(env, id, 7, "Sync @ web01", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)

	env.Jobs.BeginMaintenance()
	env.Jobs.Complete(laufend, "fertig", ptrIntTest(0), nil)
	time.Sleep(50 * time.Millisecond)
	if n := len(env.Jobs.QueuedForServer(id)); n != 1 {
		t.Fatalf("der Wartende ist während der Wartung nachgerückt (Schlange: %d)", n)
	}

	env.Jobs.EndMaintenance()
	if err := <-done; err != nil {
		t.Fatalf("nach der Wartung hätte er laufen müssen: %v", err)
	}
}
