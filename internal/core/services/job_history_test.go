package services_test

import (
	"testing"

	"LCM/internal/storage/repositories"
)

// TestJobHistoryFilteredAndPaged prüft serverseitige Filter, Pagination und
// die Gesamtanzahl.
func TestJobHistoryFilteredAndPaged(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// 5 update-Jobs + 3 health-Jobs anlegen.
	for i := 0; i < 5; i++ {
		j, _ := env.Jobs.Start(&id, nil, "update", "Update-Lauf", "admin")
		env.Jobs.Complete(j, "ok", ptrIntT(0), nil)
	}
	for i := 0; i < 3; i++ {
		j, _ := env.Jobs.Start(&id, nil, "health", "Health-Check (Ping)", "scheduler")
		env.Jobs.Complete(j, "ok", ptrIntT(0), nil)
	}

	// Ohne Filter: alle 8, Seite 1 mit page_size 5 -> 5 Einträge, total 8.
	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Limit: 5, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	// join-Job aus dem Onboarding zählt mit: 5 + 3 + 1 = 9.
	if total != 9 || len(jobs) != 5 {
		t.Fatalf("erwartet total=9, seite=5; bekam total=%d seite=%d", total, len(jobs))
	}
	// Seite 2: die restlichen 4.
	jobs2, _, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Limit: 5, Offset: 5})
	if len(jobs2) != 4 {
		t.Errorf("seite 2 sollte 4 einträge haben, bekam %d", len(jobs2))
	}

	// HideHealth: alles außer den 3 health-Jobs -> 6.
	_, totalNoHealth, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{HideHealth: true, Limit: 100})
	if totalNoHealth != 6 {
		t.Errorf("HideHealth: erwartet 6, bekam %d", totalNoHealth)
	}

	// Typ-Filter health: 3.
	_, totalHealth, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Type: "health", Limit: 100})
	if totalHealth != 3 {
		t.Errorf("Typ=health: erwartet 3, bekam %d", totalHealth)
	}

	// Auslöser-Filter scheduler: 3 (die health-Jobs).
	_, totalBy, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{TriggeredBy: "scheduler", Limit: 100})
	if totalBy != 3 {
		t.Errorf("Auslöser=scheduler: erwartet 3, bekam %d", totalBy)
	}

	// Namenssuche "Update": 5.
	_, totalName, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{NameQuery: "Update", Limit: 100})
	if totalName != 5 {
		t.Errorf("Name~Update: erwartet 5, bekam %d", totalName)
	}

	// Filter-Optionen liefern beide Typen und Auslöser.
	types, by, err := env.Jobs.FilterOptions(repositories.ScopeAll(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(types, "update") || !contains(types, "health") {
		t.Errorf("filter-optionen typen unvollständig: %v", types)
	}
	if !contains(by, "admin") || !contains(by, "scheduler") {
		t.Errorf("filter-optionen auslöser unvollständig: %v", by)
	}
}

func ptrIntT(i int) *int { return &i }

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
