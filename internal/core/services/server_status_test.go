package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// statusOf liefert die Ampel eines Servers aus StatusAll.
func statusOf(t *testing.T, env *testEnv, id uint) string {
	t.Helper()
	all, err := env.Servers.StatusAll(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range all {
		if st.ID == id {
			return st.Status
		}
	}
	t.Fatalf("server %d fehlt in StatusAll", id)
	return ""
}

func statusTestServer(t *testing.T, env *testEnv) uint {
	t.Helper()
	repo := repositories.NewServerRepository(env.db)
	s := &domain.Server{Name: "status-cache", Host: "10.9.9.9", Reachable: true}
	if err := repo.Create(s); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplacePackages(s.ID, []domain.Package{{Name: "bash", Version: "5.2"}}); err != nil {
		t.Fatal(err)
	}
	return s.ID
}

// TestStatusAllServesFromCacheUntilForgotten: Die gesammelte Ampel kommt
// aus dem Cache, bis der Server vergessen wird - dann rechnet sie neu.
func TestStatusAllServesFromCacheUntilForgotten(t *testing.T) {
	env := newTestEnv(t)
	id := statusTestServer(t, env)
	before := statusOf(t, env, id)
	if before == "red" {
		t.Fatalf("ausgangslage schon rot - test sagt nichts aus")
	}

	// Ohne Paketbestand ist der Server nicht bewertbar und damit rot.
	env.db.Exec("DELETE FROM packages")
	if got := statusOf(t, env, id); got != before {
		t.Fatalf("erwartet zwischengespeichert %q, bekam %q", before, got)
	}

	env.Servers.ForgetStatus(&id)
	if got := statusOf(t, env, id); got != "red" {
		t.Fatalf("nach dem vergessen erwartet rot, bekam %q", got)
	}
}

// TestJobEndForgetsServerStatus: Das Ende eines Jobs meldet den Server an
// den Beobachter - so verwirft der ServerService dessen Ampel.
func TestJobEndForgetsServerStatus(t *testing.T) {
	env := newTestEnv(t)
	id := statusTestServer(t, env)
	var got []*uint
	env.Jobs.OnFinished(func(serverID *uint) { got = append(got, serverID) })

	job, err := env.Jobs.Start(&id, nil, "script", "t", "test")
	if err != nil {
		t.Fatal(err)
	}
	env.Jobs.Complete(job, "", nil, nil)
	if len(got) != 1 || got[0] == nil || *got[0] != id {
		t.Fatalf("erwartet genau eine meldung für server %d, bekam %v", id, got)
	}
}
