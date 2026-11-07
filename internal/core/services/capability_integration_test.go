package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// capabilityEnv verdrahtet die Betriebsfähigkeit wie cmd/app/main.go.
func capabilityEnv(t *testing.T, env *testEnv) *repositories.CapabilityRepository {
	t.Helper()
	repo := repositories.NewCapabilityRepository(env.DB())
	svc := services.NewCapabilityService(repo)
	env.Jobs.OnOutcome(svc.RecordJob)
	env.Executor.WithCapabilities(svc)
	return repo
}

func latestJob(t *testing.T, env *testEnv, serverID uint, jobType string) domain.Job {
	t.Helper()
	var job domain.Job
	if err := env.DB().Where("server_id = ? AND type = ?", serverID, jobType).
		Order("rowid desc").First(&job).Error; err != nil {
		t.Fatalf("kein %s-Job: %v", jobType, err)
	}
	return job
}

func requireCapability(t *testing.T, repo *repositories.CapabilityRepository, serverID uint, category string) *domain.ServerCapability {
	t.Helper()
	// Der Job steht schon als beendet in der Datenbank, wenn OnOutcome die
	// Kategorie schreibt: Wer direkt danach liest, kann dazwischenkommen.
	var c *domain.ServerCapability
	waitFor(t, func() bool {
		found, err := repo.Find(serverID, category)
		c = found
		return err == nil && c != nil
	})
	return c
}

// updateRule ist die Update-Regel, die ein Zeitplan auslösen würde (der
// Executor-Weg runAptRule, nicht der Ad-hoc-Weg des ServerService).
func updateRule(t *testing.T, env *testEnv) *domain.Rule {
	t.Helper()
	rule := *findSystemSyncRule(t, env)
	rule.Name, rule.Type = "Updates", domain.RuleTypeUpdate
	return &rule
}

const lockBusyScriptOutput = "LCM: apt ist seit 10 Minuten belegt (PID 1234 (unattended-upgr), laeuft seit 10:00: x) - Lauf abgebrochen\n"

// TestAptRuleBehaeltExitCode75: LCMs eigener Code „apt ist belegt" ging
// verloren, weil der Fehlschlag ohne Exit-Code abgeschlossen wurde.
func TestAptRuleBehaeltExitCode75(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["lcm_apt_wait"] = sshx.FakeResponse{Output: lockBusyScriptOutput, ExitCode: 75}

	env.Executor.RunRule(updateRule(t, env), "admin")

	job := latestJob(t, env, id, domain.RuleTypeUpdate)
	if job.Status != domain.JobStatusFailed || job.ExitCode == nil || *job.ExitCode != 75 {
		t.Fatalf("Job: Status %q, Exit-Code %v", job.Status, job.ExitCode)
	}
	c := requireCapability(t, repo, id, domain.CapabilityPackages)
	if c.State != domain.CapabilityFailed || c.LastErrorClass != domain.ErrClassLockBusy ||
		c.LastErrorParams["process"] != "unattended-upgr" {
		t.Errorf("Betriebsfähigkeit: %+v", c)
	}
}

const partiallyBrokenAptUpdate = `Hit:1 http://deb.debian.org/debian bookworm InRelease
Err:2 https://ppa.example.org/foo/debian bookworm InRelease
  404  Not Found [IP: 1.2.3.4 443]
Reading package lists...
E: The repository 'https://ppa.example.org/foo/debian bookworm InRelease' is not signed.
`

// TestPaketlisteMitEinerKaputtenQuelleGiltAlsErfolg deckt beide Laufwege ab:
// den Ad-hoc-Weg („Paketliste aktualisieren") und den Zeitplan.
func TestPaketlisteMitEinerKaputtenQuelleGiltAlsErfolg(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, env *testEnv, id uint)
	}{
		{"ad hoc", func(t *testing.T, env *testEnv, id uint) {
			if _, err := env.Servers.RefreshPackages(repositories.ScopeAll(), id, "admin"); err != nil {
				t.Fatal(err)
			}
		}},
		{"zeitplan", func(t *testing.T, env *testEnv, _ uint) {
			rule := *findSystemSyncRule(t, env)
			rule.Name, rule.Type = "Paketliste", domain.RuleTypePackageScan
			env.Executor.RunRule(&rule, "admin")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			repo := capabilityEnv(t, env)
			id := joinTestServer(t, env, "web01")
			env.Dialer.Responses["lcm_apt_update"] = sshx.FakeResponse{Output: partiallyBrokenAptUpdate, ExitCode: 100}

			tc.run(t, env, id)

			job := waitServerJob(t, env, id, domain.RuleTypePackageScan)
			if job.Status != domain.JobStatusSuccess || job.ExitCode == nil || *job.ExitCode != 0 {
				t.Fatalf("Job: Status %q, Exit-Code %v, Output %q", job.Status, job.ExitCode, job.Output)
			}
			// Der Bestandsabgleich lief trotz Exit 100.
			if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "dpkg-query") {
				t.Error("Paket-Rescan fehlt")
			}
			c := requireCapability(t, repo, id, domain.CapabilityPackages)
			if c.State != domain.CapabilityOK || c.WarningClass != domain.ErrClassRepoError ||
				!strings.Contains(c.WarningMessage, "ppa.example.org") {
				t.Errorf("Betriebsfähigkeit: %+v", c)
			}
		})
	}
}

