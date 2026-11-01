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

// retryEnv verdrahtet Betriebsfähigkeit und „Erneut versuchen" wie
// cmd/app/main.go.
func retryEnv(t *testing.T, env *testEnv) *repositories.CapabilityRepository {
	t.Helper()
	repo := capabilityEnv(t, env)
	env.Servers.WithCapabilities(repo).WithRetryRunner(env.Executor)
	return repo
}

// waitCapability wartet, bis die Kategorie den Zustand hat: Die Zeile wird beim
// Jobende geschrieben, kurz nachdem der Job selbst fertig ist.
func waitCapability(t *testing.T, repo *repositories.CapabilityRepository, serverID uint, category, state string) *domain.ServerCapability {
	t.Helper()
	var c *domain.ServerCapability
	waitFor(t, func() bool {
		var err error
		c, err = repo.Find(serverID, category)
		return err == nil && c != nil && c.State == state
	})
	return c
}

// failUpdate lässt ein Ad-hoc-Update an der Sperre scheitern und liefert die
// Kategorie packages im Zustand failed.
func failUpdate(t *testing.T, env *testEnv, repo *repositories.CapabilityRepository, serverID uint) {
	t.Helper()
	env.Dialer.Responses["lcm_apt_wait"] = sshx.FakeResponse{Output: lockBusyScriptOutput, ExitCode: 75}
	job, err := env.Servers.UpgradeAllPackages(repositories.ScopeAll(), serverID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, env, job.ID)
	waitCapability(t, repo, serverID, domain.CapabilityPackages, domain.CapabilityFailed)
	delete(env.Dialer.Responses, "lcm_apt_wait") // die Sperre ist wieder frei
}

func setPackageManager(t *testing.T, env *testEnv, serverID uint, mgr string) {
	t.Helper()
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", serverID).Update("package_manager", mgr).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRepairPackagesAptSkripte(t *testing.T) {
	cases := []struct {
		action, jobType string
		want            []string
	}{
		{services.RepairDpkgConfigure, services.JobTypeRepairDpkg,
			[]string{"lcm_apt_wait", "DEBIAN_FRONTEND=noninteractive dpkg --configure -a"}},
		{services.RepairFixDeps, services.JobTypeRepairDeps,
			[]string{"lcm_apt_wait", "lcm_apt -f install -y"}},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			env.Dialer.Commands = nil

			job, err := env.Servers.RepairPackages(repositories.ScopeAll(), id, tc.action, "admin")
			if err != nil {
				t.Fatal(err)
			}
			done := waitForJob(t, env, job.ID)
			if done.Type != tc.jobType || done.Status != domain.JobStatusSuccess {
				t.Fatalf("Job: Typ %q, Status %q", done.Type, done.Status)
			}
			all := strings.Join(env.Dialer.Commands, "\n")
			for _, w := range tc.want {
				if !strings.Contains(all, w) {
					t.Errorf("Skript ohne %q:\n%s", w, all)
				}
			}
		})
	}
}

func TestRepairPackagesZypperUndUnbekanntes(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()

	if _, err := env.Servers.RepairPackages(scope, id, "formatiere_platte", "admin"); !errors.Is(err, services.ErrUnknownRepairAction) {
		t.Errorf("unbekannte Aktion: %v", err)
	}

	setPackageManager(t, env, id, "dnf")
	if _, err := env.Servers.RepairPackages(scope, id, services.RepairFixDeps, "admin"); !errors.Is(err, services.ErrRepairUnsupported) {
		t.Errorf("dnf: %v", err)
	}

	setPackageManager(t, env, id, "zypper")
	if _, err := env.Servers.RepairPackages(scope, id, services.RepairDpkgConfigure, "admin"); !errors.Is(err, services.ErrRepairUnsupported) {
		t.Errorf("zypper + dpkg_configure: %v", err)
	}
	env.Dialer.Commands = nil
	job, err := env.Servers.RepairPackages(scope, id, services.RepairFixDeps, "admin")
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, env, job.ID)
	if all := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(all, "zypper --non-interactive verify") {
		t.Errorf("zypper-Skript:\n%s", all)
	}
}

