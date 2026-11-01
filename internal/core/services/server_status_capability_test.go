package services_test

import (
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// capabilityStatusEnv verdrahtet die Betriebsfähigkeit in den Server-Service.
func capabilityStatusEnv(t *testing.T) *testEnv {
	t.Helper()
	env := newTestEnv(t)
	env.Servers.WithCapabilities(repositories.NewCapabilityRepository(env.DB()))
	return env
}

// capabilityServer legt einen erreichbaren Server mit Paketbestand an,
// angelegt vor `age`.
func capabilityServer(t *testing.T, env *testEnv, name string, age time.Duration) uint {
	t.Helper()
	repo := repositories.NewServerRepository(env.db)
	s := &domain.Server{Name: name, Host: "10.8.8." + name, Reachable: true, CreatedAt: time.Now().Add(-age)}
	if err := repo.Create(s); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplacePackages(s.ID, []domain.Package{{Name: "bash", Version: "5.2"}}); err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func saveCapability(t *testing.T, env *testEnv, c domain.ServerCapability) {
	t.Helper()
	if err := repositories.NewCapabilityRepository(env.DB()).Save(&c); err != nil {
		t.Fatal(err)
	}
}

func failedCap(serverID uint, category, class string, failures int) domain.ServerCapability {
	since := time.Now().Add(-26 * time.Hour)
	return domain.ServerCapability{
		ServerID: serverID, Category: category, State: domain.CapabilityFailed,
		ConsecutiveFailures: failures, FailingSince: &since, LastErrorClass: class, LastJobName: "Updates",
	}
}

func okPackagesSince(serverID uint, last time.Time) domain.ServerCapability {
	return domain.ServerCapability{
		ServerID: serverID, Category: domain.CapabilityPackages, State: domain.CapabilityOK, LastSuccessAt: &last,
	}
}

func fullStatusOf(t *testing.T, env *testEnv, id uint) (st struct {
	Status    string
	Incapable bool
	Keys      map[string]string
}) {
	t.Helper()
	all, err := env.Servers.StatusAll(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.ID != id {
			continue
		}
		st.Status, st.Incapable, st.Keys = s.Status, s.Incapable, map[string]string{}
		for _, in := range s.Insights {
			st.Keys[in.Key] = in.Severity
		}
		return st
	}
	t.Fatalf("server %d fehlt in StatusAll", id)
	return st
}

func TestStatusAktionsfehlerUndHandlungsunfaehigkeit(t *testing.T) {
	week := 24 * time.Hour
	tests := []struct {
		name       string
		caps       func(id uint) []domain.ServerCapability
		age        time.Duration
		wantStatus string
		wantKey    string
		wantSev    string
		incapable  bool
	}{
		{"Sperre einmal: gelb, aber handlungsunfähig", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{failedCap(id, domain.CapabilityPackages, domain.ErrClassLockBusy, 1)}
		}, week, "yellow", "actionFailed.packages", "warning", true},
		{"Sperre zweimal: rot", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{failedCap(id, domain.CapabilityPackages, domain.ErrClassLockBusy, 2)}
		}, week, "red", "actionFailed.packages", "critical", true},
		{"Verbindung: rot und handlungsunfähig", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{failedCap(id, domain.CapabilityConnection, domain.ErrClassNetwork, 1)}
		}, week, "red", "actionFailed.connection", "critical", true},
		{"Regel: gelb, Server bleibt pflegbar", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{failedCap(id, domain.CapabilityRules, domain.ErrClassUnknown, 4)}
		}, week, "yellow", "actionFailed.rules", "warning", false},
		{"Paketstand 3 Tage alt: gelb", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{okPackagesSince(id, time.Now().Add(-3*24*time.Hour))}
		}, 30 * week, "yellow", "packagesStale", "warning", false},
		{"Paketstand 8 Tage alt: rot, handlungsunfähig", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{okPackagesSince(id, time.Now().Add(-8*24*time.Hour))}
		}, 30 * week, "red", "packagesStaleCritical", "critical", true},
		{"nie erfolgreich, Server 10 Tage alt: Anlagedatum zählt", func(id uint) []domain.ServerCapability {
			return nil
		}, 10 * week, "red", "packagesStaleCritical", "critical", true},
		{"frischer Erfolg: nichts", func(id uint) []domain.ServerCapability {
			return []domain.ServerCapability{okPackagesSince(id, time.Now().Add(-time.Hour))}
		}, 30 * week, "", "", "", false},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := capabilityStatusEnv(t)
			id := capabilityServer(t, env, string(rune('1'+i)), tc.age)
			for _, c := range tc.caps(id) {
				saveCapability(t, env, c)
			}
			st := fullStatusOf(t, env, id)
			if tc.wantKey != "" {
				if got := st.Keys[tc.wantKey]; got != tc.wantSev {
					t.Errorf("Befund %s: Stufe %q, erwartet %q (alle: %v)", tc.wantKey, got, tc.wantSev, st.Keys)
				}
			}
			if tc.wantStatus != "" && st.Status != tc.wantStatus {
				t.Errorf("Ampel %q, erwartet %q", st.Status, tc.wantStatus)
			}
			if tc.wantKey == "" && (st.Status == "red" || st.Keys["packagesStale"] != "") {
				t.Errorf("unerwartet auffällig: %q %v", st.Status, st.Keys)
			}
			if st.Incapable != tc.incapable {
				t.Errorf("Incapable = %v, erwartet %v", st.Incapable, tc.incapable)
			}
		})
	}
}

