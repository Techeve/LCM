package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/infrastructure/trivy"
	"LCM/internal/storage/repositories"
)

// lastServerJob wartet auf den zuletzt angelegten Job eines Servers und
// liefert ihn (Paket-Updates laufen asynchron in einer Goroutine).
func waitServerJob(t *testing.T, env *testEnv, serverID uint, jobType string) domain.Job {
	t.Helper()
	var job domain.Job
	waitFor(t, func() bool {
		err := env.DB().Where("server_id = ? AND type = ?", serverID, jobType).
			Order("rowid desc").First(&job).Error
		return err == nil && job.Status != domain.JobStatusRunning
	})
	return job
}

func TestUpgradeAllPackagesRunsAptAndRescan(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Commands = nil
	job, err := env.Servers.UpgradeAllPackages(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("upgrade-all: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeUpdate)
	if done.ID != job.ID || done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "apt-get") || !strings.Contains(all, "upgrade") {
		t.Errorf("apt-upgrade nicht ausgeführt:\n%s", all)
	}
	// Rescan: dpkg-query + apt list wurden erneut ausgeführt.
	if !strings.Contains(all, "dpkg-query") {
		t.Errorf("paket-rescan fehlt:\n%s", all)
	}
}

// TestPackageUpdateRefreshesCVEs deckt den gemeldeten Bug ab: nach einem
// Paket-Update muss die CVE-Bewertung automatisch neu laufen - sonst blieben
// veraltete Sicherheits-Labels an bereits aktualisierten Paketen hängen.
func TestPackageUpdateRefreshesCVEs(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Ausgangslage: der Scan meldet eine kritische Lücke.
	env.Scanner.ScanFunc = func(trivy.Target) ([]domain.Vulnerability, error) {
		return []domain.Vulnerability{{
			CVEID: "CVE-2024-9999", PackageName: "openssl",
			InstalledVersion: "1.0", FixedVersion: "1.1",
			Severity: domain.SeverityCritical, Title: "test-lücke",
		}}, nil
	}
	env.Executor.RunCVEScanServer(id, "admin")
	rep, err := env.Servers.Vulnerabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Vulnerabilities) != 1 {
		t.Fatalf("erwartete 1 CVE nach Erst-Scan, bekam %d", len(rep.Vulnerabilities))
	}

	// Das Update behebt die Lücke: der nächste Scan meldet nichts mehr.
	env.Scanner.ScanFunc = func(trivy.Target) ([]domain.Vulnerability, error) { return nil, nil }
	env.Scanner.Calls = 0

	if _, err := env.Servers.UpgradeAllPackages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatal(err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeUpdate)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("update-job nicht erfolgreich: %+v", done)
	}
	if env.Scanner.Calls == 0 {
		t.Error("CVE-Neubewertung nach dem Update lief nicht")
	}
	if !strings.Contains(done.Output, "CVE-Bewertung nach dem Update aktualisiert") {
		t.Errorf("job-protokoll ohne CVE-Neubewertungs-Notiz:\n%s", done.Output)
	}
	// Die veralteten Labels sind verschwunden.
	rep, err = env.Servers.Vulnerabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Vulnerabilities) != 0 {
		t.Errorf("veraltete CVE-Labels blieben nach dem Update hängen: %d", len(rep.Vulnerabilities))
	}
}