func TestRepairOhneErkanntePaketverwaltungKeinAptFallback(t *testing.T) {
	// Bei einem Server ohne erkannte Paketverwaltung gibt es keine Reparatur
	// (kein stiller Fallback auf apt).
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	setPackageManager(t, env, id, "")
	if _, err := env.Servers.RepairPackages(repositories.ScopeAll(), id, services.RepairFixDeps, "admin"); !errors.Is(err, services.ErrRepairUnsupported) {
		t.Errorf("ohne Paketverwaltung: %v", err)
	}
}

// Eine Reparatur belegt nicht, dass die fehlgeschlagene Aktion wieder läuft:
// Nur deren Wiederholung heilt die Kategorie.
func TestReparaturHeiltDiePaketKategorieNicht(t *testing.T) {
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	failUpdate(t, env, repo, id)

	job, err := env.Servers.RepairPackages(repositories.ScopeAll(), id, services.RepairFixDeps, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("Reparatur: %q", done.Status)
	}
	// Der Erfolg der Reparatur ist verbucht, wenn die Verbindung heil ist.
	waitCapability(t, repo, id, domain.CapabilityConnection, domain.CapabilityOK)
	if c := requireCapability(t, repo, id, domain.CapabilityPackages); c.State != domain.CapabilityFailed {
		t.Fatalf("packages nach der Reparatur: %+v", c)
	}

	retry, err := env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityPackages, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, retry.ID); done.Type != domain.RuleTypeUpdate || done.Status != domain.JobStatusSuccess {
		t.Fatalf("Wiederholung: Typ %q, Status %q", done.Type, done.Status)
	}
	waitCapability(t, repo, id, domain.CapabilityPackages, domain.CapabilityOK)
}

func TestReparaturJobsBerührenKeineKategorie(t *testing.T) {
	for _, jobType := range []string{services.JobTypeRepairDpkg, services.JobTypeRepairDeps} {
		if got := domain.CapabilityCategoryFor(jobType); got != "" {
			t.Errorf("%s gehört zur Kategorie %q", jobType, got)
		}
	}

	// Auch über den Hook des Jobendes: weder Erfolg noch Fehlschlag einer
	// Reparatur verändert eine Zeile.
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	failUpdate(t, env, repo, id)
	before := requireCapability(t, repo, id, domain.CapabilityPackages)

	svc := services.NewCapabilityService(repo)
	for _, status := range []string{domain.JobStatusSuccess, domain.JobStatusFailed} {
		out := "dpkg: error"
		code := 1
		svc.RecordJob(services.JobOutcome{Job: &domain.Job{
			ServerID: &id, Type: services.JobTypeRepairDpkg, Status: status, Output: out, ExitCode: &code,
		}})
	}
	after := requireCapability(t, repo, id, domain.CapabilityPackages)
	if after.State != domain.CapabilityFailed || after.ConsecutiveFailures != before.ConsecutiveFailures ||
		after.LastJobType != before.LastJobType {
		t.Errorf("packages verändert: vorher %+v, nachher %+v", before, after)
	}
}

func TestRetryCapabilityNurBeiFehlschlag(t *testing.T) {
	env := newTestEnv(t)
	retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()

	if _, err := env.Servers.RetryCapability(scope, id, domain.CapabilityPackages, "admin"); !errors.Is(err, services.ErrCapabilityNotFailed) {
		t.Errorf("Kategorie in Ordnung: %v", err)
	}
	if _, err := env.Servers.RetryCapability(scope, id, "gibtsnicht", "admin"); !errors.Is(err, services.ErrUnknownCapability) {
		t.Errorf("unbekannte Kategorie: %v", err)
	}
	if _, err := env.Servers.RetryCapability(scope, 9999, domain.CapabilityPackages, "admin"); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("unbekannter Server: %v", err)
	}
}