const allSourcesBrokenAptUpdate = `Err:1 https://ppa.example.org/foo/debian bookworm InRelease
  404  Not Found [IP: 1.2.3.4 443]
E: The repository 'https://ppa.example.org/foo/debian bookworm InRelease' is not signed.
`

func TestPaketlisteMitAllenQuellenKaputtIstFehlschlag(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["lcm_apt_update"] = sshx.FakeResponse{Output: allSourcesBrokenAptUpdate, ExitCode: 100}

	if _, err := env.Servers.RefreshPackages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatal(err)
	}

	job := waitServerJob(t, env, id, domain.RuleTypePackageScan)
	if job.Status != domain.JobStatusFailed || job.ExitCode == nil || *job.ExitCode != 100 {
		t.Fatalf("Job: Status %q, Exit-Code %v", job.Status, job.ExitCode)
	}
	c := requireCapability(t, repo, id, domain.CapabilityPackages)
	if c.State != domain.CapabilityFailed || c.LastErrorClass != domain.ErrClassRepoError {
		t.Errorf("Betriebsfähigkeit: %+v", c)
	}
}

// allSourcesUnresolvableAptUpdate: apt-get update endet bei DNS-Ausfall mit
// Exit 0 - es meldet die Fehler nur als Err:- und W:-Zeilen.
const allSourcesUnresolvableAptUpdate = `Err:1 http://deb.debian.org/debian bookworm InRelease
  Temporary failure resolving 'deb.debian.org'
Err:2 http://security.debian.org/debian-security bookworm-security InRelease
  Temporary failure resolving 'security.debian.org'
Reading package lists...
W: Failed to fetch http://deb.debian.org/debian/dists/bookworm/InRelease  Temporary failure resolving 'deb.debian.org'
W: Some index files failed to download. They have been ignored, or old ones used instead.
`

// TestPaketlisteMitExit0UndAllenQuellenKaputtIstFehlschlag: Ohne Prüfung der
// Ausgabe bliebe packages grün, obwohl der Server keine Listen bekommt.
func TestPaketlisteMitExit0UndAllenQuellenKaputtIstFehlschlag(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["lcm_apt_update"] = sshx.FakeResponse{Output: allSourcesUnresolvableAptUpdate, ExitCode: 0}

	if _, err := env.Servers.RefreshPackages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatal(err)
	}

	job := waitServerJob(t, env, id, domain.RuleTypePackageScan)
	if job.Status != domain.JobStatusFailed {
		t.Fatalf("Job: Status %q, Output %q", job.Status, job.Output)
	}
	c := requireCapability(t, repo, id, domain.CapabilityPackages)
	if c.State != domain.CapabilityFailed || c.LastErrorClass != domain.ErrClassNetwork {
		t.Errorf("Betriebsfähigkeit: %+v", c)
	}
}

func TestFlottenScanOhneScannerUndWiederMit(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")

	env.Scanner.IsAvailable = false
	env.Executor.RunCVEScan("scheduler")
	c := requireCapability(t, repo, id, domain.CapabilityCVEScan)
	if c.State != domain.CapabilityFailed || c.LastErrorClass != domain.ErrClassScannerUnavailable {
		t.Fatalf("ohne Scanner: %+v", c)
	}

	env.Scanner.IsAvailable = true
	env.Executor.RunCVEScan("scheduler")
	c = requireCapability(t, repo, id, domain.CapabilityCVEScan)
	if c.State != domain.CapabilityOK || c.LastSuccessAt == nil {
		t.Fatalf("mit Scanner: %+v", c)
	}
}