// TestPackageRuleRespectsCVEScanSetting prüft den Executor-/Regel-Pfad: die
// automatische CVE-Neubewertung nach einer Paket-Update-Rule unterbleibt, wenn
// der CVE-Scan global deaktiviert ist, und läuft, wenn er aktiviert ist.
func TestPackageRuleRespectsCVEScanSetting(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Nacht", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "upd", domain.RuleTypeUpdate, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Deaktiviert: kein Rescan.
	if err := env.DB().Model(&domain.GlobalSettings{}).Where("id = ?", 1).Update("cve_scan_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	env.Scanner.Calls = 0
	env.Executor.RunRule(rule, "admin")
	if env.Scanner.Calls != 0 {
		t.Errorf("CVE-Scan lief trotz deaktivierter Einstellung (%d Aufrufe)", env.Scanner.Calls)
	}

	// Aktiviert: Rescan läuft.
	if err := env.DB().Model(&domain.GlobalSettings{}).Where("id = ?", 1).Update("cve_scan_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	env.Scanner.Calls = 0
	env.Executor.RunRule(rule, "admin")
	if env.Scanner.Calls == 0 {
		t.Error("CVE-Scan lief nicht trotz aktivierter Einstellung")
	}
}

func TestRefreshPackages(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Commands = nil
	if _, err := env.Servers.RefreshPackages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("paketliste aktualisieren: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypePackageScan)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("package-scan-job nicht erfolgreich: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	// apt-get update läuft, aber KEIN install/upgrade (installiert nichts).
	if !strings.Contains(all, "apt-get") || !strings.Contains(all, "update") {
		t.Errorf("apt-get update nicht ausgeführt:\n%s", all)
	}
	if strings.Contains(all, "upgrade") || strings.Contains(all, "install") {
		t.Errorf("package-scan darf nichts installieren:\n%s", all)
	}
	// Rescan (dpkg-query) muss laufen, damit der Bestand aktualisiert wird.
	if !strings.Contains(all, "dpkg-query") {
		t.Errorf("paket-rescan fehlt:\n%s", all)
	}
}

func TestUpgradeSecurityPackages(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Commands = nil
	if _, err := env.Servers.UpgradeSecurityPackages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatal(err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeSecurity)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("security-job nicht erfolgreich: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "security") || !strings.Contains(all, "--only-upgrade") {
		t.Errorf("security-filter nicht angewendet:\n%s", all)
	}
}

func TestUpdatePackagesNamedAndVersion(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Benannte Pakete.
	env.Dialer.Commands = nil
	if _, err := env.Servers.UpdatePackages(repositories.ScopeAll(), id, []string{"htop", "nginx"}, "", "admin"); err != nil {
		t.Fatal(err)
	}
	waitServerJob(t, env, id, domain.RuleTypePackages)
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "install --only-upgrade -y htop nginx") {
		t.Errorf("benannte pakete nicht aktualisiert: %v", env.Dialer.Commands)
	}

	// Exakte Version (Downgrade erlaubt).
	env.Dialer.Commands = nil
	if _, err := env.Servers.UpdatePackages(repositories.ScopeAll(), id, []string{"nginx"}, "1.24.0-1", "admin"); err != nil {
		t.Fatal(err)
	}
	waitServerJob(t, env, id, domain.RuleTypePackages)
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "install -y --allow-downgrades nginx=1.24.0-1") {
		t.Errorf("versionsgenaue installation falsch: %v", env.Dialer.Commands)
	}
}

// TestAutoremovePackages: der Autoremove-Lauf setzt apt autoremove ab.
func TestAutoremovePackages(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Commands = nil
	if _, err := env.Servers.AutoremovePackages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatal(err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeAutoremove)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("autoremove-job nicht erfolgreich: %+v", done)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "autoremove") {
		t.Errorf("autoremove nicht ausgeführt: %v", env.Dialer.Commands)
	}
}

