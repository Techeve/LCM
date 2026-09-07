package services_test

import (
	"context"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// setzeWartung schaltet den Wartungs-Zustand eines Servers.
func setzeWartung(t *testing.T, env *testEnv, id uint, an bool) *domain.Server {
	t.Helper()
	srv, err := env.Servers.UpdateSettings(repositories.ScopeAll(), id,
		services.ServerSettingsInput{Maintenance: &an}, "admin")
	if err != nil {
		t.Fatalf("Wartung schalten: %v", err)
	}
	return srv
}

// TestWartungHaeltZeitplaeneFern: Der Kern des Zustands. Ein Server, der
// absichtlich aus ist, darf keine Zeitplan-Läufe mehr bekommen - sonst
// erzeugt er im Wochentakt tausende Unerreichbar-Warnungen und macht das
// Protokoll unlesbar, in dem die echten Störungen stehen.
func TestWartungHaeltZeitplaeneFern(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "testumgebung")
	healthRule := findSystemHealthRule(t, env)

	// Gegenprobe zuerst: ohne Wartung läuft der Health-Check. Der Kontakt
	// muss dafür veraltet sein - ein frischer ließe den Ping ohnehin
	// entfallen (siehe skipHealthPing).
	kontaktVeralten(t, env, id)
	env.Executor.RunRule(healthRule, "scheduler")
	vorher := jobsFuerServer(t, env, id)
	if vorher == 0 {
		t.Fatal("ohne Wartung muss der Health-Check laufen")
	}

	setzeWartung(t, env, id, true)
	kontaktVeralten(t, env, id)
	env.Executor.RunRule(healthRule, "scheduler")

	if nachher := jobsFuerServer(t, env, id); nachher != vorher {
		t.Errorf("in Wartung liefen %d zusätzliche Jobs, erwartet 0", nachher-vorher)
	}

	// Und zurück: Nach dem Ende der Wartung läuft wieder alles.
	setzeWartung(t, env, id, false)
	kontaktVeralten(t, env, id)
	env.Executor.RunRule(healthRule, "scheduler")
	if nachher := jobsFuerServer(t, env, id); nachher <= vorher {
		t.Error("nach dem Ende der Wartung muss der Health-Check wieder laufen")
	}
}

// TestWartungZeitstempelKommtUndGeht: Ein stehengebliebenes Datum wäre
// schlimmer als keines - nach ein paar Wochen wäre nicht mehr zu erkennen,
// ob eine Wartung noch läuft oder jemand sie vergessen hat.
func TestWartungZeitstempelKommtUndGeht(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "testumgebung")

	if srv := setzeWartung(t, env, id, true); srv.MaintenanceSince == nil {
		t.Error("beim Einschalten fehlt der Zeitstempel")
	}
	if srv := setzeWartung(t, env, id, false); srv.MaintenanceSince != nil {
		t.Errorf("beim Ausschalten bleibt der Zeitstempel stehen: %v", srv.MaintenanceSince)
	}
}

// TestWartungNimmtServerAusDerFruehwarnung: Die Frühwarnung bewertet den
// Paketbestand - bei einem abgeschalteten System ist der eingefroren und
// seine Befunde wären eine Aussage über den Stand von damals.
func TestWartungNimmtServerAusDerFruehwarnung(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 30)
	id := seedPackages(t, env, "testumgebung", domain.Package{Name: "openssl", Version: "3.0.11"})

	setzeWartung(t, env, id, true)

	summary, err := env.Advisories.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(env.AdvSource.QueriedPurls) != 0 {
		t.Errorf("Server in Wartung wurde abgefragt: %v (%s)", env.AdvSource.QueriedPurls, summary)
	}
}

// kontaktVeralten datiert den letzten Kontakt zurück, damit ein geplanter
// Health-Ping tatsächlich läuft statt ausgelassen zu werden.
func kontaktVeralten(t *testing.T, env *testEnv, serverID uint) {
	t.Helper()
	alt := time.Now().Add(-2 * time.Hour)
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", serverID).
		Update("last_seen_at", alt).Error; err != nil {
		t.Fatalf("Kontaktzeit zurückdatieren: %v", err)
	}
}

// jobsFuerServer zählt die Jobs eines Servers.
func jobsFuerServer(t *testing.T, env *testEnv, serverID uint) int64 {
	t.Helper()
	var n int64
	if err := env.DB().Model(&domain.Job{}).Where("server_id = ?", serverID).Count(&n).Error; err != nil {
		t.Fatalf("Jobs zählen: %v", err)
	}
	return n
}
