package services_test

import (
	"slices"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// dashboardFixture: zwei Mandanten. Manager 1 verwaltet „alpha" (Gruppe ga),
// Manager 2 „beta" (Gruppe gb); der Admin sieht beides.
type dashboardFixture struct {
	env               *testEnv
	bus               *events.Bus
	dash              *services.DashboardService
	alpha, beta       uint
	scope1, scope2    repositories.AccessScope
	groupA, groupB    *domain.ServerGroup
	scheduleA, schedB *domain.Schedule
	alertRepo         *repositories.AlertRepository
}

func newDashboardFixture(t *testing.T) *dashboardFixture {
	t.Helper()
	env := newTestEnv(t)
	f := &dashboardFixture{env: env, bus: events.NewBus()}
	f.alpha = joinTestServer(t, env, "alpha")
	f.beta = joinTestServer(t, env, "beta")

	f.groupA, f.scope1 = f.tenant(t, "ga", "manager1", f.alpha)
	f.groupB, f.scope2 = f.tenant(t, "gb", "manager2", f.beta)

	f.scheduleA = f.schedule(t, f.groupA, "Nacht A", "0 3 * * *")
	f.schedB = f.schedule(t, f.groupB, "Nacht B", "30 4 * * *")

	db := env.DB()
	f.alertRepo = repositories.NewAlertRepository(db)
	f.dash = services.NewDashboardService(env.Servers, repositories.NewJobRepository(db),
		f.alertRepo, repositories.NewGroupRepository(db), f.bus)
	return f
}

// tenant legt Gruppe und Manager an und gibt dem Manager genau diesen einen Server.
func (f *dashboardFixture) tenant(t *testing.T, group, username string, serverID uint) (*domain.ServerGroup, repositories.AccessScope) {
	t.Helper()
	g, err := f.env.Groups.Create(group, "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.env.Groups.AssignServer(repositories.ScopeAll(), g.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	user, err := f.env.Users.CreateUser(username, username+"@example.com",
		"Anker5-Leuchtturm!Wind", "Mana", "Ger", []string{domain.RoleManager}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.env.Groups.AssignManager(repositories.ScopeAll(), g.ID, user.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	return g, repositories.ScopeManager(user.ID)
}

func (f *dashboardFixture) schedule(t *testing.T, g *domain.ServerGroup, name, cron string) *domain.Schedule {
	t.Helper()
	sc, err := f.env.Groups.DefineSchedule(repositories.ScopeAll(), g.ID, name, cron, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.env.Groups.DefineRule(repositories.ScopeAll(), g.ID, "Updates "+name,
		domain.RuleTypeSecurity, "", &sc.ID, false, "admin"); err != nil {
		t.Fatal(err)
	}
	return sc
}

// finishJob lässt einen Job auf dem Server laufen und beenden.
func (f *dashboardFixture) finishJob(t *testing.T, serverID uint, name string) {
	t.Helper()
	job, err := f.env.Jobs.Start(&serverID, nil, domain.RuleTypeSecurity, name, "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.env.Jobs.Complete(job, "ok", nil, nil)
}

func (f *dashboardFixture) alert(t *testing.T, serverID *uint, name string) {
	t.Helper()
	ev := &domain.AlertEvent{
		CreatedAt: time.Now(), RuleID: 1, ServerID: serverID, ServerName: name,
		Type: domain.AlertTypeHeartbeat, Severity: domain.AlertSeverityWarning, Code: "heartbeat",
	}
	if err := f.alertRepo.CreateEvent(ev); err != nil {
		t.Fatal(err)
	}
}

// jobServers liefert die Servernamen der Jobs im Dashboard (ohne Doppelte).
// Das Onboarding legt je Server einen eigenen Job an; er zählt mit.
func jobServers(d *services.Dashboard) []string {
	var names []string
	for _, j := range d.RecentJobs {
		if !slices.Contains(names, j.ServerName) {
			names = append(names, j.ServerName)
		}
	}
	return names
}

func TestDashboardMandantentrennung(t *testing.T) {
	f := newDashboardFixture(t)
	f.finishJob(t, f.alpha, "Update alpha")
	f.finishJob(t, f.beta, "Update beta")
	f.alert(t, &f.alpha, "alpha")
	f.alert(t, &f.beta, "beta")
	f.alert(t, nil, "LCM") // Selbstbeobachtung: nur für Admins

	d1, err := f.dash.Get(f.scope1, true)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Tiles.Total != 1 {
		t.Errorf("Manager 1: Server gesamt = %d, erwartet 1", d1.Tiles.Total)
	}
	for _, a := range d1.Attention {
		if a.Name != "alpha" {
			t.Errorf("Manager 1 sieht fremden Server in „Aufmerksamkeit“: %s", a.Name)
		}
	}
	if got := jobServers(d1); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("Manager 1: Jobs = %v, erwartet [alpha]", got)
	}
	if len(d1.Upcoming) != 1 || d1.Upcoming[0].GroupID != f.groupA.ID {
		t.Errorf("Manager 1: Zeitpläne = %+v", d1.Upcoming)
	}
	if len(d1.RecentAlerts) != 1 || d1.RecentAlerts[0].ServerName != "alpha" {
		t.Errorf("Manager 1: Alarme = %+v", d1.RecentAlerts)
	}

	d2, err := f.dash.Get(f.scope2, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := jobServers(d2); len(got) != 1 || got[0] != "beta" {
		t.Errorf("Manager 2: Jobs = %v, erwartet [beta]", got)
	}
	if len(d2.Upcoming) != 1 || d2.Upcoming[0].GroupID != f.groupB.ID {
		t.Errorf("Manager 2: Zeitpläne = %+v", d2.Upcoming)
	}

	admin, err := f.dash.Get(repositories.ScopeAll(), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := jobServers(admin); len(got) != 2 || admin.Tiles.Total != 2 || len(admin.RecentAlerts) != 3 {
		t.Errorf("Admin: Server %d, Job-Server %v, Alarme %d - erwartet 2/2/3",
			admin.Tiles.Total, got, len(admin.RecentAlerts))
	}
	// Die System-Gruppe hat eigene Zeitpläne; sie gehören nicht ins Dashboard.
	for _, run := range admin.Upcoming {
		if run.GroupID != f.groupA.ID && run.GroupID != f.groupB.ID {
			t.Errorf("Admin: fremder Zeitplan %+v", run)
		}
	}
}

func TestDashboardAlarmeNurMitRecht(t *testing.T) {
	f := newDashboardFixture(t)
	f.alert(t, &f.alpha, "alpha")

	d, err := f.dash.Get(f.scope1, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.RecentAlerts == nil || len(d.RecentAlerts) != 0 {
		t.Errorf("ohne Recht erwartet leere Liste, bekam %+v", d.RecentAlerts)
	}
}

func TestDashboardKachelnUndAufmerksamkeit(t *testing.T) {
	f := newDashboardFixture(t)
	d, err := f.dash.Get(repositories.ScopeAll(), false)
	if err != nil {
		t.Fatal(err)
	}
	sum := d.Tiles.Excellent + d.Tiles.Green + d.Tiles.Yellow + d.Tiles.Red
	if sum != d.Tiles.Total || d.Tiles.Total != 2 {
		t.Errorf("Kacheln %+v: Summe der Stufen muss der Gesamtzahl 2 entsprechen", d.Tiles)
	}
	if want := d.Tiles.Yellow + d.Tiles.Red; len(d.Attention) != want {
		t.Errorf("Aufmerksamkeit = %d Einträge, erwartet %d (rot + gelb)", len(d.Attention), want)
	}
	seenYellow := false
	for _, a := range d.Attention {
		if a.Level == domain.ServerStatusYellow {
			seenYellow = true
		}
		if a.Level == domain.ServerStatusRed && seenYellow {
			t.Error("rote Server müssen vor gelben stehen")
		}
	}
}

func TestDashboardZwischenspeicherUndEntwertung(t *testing.T) {
	f := newDashboardFixture(t)
	first, err := f.dash.Get(f.scope1, false)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := f.dash.Get(f.scope1, false)
	if first != again {
		t.Error("zweiter Aufruf hätte aus dem Zwischenspeicher kommen müssen")
	}

	f.bus.Publish(events.JobFinished("j1", &f.alpha, domain.JobStatusSuccess))
	third, _ := f.dash.Get(f.scope1, false)
	if third == first {
		t.Error("job.finished hätte den Zwischenspeicher entwerten müssen")
	}

	// Ein Ereignis, das das Dashboard nicht ändert, entwertet nichts.
	f.bus.Publish(events.JobProgress("j1", &f.alpha))
	if fourth, _ := f.dash.Get(f.scope1, false); fourth != third {
		t.Error("job.progress darf den Zwischenspeicher nicht entwerten")
	}
}