// TestRemovePackages: gezieltes Entfernen setzt apt remove ab; geschützte
// Systempakete werden abgelehnt (kein Job).
func TestRemovePackages(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Gewöhnliches Paket → apt remove.
	env.Dialer.Commands = nil
	if _, err := env.Servers.RemovePackages(repositories.ScopeAll(), id, []string{"altpaket"}, "admin"); err != nil {
		t.Fatal(err)
	}
	waitServerJob(t, env, id, domain.RuleTypeAutoremove)
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "remove altpaket") {
		t.Errorf("gezieltes remove nicht ausgeführt: %v", env.Dialer.Commands)
	}

	// Geschütztes Paket → Ablehnung, kein Job.
	if _, err := env.Servers.RemovePackages(repositories.ScopeAll(), id, []string{"openssh-server"}, "admin"); !errors.Is(err, services.ErrProtectedPackage) {
		t.Errorf("geschütztes paket sollte ErrProtectedPackage liefern, bekam: %v", err)
	}
	// Auch in einer Liste mit einem harmlosen Paket muss das geschützte kippen.
	if _, err := env.Servers.RemovePackages(repositories.ScopeAll(), id, []string{"htop", "sudo"}, "admin"); !errors.Is(err, services.ErrProtectedPackage) {
		t.Errorf("gemischte liste mit sudo sollte abgelehnt werden, bekam: %v", err)
	}
	// Ungültiger Name → Validierungsfehler.
	if _, err := env.Servers.RemovePackages(repositories.ScopeAll(), id, []string{"rm -rf /"}, "admin"); err == nil {
		t.Error("gefährlicher paketname sollte abgelehnt werden")
	}
}

func TestUpdatePackagesValidation(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Version mit mehr als einem Paket.
	if _, err := env.Servers.UpdatePackages(repositories.ScopeAll(), id, []string{"a", "b"}, "1.0", "admin"); err == nil {
		t.Error("version für mehrere pakete sollte fehlschlagen")
	}
	// Ungültiger Paketname.
	if _, err := env.Servers.UpdatePackages(repositories.ScopeAll(), id, []string{"rm -rf /"}, "", "admin"); err == nil {
		t.Error("gefährlicher paketname sollte abgelehnt werden")
	}
	// Ungültige Version.
	if _, err := env.Servers.UpdatePackages(repositories.ScopeAll(), id, []string{"nginx"}, "1.0; reboot", "admin"); err == nil {
		t.Error("gefährliche version sollte abgelehnt werden")
	}
}

func TestPackageVersionsParsesMadison(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["apt-cache madison"] = sshx.FakeResponse{Output: " nginx | 1.24.0-1 | http://deb.debian.org bookworm/main amd64 Packages\n nginx | 1.22.1-9 | http://deb.debian.org bookworm/main amd64 Packages\n"}

	versions, err := env.Servers.PackageVersions(repositories.ScopeAll(), id, "nginx")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != "1.24.0-1" {
		t.Errorf("versionen falsch geparst: %v", versions)
	}
	// Ungültiger Paketname wird nicht ausgeführt.
	if _, err := env.Servers.PackageVersions(repositories.ScopeAll(), id, "foo; rm"); err == nil {
		t.Error("gefährlicher paketname sollte abgelehnt werden")
	}
}

// TestPackagesRuleAppliesToGroupServers prüft, dass eine benannte
// Paket-Update-Rule die genannten Pakete auf allen Gruppen-Servern aktualisiert.
func TestPackagesRuleAppliesToGroupServers(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	group, err := env.Groups.Create("Webserver", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Nacht", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	// Ungültige Paketliste wird beim Definieren abgelehnt.
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "bad", domain.RuleTypePackages, "rm -rf /", &sched.ID, false, "admin"); err == nil {
		t.Error("ungültige paketliste sollte abgelehnt werden")
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "web-pkgs", domain.RuleTypePackages, "htop, unzip", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("packages-rule definieren: %v", err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "install --only-upgrade -y htop unzip") {
		t.Errorf("packages-rule nicht angewendet: %v", env.Dialer.Commands)
	}
}

// TestServerBusyRejectsSecondPackageJob stellt sicher, dass überlappende
// Paket-Jobs pro Server durch den Concurrency-Lock verhindert werden.
func TestServerBusyRejectsSecondPackageJob(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Ersten Job manuell als laufend anlegen (blockiert den Server).
	if _, err := env.Jobs.Start(&id, nil, "update", "block", "admin"); err != nil {
		t.Fatal(err)
	}
	_, err := env.Servers.UpgradeAllPackages(repositories.ScopeAll(), id, "admin")
	if !errors.Is(err, services.ErrServerBusy) {
		t.Errorf("zweiter paket-job sollte ErrServerBusy liefern, bekam %v", err)
	}
}
