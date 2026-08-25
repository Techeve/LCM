package services_test

import (
	"context"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestPollNowIstSichtbar: Der Durchgang auf Knopfdruck erzeugt - anders als
// der Viertelstundentakt - IMMER einen Job. Wer ihn ausloest, will das
// Ergebnis sehen, auch wenn es "keine neuen Befunde" lautet.
func TestPollNowIstSichtbar(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	if _, err := env.Scheduler.TriggerAdvisoryPoll("admin"); err != nil {
		t.Fatalf("TriggerAdvisoryPoll: %v", err)
	}
	// Der Lauf arbeitet im Hintergrund - auf seinen Abschluss warten.
	waitFor(t, func() bool {
		_, total, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(),
			repositories.JobFilter{NameQuery: "Fruehwarnung", Status: "success", Limit: 5})
		return total > 0
	})

	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(),
		repositories.JobFilter{NameQuery: "Fruehwarnung", Limit: 5})
	if err != nil || total != 1 {
		t.Fatalf("erwartet genau 1 Job, bekam %d (%v)", total, err)
	}
	if jobs[0].Status != "success" {
		t.Errorf("Job sollte success sein, war %q", jobs[0].Status)
	}
	if !strings.Contains(jobs[0].Output, "geprüft") {
		t.Errorf("Zusammenfassung fehlt im Job: %q", jobs[0].Output)
	}
}

// TestPollSchreibtZeitstempelFort: Ohne ihn ist eine leere Liste nicht von
// "noch nie nachgesehen" zu unterscheiden.
func TestPollSchreibtZeitstempelFort(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 10)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	if !env.Advisories.LastPollAt().IsZero() {
		t.Fatal("vor dem ersten Lauf darf kein Zeitstempel stehen")
	}
	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if env.Advisories.LastPollAt().IsZero() {
		t.Error("nach dem Lauf muss der Zeitstempel stehen")
	}
}
