package services_test

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// weitergabeVerzoegern bremst das Schreiben jeder Job-Zeile, die auf „läuft"
// wechselt. Das macht die Lücke in startNext breit, die im Betrieb nur
// Mikrosekunden misst: Der Wartende läuft im Prozess schon, seine Zeile steht
// in der Datenbank noch auf „wartend".
func weitergabeVerzoegern(t *testing.T, env *testEnv, pause time.Duration) {
	t.Helper()
	err := env.DB().Callback().Update().Before("gorm:begin_transaction").Register("test:weitergabe_verzoegern", func(tx *gorm.DB) {
		if job, ok := tx.Statement.Dest.(*domain.Job); ok && job.Status == domain.JobStatusRunning {
			time.Sleep(pause)
		}
	})
	if err != nil {
		t.Fatalf("Callback registrieren: %v", err)
	}
}

// laufendeJobs zählt die Jobs eines Servers, die laut Datenbank laufen.
func laufendeJobs(t *testing.T, env *testEnv, serverID uint) int64 {
	t.Helper()
	var n int64
	if err := env.DB().Model(&domain.Job{}).
		Where("server_id = ? AND status = ?", serverID, domain.JobStatusRunning).
		Count(&n).Error; err != nil {
		t.Fatalf("laufende Jobs zählen: %v", err)
	}
	return n
}

// TestKeinStartWaehrendDerWeitergabe: Endet ein Job, gibt startNext den Server
// an den Wartenden weiter. Bisher fragten Start und StartOrQueue nur die
// Datenbank - und die sagte „frei", bis die Zeile des Wartenden geschrieben
// war. Wer in dieser Lücke kam, lief neben ihm auf demselben Server.
func TestKeinStartWaehrendDerWeitergabe(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	weitergabeVerzoegern(t, env, 300*time.Millisecond)
	laufend := laufendenJobStarten(t, env, id)
	wartend := warteschlangeStart(env, id, 7, "Sync @ web01", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)

	go env.Jobs.Complete(laufend, "fertig", ptrIntTest(0), nil)
	warteAufStatus(t, env, id, 0) // der Wartende ist aus der Schlange genommen

	if _, err := env.Jobs.Start(&id, nil, domain.RuleTypeHealth, "Aktion", "admin"); !errors.Is(err, services.ErrServerBusy) {
		t.Errorf("Start während der Weitergabe: err = %v, erwartet ErrServerBusy", err)
	}
	if _, err := env.Jobs.StartOrQueue(services.QueuedStart{
		ServerID: id, Type: domain.RuleTypeHealth, Name: "Zeitplan", TriggeredBy: "scheduler",
	}); !errors.Is(err, services.ErrServerBusy) {
		t.Errorf("Zeitplan-Lauf während der Weitergabe: err = %v, erwartet ErrServerBusy", err)
	}
	if err := <-wartend; err != nil {
		t.Fatalf("der Wartende hätte laufen müssen: %v", err)
	}
	if n := laufendeJobs(t, env, id); n != 1 {
		t.Errorf("%d Jobs laufen auf dem Server, erwartet genau einer", n)
	}
}

// TestSpaetesCompleteNachAbbruchGibtNichtFrei: abort() gibt den Server sofort
// an den nächsten Wartenden weiter. Kehrt danach die abgebrochene Goroutine
// zurück, darf ihr Complete ihn nicht ein zweites Mal weitergeben - sonst
// liefe der Übernächste neben dem Nächsten.
func TestSpaetesCompleteNachAbbruchGibtNichtFrei(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	weitergabeVerzoegern(t, env, 300*time.Millisecond)
	laufend := laufendenJobStarten(t, env, id)
	naechster := warteschlangeStart(env, id, 1, "zuerst", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 1)
	uebernaechster := warteschlangeStart(env, id, 2, "danach", domain.DefaultGroupPriority, 5*time.Second)
	warteAufStatus(t, env, id, 2)

	abgebrochen := make(chan error, 1)
	go func() {
		_, err := env.Jobs.Abort(repositories.ScopeAll(), laufend.ID, "admin")
		abgebrochen <- err
	}()
	warteAufStatus(t, env, id, 1) // „zuerst" wird gerade weitergegeben
	env.Jobs.Complete(laufend, "abgeschnitten", nil, errors.New("verbindung geschlossen"))

	if err := <-abgebrochen; err != nil {
		t.Fatalf("Abbruch: %v", err)
	}
	if err := <-naechster; err != nil {
		t.Fatalf("der Nächste hätte laufen müssen: %v", err)
	}
	if q := env.Jobs.QueuedForServer(id); len(q) != 1 || q[0].Name != "danach" {
		t.Fatalf("der Übernächste muss weiter warten, Schlange: %v", q)
	}
	if n := laufendeJobs(t, env, id); n != 1 {
		t.Fatalf("%d Jobs laufen auf dem Server, erwartet genau einer", n)
	}
	beenden(t, env, id)
	if err := <-uebernaechster; err != nil {
		t.Fatalf("der Übernächste hätte danach laufen müssen: %v", err)
	}
}

// beenden schließt den laufenden Job eines Servers regulär ab.
func beenden(t *testing.T, env *testEnv, id uint) {
	t.Helper()
	job, err := env.Jobs.RunningForServer(id)
	if err != nil || job == nil {
		t.Fatalf("laufender Job: %v, %v", job, err)
	}
	env.Jobs.Complete(job, "fertig", ptrIntTest(0), nil)
}