func TestRetryCapabilityStarteDieAdHocAktion(t *testing.T) {
	cases := []struct {
		name, jobType string
		start         func(env *testEnv, id uint) (*domain.Job, error)
	}{
		{"update", domain.RuleTypeUpdate, func(env *testEnv, id uint) (*domain.Job, error) {
			return env.Servers.UpgradeAllPackages(repositories.ScopeAll(), id, "admin")
		}},
		{"security", domain.RuleTypeSecurity, func(env *testEnv, id uint) (*domain.Job, error) {
			return env.Servers.UpgradeSecurityPackages(repositories.ScopeAll(), id, "admin")
		}},
		{"package-scan", domain.RuleTypePackageScan, func(env *testEnv, id uint) (*domain.Job, error) {
			return env.Servers.RefreshPackages(repositories.ScopeAll(), id, "admin")
		}},
		{"autoremove", domain.RuleTypeAutoremove, func(env *testEnv, id uint) (*domain.Job, error) {
			return env.Servers.AutoremovePackages(repositories.ScopeAll(), id, "admin")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			repo := retryEnv(t, env)
			id := joinTestServer(t, env, "web01")
			env.Dialer.Responses["lcm_apt_wait"] = sshx.FakeResponse{Output: lockBusyScriptOutput, ExitCode: 75}
			job, err := tc.start(env, id)
			if err != nil {
				t.Fatal(err)
			}
			waitForJob(t, env, job.ID)
			waitCapability(t, repo, id, domain.CapabilityPackages, domain.CapabilityFailed)
			delete(env.Dialer.Responses, "lcm_apt_wait")

			retry, err := env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityPackages, "admin")
			if err != nil {
				t.Fatal(err)
			}
			done := waitForJob(t, env, retry.ID)
			if done.Type != tc.jobType || done.RuleID != nil || done.Status != domain.JobStatusSuccess {
				t.Fatalf("Wiederholung: Typ %q, Regel %v, Status %q", done.Type, done.RuleID, done.Status)
			}
			waitCapability(t, repo, id, domain.CapabilityPackages, domain.CapabilityOK)
		})
	}
}

// Stammt der Fehlschlag aus einer Regel, läuft dieselbe Regel - nur auf dem
// Server, dessen Karte „Erneut versuchen" zeigt.
func TestRetryCapabilityRegelNurFuerDiesenServer(t *testing.T) {
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	web1 := joinTestServer(t, env, "web01")
	web2 := joinTestServer(t, env, "web02")
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{web1, web2} {
		if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "nächtlich", "0 3 * * *", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Updates", domain.RuleTypeUpdate, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}

	env.Dialer.Responses["lcm_apt_wait"] = sshx.FakeResponse{Output: lockBusyScriptOutput, ExitCode: 75}
	env.Executor.RunRule(rule, "scheduler")
	waitCapability(t, repo, web1, domain.CapabilityPackages, domain.CapabilityFailed)
	waitCapability(t, repo, web2, domain.CapabilityPackages, domain.CapabilityFailed)
	delete(env.Dialer.Responses, "lcm_apt_wait")

	retry, err := env.Servers.RetryCapability(repositories.ScopeAll(), web1, domain.CapabilityPackages, "admin")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, retry.ID)
	if done.RuleID == nil || *done.RuleID != rule.ID || done.ServerID == nil || *done.ServerID != web1 ||
		done.Status != domain.JobStatusSuccess || done.TriggeredBy != "admin" {
		t.Fatalf("Wiederholung: %+v", done)
	}
	waitCapability(t, repo, web1, domain.CapabilityPackages, domain.CapabilityOK)

	// web02 blieb unberührt: kein zweiter Job, Kategorie weiter fehlgeschlagen.
	var jobs int64
	env.DB().Model(&domain.Job{}).Where("server_id = ? AND type = ?", web2, domain.RuleTypeUpdate).Count(&jobs)
	if jobs != 1 {
		t.Errorf("web02 hat %d Update-Jobs, erwartet 1", jobs)
	}
	if c := requireCapability(t, repo, web2, domain.CapabilityPackages); c.State != domain.CapabilityFailed {
		t.Errorf("web02: %+v", c)
	}
}

func TestRetryCapabilityVerbindungPrueftNurDieVerbindung(t *testing.T) {
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	// Das Update, das an der Verbindung scheiterte, ist der Anlass - die
	// Wiederholung darf nicht ein volles Upgrade anstoßen.
	if err := repo.Save(&domain.ServerCapability{
		ServerID: id, Category: domain.CapabilityConnection, State: domain.CapabilityFailed,
		ConsecutiveFailures: 3, LastErrorClass: domain.ErrClassNetwork, LastJobType: domain.RuleTypeUpdate,
	}); err != nil {
		t.Fatal(err)
	}

	retry, err := env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityConnection, "admin")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, retry.ID)
	if done.Type != domain.RuleTypeHealth || done.RuleID != nil || done.Status != domain.JobStatusSuccess {
		t.Fatalf("Verbindungs-Check: Typ %q, Regel %v, Status %q", done.Type, done.RuleID, done.Status)
	}
	waitCapability(t, repo, id, domain.CapabilityConnection, domain.CapabilityOK)
}

