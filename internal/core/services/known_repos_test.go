package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

func TestKnownReposSeededIntoCatalog(t *testing.T) {
	env := newTestEnv(t)

	repos, err := env.Settings.ListKnownRepos()
	if err != nil {
		t.Fatalf("katalog listen: %v", err)
	}
	if len(repos) != len(domain.DefaultKnownRepos()) {
		t.Fatalf("erwartet %d seed-einträge, bekommen %d", len(domain.DefaultKnownRepos()), len(repos))
	}
	// Die Server-Sicht (Dropdown im Server-Detail) liest denselben Bestand.
	catalog, err := env.Servers.KnownRepoCatalog()
	if err != nil {
		t.Fatalf("server-katalog: %v", err)
	}
	if len(catalog) != len(repos) {
		t.Errorf("server-katalog weicht ab: %d vs %d", len(catalog), len(repos))
	}
}

func TestSaveKnownRepoValidation(t *testing.T) {
	env := newTestEnv(t)
	valid := domain.KnownRepo{
		Key:  "nodejs",
		Name: "NodeSource",
		Line: "deb [signed-by=/etc/apt/keyrings/nodejs.asc] https://deb.nodesource.com/node_22.x nodistro main",
	}

	cases := []struct {
		name   string
		mutate func(r *domain.KnownRepo)
	}{
		{"key mit großbuchstaben", func(r *domain.KnownRepo) { r.Key = "NodeJS" }},
		{"key mit slash", func(r *domain.KnownRepo) { r.Key = "node/js" }},
		{"leerer name", func(r *domain.KnownRepo) { r.Name = "" }},
		{"line ohne deb-präfix", func(r *domain.KnownRepo) { r.Line = "rpm https://example.com" }},
		{"key-url ohne https", func(r *domain.KnownRepo) { r.KeyURL = "http://example.com/key.asc" }},
		{"quotes in der line", func(r *domain.KnownRepo) { r.Line = `deb "https://example.com" stable main` }},
		{"subshell in der key-url", func(r *domain.KnownRepo) { r.KeyURL = "https://example.com/$(id)" }},
		{"vergebener key", func(r *domain.KnownRepo) { r.Key = "docker" }},
	}
	for _, tc := range cases {
		r := valid
		tc.mutate(&r)
		if _, err := env.Settings.SaveKnownRepo(r, "admin"); !errors.Is(err, services.ErrKnownRepoInvalid) {
			t.Errorf("%s: erwartet ErrKnownRepoInvalid, bekommen %v", tc.name, err)
		}
	}

	if _, err := env.Settings.SaveKnownRepo(valid, "admin"); err != nil {
		t.Fatalf("gültiger eintrag abgelehnt: %v", err)
	}
}

func TestKnownRepoCRUDAndServerAction(t *testing.T) {
	env := newTestEnv(t)

	// Anlegen - taucht im Katalog auf.
	created, err := env.Settings.SaveKnownRepo(domain.KnownRepo{
		Key:    "nodejs",
		Name:   "NodeSource",
		KeyURL: "https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key",
		Line:   "deb [signed-by=/etc/apt/keyrings/nodejs.asc] https://deb.nodesource.com/node_22.x nodistro main",
	}, "admin")
	if err != nil {
		t.Fatalf("anlegen: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("keine id vergeben")
	}

	// Bearbeiten - Änderung kommt an, Key-Wechsel auf freien Key erlaubt.
	created.Name = "NodeSource (Node 22)"
	updated, err := env.Settings.SaveKnownRepo(*created, "admin")
	if err != nil {
		t.Fatalf("bearbeiten: %v", err)
	}
	if updated.Name != "NodeSource (Node 22)" {
		t.Errorf("name nicht aktualisiert: %q", updated.Name)
	}

	// Die Server-Aktion richtet den neuen Katalog-Eintrag ein (DB-Lookup).
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["keyrings/nodejs.asc"] = sshx.FakeResponse{Output: "OK\n"}
	if _, err := env.Servers.AddKnownRepository(repositories.ScopeAll(), id, "nodejs", "admin"); err != nil {
		t.Fatalf("nodejs-repo einrichten: %v", err)
	}

	// Löschen - Eintrag verschwindet, Einrichten schlägt danach fehl.
	if err := env.Settings.DeleteKnownRepo(updated.ID, "admin"); err != nil {
		t.Fatalf("löschen: %v", err)
	}
	if _, err := env.Servers.AddKnownRepository(repositories.ScopeAll(), id, "nodejs", "admin"); !errors.Is(err, services.ErrUnknownRepo) {
		t.Errorf("gelöschter eintrag noch einrichtbar: %v", err)
	}
	if err := env.Settings.DeleteKnownRepo(updated.ID, "admin"); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("doppeltes löschen: erwartet ErrNotFound, bekommen %v", err)
	}
}
