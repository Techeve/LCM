package services_test

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestFrischerKontaktErspartDenPing: Ein erfolgreicher Job IST das
// Lebenszeichen des Servers. Wer gerade mit ihm gesprochen hat, muss ihn nicht
// unmittelbar danach anpingen, um zu erfahren, ob er erreichbar ist.
func TestFrischerKontaktErspartDenPing(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)

	// Der Join selbst hat schon Jobs erzeugt - gezählt wird, was DANACH
	// dazukommt.
	vorher := jobsFuerServer(t, env, id)

	// Der Join hat gerade Kontakt gehabt - der geplante Ping entfällt.
	env.Dialer.Commands = nil
	env.Executor.RunRule(health, "scheduler")

	if cmds := strings.Join(env.Dialer.Commands, "\n"); strings.Contains(cmds, "lcm-health-ok") {
		t.Errorf("bei frischem Kontakt darf kein Ping laufen:\n%s", cmds)
	}
	if n := jobsFuerServer(t, env, id); n != vorher {
		t.Errorf("ein ausgelassener Ping darf keine Job-Zeile schreiben, waren %d zusätzliche", n-vorher)
	}
}

// TestVeralteterKontaktLaesstDenPingLaufen: die Gegenprobe. Bleibt der Kontakt
// aus, wächst sein Alter über den Takt - dann läuft der Ping wieder. Die
// Auslassung kann sich also nicht verselbständigen.
func TestVeralteterKontaktLaesstDenPingLaufen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)

	kontaktVeralten(t, env, id)
	env.Dialer.Commands = nil
	env.Executor.RunRule(health, "scheduler")

	if cmds := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(cmds, "lcm-health-ok") {
		t.Errorf("bei veraltetem Kontakt muss der Ping laufen:\n%s", cmds)
	}
}

// TestManuellerPingLaeuftImmer: Wer den Health-Check von Hand anstößt, will
// einen frischen Kontakt - und bekommt ihn, egal wie jung der letzte ist.
func TestManuellerPingLaeuftImmer(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)

	env.Dialer.Commands = nil
	env.Executor.RunRule(health, "admin")

	if cmds := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(cmds, "lcm-health-ok") {
		t.Errorf("ein manuell ausgelöster Ping muss laufen:\n%s", cmds)
	}
}

// TestPingLandetNichtImSSHProtokoll: Der Ping erzeugte je Server und
// Viertelstunde eine Protokollzeile mit Kommando und Ausgabe - über die
// Aufbewahrungsfrist von 90 Tagen bei dreihundert Servern einige Millionen
// Zeilen für ein „echo". Die Sitzung bleibt: Auf derselben Verbindung laufen
// Grundsatz-Regeln und Speichermessung, und die gehören ins Protokoll.
func TestPingLandetNichtImSSHProtokoll(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)

	env.Executor.RunRule(health, "admin")

	sessions, err := env.SSHLogs.ServerSessions(repositories.ScopeAll(), id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var healthSession *domain.SSHSession
	for i := range sessions {
		if sessions[i].Purpose == "health-check" {
			healthSession = &sessions[i]
		}
	}
	if healthSession == nil {
		t.Fatal("die Health-Sitzung selbst muss protokolliert werden")
	}

	voll, err := env.SSHLogs.Session(repositories.ScopeAll(), healthSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range voll.Commands {
		if strings.Contains(c.Command, "lcm-health-ok") {
			t.Errorf("der Ping selbst darf nicht protokolliert werden: %q", c.Command)
		}
	}
}

// TestSkipFensterFolgtDemTakt: Das Fenster ist der Takt des Zeitplans, keine
// gegriffene Zahl - stellt der Betreiber den Health-Check auf fünf Minuten,
// schrumpft es mit. Eine feste Zahl hätte bei kürzerem Takt alle Pings
// verschluckt.
func TestSkipFensterFolgtDemTakt(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)

	// Takt auf eine Minute stellen und den Kontakt 90 Sekunden zurückdatieren:
	// älter als der neue Takt, jünger als der alte von 15 Minuten.
	setzeHealthTakt(t, env, health, "* * * * *")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Update("last_seen_at", time.Now().Add(-90*time.Second)).Error; err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(health, "scheduler")

	if cmds := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(cmds, "lcm-health-ok") {
		t.Errorf("bei Ein-Minuten-Takt ist ein 90 s alter Kontakt veraltet - der Ping muss laufen:\n%s", cmds)
	}
}

// setzeHealthTakt ändert den Cron-Ausdruck des Health-Zeitplans.
func setzeHealthTakt(t *testing.T, env *testEnv, rule *domain.Rule, cron string) {
	t.Helper()
	if rule.ScheduleID == nil {
		t.Fatal("Health-Regel ohne Zeitplan")
	}
	if err := env.DB().Model(&domain.Schedule{}).Where("id = ?", *rule.ScheduleID).
		Update("cron_expr", cron).Error; err != nil {
		t.Fatalf("Takt setzen: %v", err)
	}
}