func TestRetryCapabilityCVEScan(t *testing.T) {
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	env.Scanner.IsAvailable = false
	env.Executor.RunCVEScan("scheduler")
	waitCapability(t, repo, id, domain.CapabilityCVEScan, domain.CapabilityFailed)

	env.Scanner.IsAvailable = true
	retry, err := env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityCVEScan, "admin")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, retry.ID)
	if done.Type != domain.RuleTypeCVEScan || done.Status != domain.JobStatusSuccess {
		t.Fatalf("CVE-Scan: Typ %q, Status %q, Output %q", done.Type, done.Status, done.Output)
	}
	waitCapability(t, repo, id, domain.CapabilityCVEScan, domain.CapabilityOK)
}

func TestRetryCapabilityBelegterServer(t *testing.T) {
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	failUpdate(t, env, repo, id)

	// Läuft gerade etwas, kommt die Absage sofort (Job-Sperre).
	busy, err := env.Jobs.Start(&id, nil, domain.RuleTypeScript, "Skript @ web01", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityPackages, "admin"); !errors.Is(err, services.ErrServerBusy) {
		t.Errorf("belegter Server: %v", err)
	}
	zero := 0
	env.Jobs.Complete(busy, "", &zero, nil)
}

func TestRetryCapabilityNichtWiederholbar(t *testing.T) {
	env := newTestEnv(t)
	repo := retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	// Ein Skript ohne erhaltene Regel lässt sich nicht erneut auslösen.
	if err := repo.Save(&domain.ServerCapability{
		ServerID: id, Category: domain.CapabilityRules, State: domain.CapabilityFailed,
		LastJobType: domain.RuleTypeScript, LastJobID: "weg",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.RetryCapability(repositories.ScopeAll(), id, domain.CapabilityRules, "admin"); !errors.Is(err, services.ErrNotRetryable) {
		t.Errorf("Skript ohne Regel: %v", err)
	}
}

func TestCapabilityActions(t *testing.T) {
	env := newTestEnv(t)
	retryEnv(t, env)
	id := joinTestServer(t, env, "web01")
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	failed := func(category, class, jobType string) *domain.ServerCapability {
		return &domain.ServerCapability{
			ServerID: id, Category: category, State: domain.CapabilityFailed,
			LastErrorClass: class, LastJobType: jobType,
		}
	}
	cases := []struct {
		name      string
		mgr       string
		c         *domain.ServerCapability
		repair    string
		retryable bool
	}{
		{"dpkg unterbrochen (apt)", "apt", failed("packages", domain.ErrClassDpkgInterrupted, "update"), "dpkg_configure", true},
		{"kaputte Abhängigkeiten (apt)", "apt", failed("packages", domain.ErrClassBrokenDeps, "update"), "fix_deps", true},
		{"kaputte Abhängigkeiten (zypper)", "zypper", failed("packages", domain.ErrClassBrokenDeps, "update"), "fix_deps", true},
		{"dpkg unterbrochen (zypper)", "zypper", failed("packages", domain.ErrClassDpkgInterrupted, "update"), "", true},
		{"kaputte Abhängigkeiten (dnf)", "dnf", failed("packages", domain.ErrClassBrokenDeps, "update"), "", true},
		{"Sperre belegt", "apt", failed("packages", domain.ErrClassLockBusy, "security"), "", true},
		{"Autoremove", "apt", failed("packages", domain.ErrClassUnknown, "autoremove"), "", true},
		{"Einzelpaket-Aktion", "apt", failed("packages", domain.ErrClassUnknown, "packages"), "", false},
		{"Regel-Skript", "apt", failed("rules", domain.ErrClassUnknown, "script"), "", false},
		{"CVE-Scan", "apt", failed("cve_scan", domain.ErrClassScannerUnavailable, "cve-scan"), "", true},
		{"Verbindung", "apt", failed("connection", domain.ErrClassNetwork, "update"), "", true},
		{"in Ordnung", "apt", &domain.ServerCapability{ServerID: id, Category: "packages", State: domain.CapabilityOK}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server.PackageManager = tc.mgr
			got := env.Servers.CapabilityActions(server, tc.c)
			if got.Repair != tc.repair || got.Retryable != tc.retryable {
				t.Errorf("Repair %q (erwartet %q), Retryable %v (erwartet %v)", got.Repair, tc.repair, got.Retryable, tc.retryable)
			}
		})
	}
}
