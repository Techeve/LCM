package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// Der Einzelscan erfasste nur Betriebssystempakete, meldete aber eine
// Gesamtzahl: auf einem Server mit 2475 Container-Funden (22 kritisch, 291
// hoch) lautete die Antwort „0 kritische, 0 hohe" - formal auf den eigenen
// Ausschnitt bezogen richtig, in der Sache eine Entwarnung für etwas, das gar
// nicht geprüft wurde (R2-086). Wer nach einer Meldung gezielt nachscannt,
// bekam so das Gegenteil der Wahrheit.

// TestEinzelscanNenntSeinenGegenstand: die Zahlen müssen sagen, worauf sie
// sich beziehen - und die Container-Seite muss mitgescannt werden.
func TestEinzelscanNenntSeinenGegenstand(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Server mit Docker, aber ohne erfasste Images: der Scan muss trotzdem
	// eine Aussage über die Container-Seite treffen.
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Update("has_docker", true).Error; err != nil {
		t.Fatal(err)
	}

	env.Scheduler.TriggerCVEScanServer(id, "admin")
	job := waitForJobByName(t, env, "CVE-Scan @ web01")
	if job.Status != domain.JobStatusSuccess {
		t.Fatalf("scan fehlgeschlagen: %s", job.Output)
	}
	if !strings.Contains(job.Output, "Betriebssystempaketen") {
		t.Errorf("die Meldung benennt ihren Gegenstand nicht:\n%s", job.Output)
	}
	if !strings.Contains(job.Output, "Image") {
		t.Errorf("die Container-Seite kommt in der Meldung nicht vor:\n%s", job.Output)
	}
}

// TestEinzelscanOhneDockerSagtDasAuch: auf einem Server ohne Docker soll die
// Meldung das ausdrücklich festhalten, statt die Frage offenzulassen.
func TestEinzelscanOhneDockerSagtDasAuch(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Update("has_docker", false).Error; err != nil {
		t.Fatal(err)
	}

	env.Scheduler.TriggerCVEScanServer(id, "admin")
	job := waitForJobByName(t, env, "CVE-Scan @ web01")
	if job.Status != domain.JobStatusSuccess {
		t.Fatalf("scan fehlgeschlagen: %s", job.Output)
	}
	if !strings.Contains(job.Output, "kein Docker") {
		t.Errorf("die Meldung hält nicht fest, dass es keine Container gibt:\n%s", job.Output)
	}
}

// waitForJobByName wartet auf den jüngsten Job mit diesem Namen. Der
// Einzelscan läuft asynchron und liefert keine Job-ID zurück.
func waitForJobByName(t *testing.T, env *testEnv, name string) *domain.Job {
	t.Helper()
	var job domain.Job
	waitFor(t, func() bool {
		err := env.DB().Where("name = ? AND status <> ?", name, domain.JobStatusRunning).
			Order("created_at DESC").First(&job).Error
		return err == nil
	})
	return &job
}