// Demo-Server haben erfundene Daten, Geräte wie RouterOS keine Pakete - beide
// dürfen nicht wegen eines "Stillstands" rot werden.
func TestStillstandIgnoriertDemoUndApiGeraete(t *testing.T) {
	env := capabilityStatusEnv(t)
	repo := repositories.NewServerRepository(env.db)
	old := time.Now().Add(-90 * 24 * time.Hour)
	demo := &domain.Server{Name: "demo", Host: "10.7.7.1", Reachable: true, IsDemo: true, CreatedAt: old}
	router := &domain.Server{Name: "router", Host: "10.7.7.2", Reachable: true, OSID: domain.OSIDRouterOS, CreatedAt: old}
	for _, s := range []*domain.Server{demo, router} {
		if err := repo.Create(s); err != nil {
			t.Fatal(err)
		}
		if err := repo.ReplacePackages(s.ID, []domain.Package{{Name: "x", Version: "1"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uint{demo.ID, router.ID} {
		st := fullStatusOf(t, env, id)
		if st.Keys["packagesStale"] != "" || st.Keys["packagesStaleCritical"] != "" || st.Incapable {
			t.Errorf("Server %d: Stillstand gemeldet: %v incapable=%v", id, st.Keys, st.Incapable)
		}
	}
}

// StatusAll fragt die Betriebsfähigkeit aller Server mit EINER Abfrage ab und
// ordnet sie den richtigen Servern zu. Aus dem Cache beantwortete Server
// kosten keine Abfrage.
func TestStatusAllLaedtFaehigkeitenMitEinerAbfrage(t *testing.T) {
	env := capabilityStatusEnv(t)
	broken := capabilityServer(t, env, "1", 24*time.Hour)
	fine := capabilityServer(t, env, "2", 24*time.Hour)
	other := capabilityServer(t, env, "3", 24*time.Hour)
	saveCapability(t, env, failedCap(broken, domain.CapabilityPackages, domain.ErrClassDpkgInterrupted, 1))

	var queries atomic.Int32
	err := env.DB().Callback().Query().Before("gorm:query").Register("count_capability_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "server_capabilities" {
			queries.Add(1)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := env.Servers.StatusAll(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	if got := queries.Load(); got != 1 {
		t.Errorf("Abfragen auf server_capabilities: %d, erwartet 1", got)
	}
	incapable := map[uint]bool{}
	for _, st := range all {
		incapable[st.ID] = st.Incapable
	}
	if !incapable[broken] || incapable[fine] || incapable[other] {
		t.Errorf("Zuordnung falsch: %v", incapable)
	}

	if _, err := env.Servers.StatusAll(repositories.ScopeAll()); err != nil {
		t.Fatal(err)
	}
	if got := queries.Load(); got != 1 {
		t.Errorf("zweiter Aufruf aus dem Cache kostete %d weitere Abfragen", got-1)
	}
}

func TestCapabilitiesLiefertAlleKategorienInReihenfolge(t *testing.T) {
	env := capabilityStatusEnv(t)
	id := capabilityServer(t, env, "1", 24*time.Hour)
	saveCapability(t, env, failedCap(id, domain.CapabilityRules, domain.ErrClassUnknown, 2))

	res, err := env.Servers.Capabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Server == nil || res.Server.ID != id {
		t.Errorf("Server nicht mitgeliefert: %+v", res.Server)
	}
	if len(res.Rows) != len(domain.CapabilityCategories) {
		t.Fatalf("%d Kategorien, erwartet %d", len(res.Rows), len(domain.CapabilityCategories))
	}
	for i, c := range res.Rows {
		if c.Category != domain.CapabilityCategories[i] || c.ServerID != id {
			t.Errorf("Position %d: %s/server %d", i, c.Category, c.ServerID)
		}
		wantState := domain.CapabilityOK
		if c.Category == domain.CapabilityRules {
			wantState = domain.CapabilityFailed
		}
		if c.State != wantState {
			t.Errorf("%s: Zustand %q, erwartet %q", c.Category, c.State, wantState)
		}
	}
	if res.Stale != "" {
		t.Errorf("Stillstand %q bei frischem Server", res.Stale)
	}
}

func TestCapabilitiesPrueftDenZugriff(t *testing.T) {
	env := capabilityStatusEnv(t)
	id := capabilityServer(t, env, "1", 24*time.Hour)
	// Ein Benutzer ohne Gruppe sieht den Server nicht.
	if _, err := env.Servers.Capabilities(repositories.ScopeManager(99999), id); err == nil {
		t.Fatal("Zugriff ohne Berechtigung wurde nicht abgewiesen")
	}
}

func TestCapabilitiesMeldetStillstand(t *testing.T) {
	env := capabilityStatusEnv(t)
	id := capabilityServer(t, env, "1", 30*24*time.Hour)
	saveCapability(t, env, okPackagesSince(id, time.Now().Add(-3*24*time.Hour)))
	res, err := env.Servers.Capabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stale != domain.PackagesStaleWarning {
		t.Errorf("Stillstand %q, erwartet %q", res.Stale, domain.PackagesStaleWarning)
	}
}
