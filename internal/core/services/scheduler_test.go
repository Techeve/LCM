package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// waitFor pollt eine Bedingung bis zu 2 Sekunden (für asynchron laufende
// Jobs). Schlägt fehl, wenn die Bedingung nicht eintritt.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bedingung nicht innerhalb von 2s erfüllt")
}

// TestSchedulerSystemSchedulesFromSettings prüft, dass Backup und
// Log-Bereinigung als system-globale Schedules (aus den Einstellungen)
// erscheinen - nicht als Gruppen-Rules.
func TestSchedulerSystemSchedulesFromSettings(t *testing.T) {
	env := newTestEnv(t)
	if err := env.Scheduler.Reload(); err != nil {
		t.Fatal(err)
	}

	overview, err := env.Scheduler.Overview()
	if err != nil {
		t.Fatal(err)
	}

	var backup, cleanup, groupSchedules int
	for _, s := range overview {
		switch s.Kind {
		case services.KindBackup:
			backup++
			if s.ScheduleID != 0 {
				t.Error("backup-schedule darf keine schedule-id haben")
			}
		case services.KindCleanup:
			cleanup++
		case services.KindSchedule:
			groupSchedules++
			if len(s.Rules) == 0 {
				t.Errorf("gruppen-schedule %q sollte seine rules mitliefern", s.Name)
			}
		}
	}
	if backup != 1 {
		t.Errorf("erwartet genau 1 backup-schedule, bekam %d", backup)
	}
	if cleanup != 1 {
		t.Errorf("erwartet genau 1 cleanup-schedule, bekam %d", cleanup)
	}
	// Die Gruppen-Schedules der System-Gruppe (Health-Check + Sync)
	// erscheinen als KindSchedule mit ihren Rules.
	if groupSchedules < 2 {
		t.Errorf("erwartet mind. 2 gruppen-schedules (health, sync), bekam %d", groupSchedules)
	}
}

// TestJederSystemScheduleIstAusloesbar: Was die Übersicht anbietet, muss sich
// auch auslösen lassen. Genau das lief auseinander - „Update-Prüfung" stand in
// der Liste (mit „Jetzt ausführen"-Knopf), TriggerSystem kannte die Art aber
// nicht und antwortete mit „unbekannter system-schedule: \"update-check\"".
//
// Der Test geht deshalb NICHT von einer Liste bekannter Kinds aus, sondern von
// dem, was die Übersicht tatsächlich ausgibt: jeder neue System-Schedule fällt
// hier automatisch mit auf.
func TestJederSystemScheduleIstAusloesbar(t *testing.T) {
	env := newTestEnv(t)
	// Optional verdrahtete Schedules ebenfalls anschalten - sonst prüft der
	// Test genau die Fälle nicht, die erst dadurch in der Liste auftauchen.
	ran := make(chan struct{}, 1)
	env.Scheduler.WithUpdateCheck(func() { ran <- struct{}{} })

	overview, err := env.Scheduler.Overview()
	if err != nil {
		t.Fatalf("übersicht: %v", err)
	}
	system := 0
	kinds := map[string]bool{}
	for _, ov := range overview {
		if ov.Kind == services.KindSchedule {
			continue // Gruppen-Schedule, läuft über TriggerScheduleNow
		}
		system++
		kinds[ov.Kind] = true
		if err := env.Scheduler.TriggerSystem(ov.Kind, "admin"); err != nil {
			t.Errorf("System-Schedule %q (%s) steht in der Übersicht, lässt sich aber nicht auslösen: %v",
				ov.Kind, ov.Name, err)
		}
	}
	if system == 0 {
		t.Fatal("keine System-Schedules in der Übersicht - der Test prüft nichts")
	}
	if !kinds[services.KindUpdate] {
		t.Error("die Update-Prüfung fehlt in der Übersicht, obwohl sie verdrahtet ist")
	}
	// Und die Update-Prüfung muss wirklich gelaufen sein, nicht nur
	// fehlerfrei angenommen worden sein.
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Error("Update-Prüfung wurde angenommen, aber nicht ausgeführt")
	}
}

// TestTriggerSystemSchedule stellt sicher, dass die manuelle Auslösung
// eines System-Schedules einen Job erzeugt.
func TestTriggerSystemSchedule(t *testing.T) {
	env := newTestEnv(t)

	// Ungültiger Kind wird abgelehnt.
	if err := env.Scheduler.TriggerSystem("gibtsnicht", "admin"); err == nil {
		t.Error("unbekannter system-schedule sollte fehler liefern")
	}

	// Cleanup läuft synchron genug: wir prüfen, dass kein Panik/Fehler
	// auftritt und danach ein cleanup-job existiert.
	env.Scheduler.TriggerSystem(services.KindCleanup, "admin")
	// TriggerSystem startet eine Goroutine; wir warten kurz über einen
	// erneuten Job-History-Poll (der Job wird protokolliert).
	waitFor(t, func() bool {
		jobs, _, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Type: domain.RuleTypeCleanup, Limit: 50})
		return len(jobs) > 0
	})
}