func TestEinzelScanOhneScannerIstFehlgeschlagenerJob(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")
	env.Scanner.IsAvailable = false

	env.Executor.RunCVEScanServer(id, "admin")

	job := latestJob(t, env, id, domain.RuleTypeCVEScan)
	if job.Status != domain.JobStatusFailed || !strings.Contains(job.Output, "Trivy nicht verfügbar") {
		t.Fatalf("Job: Status %q, Output %q", job.Status, job.Output)
	}
	if c := requireCapability(t, repo, id, domain.CapabilityCVEScan); c.LastErrorClass != domain.ErrClassScannerUnavailable {
		t.Errorf("Betriebsfähigkeit: %+v", c)
	}
}

// TestUpdateFehlerBleibtNachHealthPingStehen: Ende-zu-Ende über den echten
// Job-Hook - ein Health-Ping nach einem fehlgeschlagenen Update lässt
// packages rot.
func TestUpdateFehlerBleibtNachHealthPingStehen(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["lcm_apt_wait"] = sshx.FakeResponse{Output: lockBusyScriptOutput, ExitCode: 75}
	env.Executor.RunRule(updateRule(t, env), "admin")

	health := *findSystemSyncRule(t, env)
	health.Name, health.Type = "Health", domain.RuleTypeHealth
	env.Executor.RunRule(&health, "admin") // der Scheduler ließe den Ping nach frischem Kontakt aus

	if c := requireCapability(t, repo, id, domain.CapabilityPackages); c.State != domain.CapabilityFailed {
		t.Errorf("Health-Ping hat packages geheilt: %+v", c)
	}
	if c := requireCapability(t, repo, id, domain.CapabilityConnection); c.State != domain.CapabilityOK {
		t.Errorf("connection: %+v", c)
	}
}

// TestUebersprungenerJobHeiltDieVerbindungNicht: Im eingeschränkten Modus endet
// ein Skript-Lauf als grüner Job, ohne dass LCM den Server je kontaktiert. Er
// beweist nichts über die Verbindung.
func TestUebersprungenerJobHeiltDieVerbindungNicht(t *testing.T) {
	env := newTestEnv(t)
	repo := capabilityEnv(t, env)
	id := joinTestServer(t, env, "web01")
	health := *findSystemSyncRule(t, env)
	health.Name, health.Type = "Health", domain.RuleTypeHealth

	env.Dialer.FailKey = errors.New("dial tcp 10.0.0.1:22: i/o timeout")
	env.Executor.RunRule(&health, "admin")
	if c := requireCapability(t, repo, id, domain.CapabilityConnection); c.State != domain.CapabilityFailed {
		t.Fatalf("Verbindung nach Fehlschlag: %+v", c)
	}
	env.Dialer.FailKey = nil

	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true).Error; err != nil {
		t.Fatal(err)
	}
	script := health
	script.Name, script.Type, script.Command = "Skript", domain.RuleTypeScript, "echo hi"
	env.Executor.RunRule(&script, "admin")

	job := latestJob(t, env, id, domain.RuleTypeScript)
	if job.Status != domain.JobStatusSuccess || !strings.Contains(job.Output, "übersprungen") {
		t.Fatalf("Job: Status %q, Output %q", job.Status, job.Output)
	}
	if c := requireCapability(t, repo, id, domain.CapabilityConnection); c.State != domain.CapabilityFailed {
		t.Errorf("der übersprungene Job hat die Verbindung geheilt: %+v", c)
	}

	// Ein echter Erfolg heilt sie.
	env.Executor.RunRule(&health, "admin")
	if c := requireCapability(t, repo, id, domain.CapabilityConnection); c.State != domain.CapabilityOK {
		t.Errorf("Health-Ping hat die Verbindung nicht geheilt: %+v", c)
	}
}

func TestNichtsOffenBerücksichtigtKandidatenUndGescheiterteUpdatePruefung(t *testing.T) {
	env := newTestEnv(t)
	servers := repositories.NewServerRepository(env.DB())
	s := &domain.Server{Name: "noble", Host: "10.1.1.1", Reachable: true}
	if err := servers.Create(s); err != nil {
		t.Fatal(err)
	}

	erwarte := func(want bool) {
		t.Helper()
		got, err := servers.NothingPending(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("NothingPending = %v, erwartet %v", got, want)
		}
	}

	erwarte(true)
	if err := servers.ReplacePackages(s.ID, []domain.Package{{Name: "curl", Version: "1", CandidateVersion: "2"}}); err != nil {
		t.Fatal(err)
	}
	erwarte(false)
	if err := servers.ReplacePackages(s.ID, []domain.Package{{Name: "curl", Version: "2"}}); err != nil {
		t.Fatal(err)
	}
	erwarte(true)
	if err := servers.UpdateFields(s.ID, map[string]any{"update_check_error": "Quelle kaputt"}); err != nil {
		t.Fatal(err)
	}
	erwarte(false)
}
