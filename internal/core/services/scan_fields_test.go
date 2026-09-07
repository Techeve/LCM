package services_test

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// findSystemSyncRule liefert die System-Sync-Regel der geschützten Gruppe.
func findSystemSyncRule(t *testing.T, env *testEnv) *domain.Rule {
	t.Helper()
	groups, err := env.Groups.List(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if !g.IsSystem {
			continue
		}
		rules, err := env.Groups.ListRules(repositories.ScopeAll(), g.ID)
		if err != nil {
			t.Fatal(err)
		}
		for i := range rules {
			if rules[i].Type == domain.RuleTypeSync {
				return &rules[i]
			}
		}
	}
	t.Fatal("keine System-Sync-Regel gefunden")
	return nil
}

// vollstaendigeErkennung ergänzt die Fake-Antworten um das, was der Sync
// bisher wegwarf. NACH dem Join zu setzen ist Absicht: joinTestServer ersetzt
// die Antwort-Tabelle vollständig.
func vollstaendigeErkennung(env *testEnv) {
	env.Dialer.Responses["cat /etc/os-release"] = sshx.FakeResponse{Output: "" +
		"NAME=\"Debian GNU/Linux\"\nVERSION=\"12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\n"}
	env.Dialer.Responses["systemd-detect-virt"] = sshx.FakeResponse{Output: "lxc\n"}
}

// TestSyncSpeichertDieErkannteIdentitaet ist der Regressionstest gegen die
// gefundene Fehlerklasse: Der nächtliche System-Sync ERFASSTE vierzig Werte
// und schrieb sechzehn davon weg - neunzehn fielen jede Nacht unter den Tisch,
// weil er eine eigene, von Hand gepflegte Feldliste führte.
//
// Am folgenreichsten waren os_id und os_version_id: Aus ihnen rechnet die
// Support-/EOL-Bewertung. Nach einem Distributions-Upgrade zeigte die
// Oberfläche den neuen Namen und bewertete weiter die alte Version - ein
// Server blieb also rot wegen eines End-of-Life, das das Upgrade gerade
// behoben hatte.
func TestSyncSpeichertDieErkannteIdentitaet(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)

	// Die Felder leeren, damit der Test nicht den Stand vom Join sieht - dort
	// wird der volle Datensatz geschrieben, der Fehler lag allein im Sync.
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Updates(map[string]any{
		"os_id": "", "os_version_id": "", "package_manager": "", "virtualization": "",
	}).Error; err != nil {
		t.Fatal(err)
	}

	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	nachher, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ name, wert string }{
		{"os_id", nachher.OSID},
		{"os_version_id", nachher.OSVersionID},
		{"package_manager", nachher.PackageManager},
		{"virtualization", nachher.Virtualization},
	} {
		if f.wert == "" {
			t.Errorf("%s wurde vom System-Sync nicht gespeichert", f.name)
		}
	}
}

// TestScanUeberschreibtErkanntesNichtMitLeere: Ein Durchgang, dem ein Kommando
// weggebrochen ist, darf einen einmal erkannten Wert nicht löschen.
//
// Ohne diese Regel verlöre ein Server bei einem einzigen gestörten Scan seine
// Distributions-Kennung - und mit ihr die EOL-Bewertung, die daraus rechnet.
func TestScanUeberschreibtErkanntesNichtMitLeere(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)

	// Erst ein sauberer Durchgang, damit etwas da ist, das verloren gehen kann.
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")
	vorher, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if vorher.OSID == "" || vorher.PackageManager == "" {
		t.Fatal("Vorbedingung: der erste Durchgang muss die Kennung erfasst haben")
	}

	// Jetzt bricht die Erkennung weg - os-release und die Paketverwaltung
	// sind nicht mehr lesbar.
	env.Dialer.Responses["cat /etc/os-release"] = sshx.FakeResponse{ExitCode: 1}
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{ExitCode: 1}
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	nachher, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if nachher.OSID != vorher.OSID {
		t.Errorf("ein gestörter Scan hat die Kennung überschrieben: %q → %q", vorher.OSID, nachher.OSID)
	}
	if nachher.PackageManager != vorher.PackageManager {
		t.Errorf("ein gestörter Scan hat die Paketverwaltung überschrieben: %q → %q",
			vorher.PackageManager, nachher.PackageManager)
	}
}

// TestBeideScanWegeSchreibenDasselbe hält die Zusammenführung fest: System-Sync
// und Voll-Refresh dürfen nicht wieder auseinanderlaufen. Genau das war der
// Fehler - zwei von Hand gepflegte Listen, von denen eine zurückblieb.
func TestBeideScanWegeSchreibenDasselbe(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)

	// Einmal über den Sync …
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")
	nachSync, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}

	// … und einmal über den vollen Refresh.
	job, err := env.Servers.RefreshAll(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatal(err)
	}
	wartenAufJob(t, env, job.ID)
	nachRefresh, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}

	vergleiche := []struct {
		name       string
		sync, full string
	}{
		{"os_id", nachSync.OSID, nachRefresh.OSID},
		{"os_version_id", nachSync.OSVersionID, nachRefresh.OSVersionID},
		{"package_manager", nachSync.PackageManager, nachRefresh.PackageManager},
		{"virtualization", nachSync.Virtualization, nachRefresh.Virtualization},
		{"firewall_tool", nachSync.FirewallTool, nachRefresh.FirewallTool},
		{"timezone", nachSync.Timezone, nachRefresh.Timezone},
	}
	if nachSync.OSID == "" || nachSync.PackageManager == "" {
		t.Fatal("der Sync hat gar nichts erfasst - der Vergleich wäre wertlos")
	}
	for _, v := range vergleiche {
		if v.sync != v.full {
			t.Errorf("%s: Sync schreibt %q, Voll-Refresh %q - die Wege sind auseinandergelaufen",
				v.name, v.sync, v.full)
		}
	}
}

// wartenAufJob wartet, bis ein Job einen Endzustand erreicht hat.
func wartenAufJob(t *testing.T, env *testEnv, jobID string) {
	t.Helper()
	for i := 0; i < 400; i++ {
		job, err := env.Jobs.Status(jobID)
		if err == nil && job != nil {
			switch job.Status {
			case domain.JobStatusSuccess, domain.JobStatusFailed, domain.JobStatusAborted:
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Job wurde nicht fertig")
}
