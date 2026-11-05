package services_test

import (
	"slices"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// listEnv legt drei Server mit unterschiedlichem OS, Kernel und Kontakt an.
func listEnv(t *testing.T) (*testEnv, map[string]uint) {
	t.Helper()
	env := newTestEnv(t)
	repo := repositories.NewServerRepository(env.DB())
	ids := map[string]uint{}
	now := time.Now()
	setups := []struct {
		name, os, kernel string
		seen             time.Time
	}{
		{"web-berlin", "Debian GNU/Linux", "6.1.0-13-amd64", now.Add(-time.Hour)},
		{"Db-Hamburg", "Ubuntu", "5.15.0-91-generic", now.Add(-time.Minute)},
		{"cache-koeln", "Rocky Linux", "5.14.0-362.el9", now.Add(-48 * time.Hour)},
	}
	for _, setup := range setups {
		id := joinTestServer(t, env, setup.name)
		srv, err := repo.FindByIDUnscoped(id)
		if err != nil {
			t.Fatal(err)
		}
		srv.OSName, srv.KernelVersion, srv.LastSeenAt = setup.os, setup.kernel, &setup.seen
		if err := repo.Update(srv); err != nil {
			t.Fatal(err)
		}
		ids[setup.name] = id
	}
	return env, ids
}

func listNames(page *services.ServerPage) []string {
	var names []string
	for _, item := range page.Items {
		names = append(names, item.Name)
	}
	return names
}

func TestServerListeFiltertNachText(t *testing.T) {
	env, _ := listEnv(t)
	all := repositories.ScopeAll()
	for _, tc := range []struct {
		name  string
		query services.ServerListQuery
		want  []string
	}{
		{"alle, Vorgabe nach Name", services.ServerListQuery{}, []string{"cache-koeln", "Db-Hamburg", "web-berlin"}},
		{"q in Name ohne Groß-/Kleinschreibung", services.ServerListQuery{Q: "HAMBURG"}, []string{"Db-Hamburg"}},
		{"q im Host", services.ServerListQuery{Q: "koeln.test"}, []string{"cache-koeln"}},
		{"genauer Name", services.ServerListQuery{Name: "db-hamburg"}, []string{"Db-Hamburg"}},
		{"Name ist kein Teilstring", services.ServerListQuery{Name: "db"}, nil},
		{"os", services.ServerListQuery{OS: "ubuntu"}, []string{"Db-Hamburg"}},
		{"kernel: Präfix je Stelle", services.ServerListQuery{Kernel: "5"}, []string{"cache-koeln", "Db-Hamburg"}},
		{"kernel: zweite Stelle genau", services.ServerListQuery{Kernel: "5.15"}, []string{"Db-Hamburg"}},
		{"kernel: 1 trifft nicht 15 oder 0-13", services.ServerListQuery{Kernel: "1"}, nil},
		{"kernel: Textpräfix in der letzten Stelle", services.ServerListQuery{Kernel: "6.1.0-13"}, []string{"web-berlin"}},
		{"os und kernel verknüpft", services.ServerListQuery{OS: "linux", Kernel: "5.14"}, []string{"cache-koeln"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := env.Servers.ListPage(all, tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if got := listNames(page); !slices.Equal(got, tc.want) || page.Total != len(tc.want) {
				t.Errorf("%v (total %d), erwartet %v", got, page.Total, tc.want)
			}
		})
	}
}

func TestServerListeNenntVorkommendeBetriebssysteme(t *testing.T) {
	env, _ := listEnv(t)
	page, err := env.Servers.ListPage(repositories.ScopeAll(), services.ServerListQuery{OS: "ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Debian GNU/Linux", "Rocky Linux", "Ubuntu"}
	if !slices.Equal(page.OSNames, want) {
		t.Errorf("os_names %v, erwartet %v (vor den Textfiltern)", page.OSNames, want)
	}
}

func TestServerListeSortiert(t *testing.T) {
	env, _ := listEnv(t)
	for _, tc := range []struct {
		sort string
		want []string
	}{
		{"name", []string{"cache-koeln", "Db-Hamburg", "web-berlin"}},
		{"-name", []string{"web-berlin", "Db-Hamburg", "cache-koeln"}},
		{"last_seen", []string{"cache-koeln", "web-berlin", "Db-Hamburg"}},
		{"-last_seen", []string{"Db-Hamburg", "web-berlin", "cache-koeln"}},
		{"os", []string{"web-berlin", "cache-koeln", "Db-Hamburg"}}, // Debian, Rocky, Ubuntu
		{"-kernel", []string{"web-berlin", "Db-Hamburg", "cache-koeln"}},
	} {
		t.Run(tc.sort, func(t *testing.T) {
			page, err := env.Servers.ListPage(repositories.ScopeAll(), services.ServerListQuery{Sort: tc.sort})
			if err != nil {
				t.Fatal(err)
			}
			if got := listNames(page); !slices.Equal(got, tc.want) {
				t.Errorf("%v, erwartet %v", got, tc.want)
			}
		})
	}
}

func TestServerListeBlaettertUndZaehltAlleTreffer(t *testing.T) {
	env, _ := listEnv(t)
	all := repositories.ScopeAll()
	page, err := env.Servers.ListPage(all, services.ServerListQuery{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := listNames(page); !slices.Equal(got, []string{"Db-Hamburg", "web-berlin"}) || page.Total != 3 {
		t.Errorf("Seite 2: %v, total %d", got, page.Total)
	}
	page, _ = env.Servers.ListPage(all, services.ServerListQuery{Limit: 2, Offset: 5})
	if len(page.Items) != 0 || page.Total != 3 {
		t.Errorf("hinter dem Ende: %d Einträge, total %d", len(page.Items), page.Total)
	}
}

func TestServerListeGruppeUndKennungen(t *testing.T) {
	env, ids := listEnv(t)
	group, err := env.Groups.Create("Nord", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, ids["Db-Hamburg"], "admin"); err != nil {
		t.Fatal(err)
	}
	all := repositories.ScopeAll()
	page, _ := env.Servers.ListPage(all, services.ServerListQuery{GroupID: group.ID})
	if got := listNames(page); !slices.Equal(got, []string{"Db-Hamburg"}) {
		t.Errorf("group_id: %v", got)
	}
	if len(page.Items) == 1 && len(page.Items[0].Groups) == 0 {
		t.Error("der Eintrag trägt seine Gruppen nicht")
	}
	page, _ = env.Servers.ListPage(all, services.ServerListQuery{IDs: []uint{ids["web-berlin"], ids["cache-koeln"]}})
	if got := listNames(page); !slices.Equal(got, []string{"cache-koeln", "web-berlin"}) {
		t.Errorf("ids: %v", got)
	}
}

func TestServerListeAmpelFilterUndSortierung(t *testing.T) {
	env, ids := listEnv(t)
	repo := repositories.NewServerRepository(env.DB())
	// Einer nicht erreichbar -> rot; die übrigen bleiben, wie sie sind.
	if err := repo.UpdateFields(ids["Db-Hamburg"], map[string]any{"reachable": false, "failed_checks": 5}); err != nil {
		t.Fatal(err)
	}
	env.Servers.ForgetStatus(nil)
	all := repositories.ScopeAll()

	page, err := env.Servers.ListPage(all, services.ServerListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, item := range page.Items {
		status[item.Name] = item.Status
		if item.Status == "" || item.Insights == nil {
			t.Errorf("%s: Ampel fehlt (%q)", item.Name, item.Status)
		}
	}
	var red []string
	for name, st := range status {
		if st == domain.ServerStatusRed {
			red = append(red, name)
		}
	}
	if !slices.Contains(red, "Db-Hamburg") {
		t.Fatalf("Db-Hamburg soll rot sein: %v", status)
	}

	page, _ = env.Servers.ListPage(all, services.ServerListQuery{Statuses: []string{domain.ServerStatusRed}})
	for _, item := range page.Items {
		if item.Status != domain.ServerStatusRed {
			t.Errorf("Filter red lieferte %s mit %s", item.Name, item.Status)
		}
	}
	if page.Total != len(red) {
		t.Errorf("total %d, erwartet %d", page.Total, len(red))
	}

	// „status": schlechtester zuerst, „-status": bester zuerst.
	worst, _ := env.Servers.ListPage(all, services.ServerListQuery{Sort: "status"})
	best, _ := env.Servers.ListPage(all, services.ServerListQuery{Sort: "-status"})
	if worst.Items[0].Status != domain.ServerStatusRed {
		t.Errorf("status: zuerst %s, erwartet red", worst.Items[0].Status)
	}
	if best.Items[len(best.Items)-1].Status != domain.ServerStatusRed {
		t.Errorf("-status: zuletzt %s, erwartet red", best.Items[len(best.Items)-1].Status)
	}
}

func TestServerListeZaehltUpdates(t *testing.T) {
	env, ids := listEnv(t)
	repo := repositories.NewServerRepository(env.DB())
	// Zwei überfällige Pakete auf einem Server, keins auf den anderen.
	if err := repo.ReplacePackages(ids["web-berlin"], []domain.Package{
		{Name: "a", Version: "1", CandidateVersion: "2"},
		{Name: "b", Version: "1", CandidateVersion: "3"},
		{Name: "c", Version: "1", CandidateVersion: "1"},
	}); err != nil {
		t.Fatal(err)
	}
	all := repositories.ScopeAll()
	page, _ := env.Servers.ListPage(all, services.ServerListQuery{Sort: "-updates"})
	if page.Items[0].Name != "web-berlin" || page.Items[0].Updates != 2 {
		t.Errorf("-updates: zuerst %s mit %d", page.Items[0].Name, page.Items[0].Updates)
	}
	page, _ = env.Servers.ListPage(all, services.ServerListQuery{Q: "berlin"})
	if page.Items[0].Updates != 2 {
		t.Errorf("updates ohne Updates-Sortierung: %d, erwartet 2", page.Items[0].Updates)
	}
}

func TestServerListeNurImSichtbereich(t *testing.T) {
	env, ids := listEnv(t)
	group, err := env.Groups.Create("Eigene", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, ids["web-berlin"], "admin"); err != nil {
		t.Fatal(err)
	}
	manager, err := env.Users.CreateUser("mia", "", "Anker5-Leuchtturm!Wind", "", "", []string{"manager"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignManager(repositories.ScopeAll(), group.ID, manager.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	scope := repositories.ScopeManager(manager.ID)
	page, err := env.Servers.ListPage(scope, services.ServerListQuery{Q: "hamburg"})
	if err != nil || len(page.Items) != 0 || page.Total != 0 {
		t.Errorf("fremder Server per q: %v, %+v", err, page)
	}
	page, _ = env.Servers.ListPage(scope, services.ServerListQuery{IDs: []uint{ids["Db-Hamburg"], ids["web-berlin"]}})
	if got := listNames(page); !slices.Equal(got, []string{"web-berlin"}) {
		t.Errorf("ids über den Sichtbereich hinaus: %v", got)
	}
	page, _ = env.Servers.ListPage(scope, services.ServerListQuery{Statuses: []string{"green", "yellow", "red", "excellent"}, Sort: "status"})
	if page.Total != 1 {
		t.Errorf("Ampel-Filter im Sichtbereich: total %d, erwartet 1", page.Total)
	}
}

// Die Liste meldet den LCM-Host selbst (Loopback und Standardport), damit die
// Oberfläche die Regel nicht nachbauen muss. Ein Port-Forward über 127.0.0.1
// auf einen hohen Port ist kein LCM-Host.
func TestServerListeMeldetLcmHost(t *testing.T) {
	env, ids := listEnv(t)
	repo := repositories.NewServerRepository(env.DB())
	for name, port := range map[string]int{"web-berlin": 22, "Db-Hamburg": 2222} {
		srv, err := repo.FindByIDUnscoped(ids[name])
		if err != nil {
			t.Fatal(err)
		}
		srv.Host, srv.SSHPort = "localhost", port
		if err := repo.Update(srv); err != nil {
			t.Fatal(err)
		}
	}
	page, err := env.Servers.ListPage(repositories.ScopeAll(), services.ServerListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if want := item.Name == "web-berlin"; item.IsLcmHost != want {
			t.Errorf("%s: is_lcm_host = %v, erwartet %v", item.Name, item.IsLcmHost, want)
		}
	}
}
