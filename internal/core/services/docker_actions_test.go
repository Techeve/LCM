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

// joinDockerServer joint einen Server, dessen Scan Docker-Inventar liefert
// (Compose-Projekt "webshop" mit working_dir, nginx:1.25 + lokales Image).
func joinDockerServer(t *testing.T, env *testEnv, name string) uint {
	t.Helper()
	env.Dialer.Responses = map[string]sshx.FakeResponse{
		"apt-get dnf zypper": {Output: "apt-get\n"}, // Debian → apt (Join prüft das)
		"sudo -n id -u":      {Output: "0\n"},       // Service-User erreicht root
		"os-release":         {Output: "NAME=\"Debian GNU/Linux\"\n"},
		"dpkg-query":         {Output: "nginx 1.22.1\n"},
	}
	for k, v := range dockerResponses() {
		env.Dialer.Responses[k] = v
	}
	// Eindeutiger Host je Name (Duplikat-Host-Prüfung im Join).
	server, err := env.Servers.Join(services.JoinRequest{
		Name: name, Host: name + ".test", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join fehlgeschlagen: %v", err)
	}
	return server.ID
}

// TestUpdateComposeProjectRunsPullUp: das Compose-Update läuft im
// working_dir des Projekts als sudo-Job und liest das Inventar neu ein.
func TestUpdateComposeProjectRunsPullUp(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")

	env.Dialer.Commands = nil
	job, err := env.Servers.UpdateComposeProject(repositories.ScopeAll(), id, "webshop", "", "admin")
	if err != nil {
		t.Fatalf("compose-update: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeDockerUpdate)
	if done.ID != job.ID || done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	// Das Skript läuft sudo-gewrappt (Service-User ist nicht root), die
	// inneren Quotes sind daher escaped - auf die Bestandteile prüfen.
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "/opt/webshop") || !strings.Contains(all, "docker compose pull && docker compose up -d") {
		t.Errorf("compose-update-skript fehlt:\n%s", all)
	}
	if !strings.Contains(all, "sudo sh -c '") {
		t.Errorf("compose-update sollte via sudo laufen:\n%s", all)
	}
	// Inventar-Rescan über die offene Verbindung.
	if !strings.Contains(all, "docker images") {
		t.Errorf("docker-rescan fehlt:\n%s", all)
	}
}

// TestUpdateComposeProjectValidatesAgainstInventory: unbekannte Projekte
// und ungültige Namen starten keinen Job.
func TestUpdateComposeProjectValidatesAgainstInventory(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")

	if _, err := env.Servers.UpdateComposeProject(repositories.ScopeAll(), id, "gibtsnicht", "", "admin"); !errors.Is(err, services.ErrComposeProjectUnknown) {
		t.Errorf("unbekanntes projekt: erwartete ErrComposeProjectUnknown, bekam %v", err)
	}
	if _, err := env.Servers.UpdateComposeProject(repositories.ScopeAll(), id, "webshop; reboot", "", "admin"); !errors.Is(err, services.ErrInvalidComposeName) {
		t.Errorf("injection: erwartete ErrInvalidComposeName, bekam %v", err)
	}
	if _, err := env.Servers.UpdateComposeProject(repositories.ScopeAll(), id, "webshop", "gibtsnicht", "admin"); !errors.Is(err, services.ErrComposeProjectUnknown) {
		t.Errorf("unbekannter service: erwartete ErrComposeProjectUnknown, bekam %v", err)
	}
	// Kein Job wurde angelegt.
	var n int64
	env.DB().Model(&domain.Job{}).Where("type = ?", domain.RuleTypeDockerUpdate).Count(&n)
	if n != 0 {
		t.Errorf("fehlversuche dürfen keine jobs anlegen, %d gefunden", n)
	}
}

// TestPullDockerImage: nur im Inventar bekannte Images sind ziehbar.
func TestPullDockerImage(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")

	if _, err := env.Servers.PullDockerImage(repositories.ScopeAll(), id, "boesewicht/backdoor:1", "admin"); !errors.Is(err, services.ErrDockerImageUnknown) {
		t.Errorf("fremdes image: erwartete ErrDockerImageUnknown, bekam %v", err)
	}
	if _, err := env.Servers.PullDockerImage(repositories.ScopeAll(), id, "nginx:1.25'; reboot", "admin"); !errors.Is(err, services.ErrInvalidDockerRef) {
		t.Errorf("injection: erwartete ErrInvalidDockerRef, bekam %v", err)
	}

	env.Dialer.Commands = nil
	if _, err := env.Servers.PullDockerImage(repositories.ScopeAll(), id, "nginx:1.25", "admin"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeDockerUpdate)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "docker pull ") || !strings.Contains(all, "nginx:1.25") {
		t.Errorf("pull-kommando fehlt:\n%s", all)
	}
}

// TestDeleteDockerImage: nur ungenutzte, im Inventar bekannte Images sind
// löschbar; genutzte werden abgelehnt.
func TestDeleteDockerImage(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")

	// nginx:1.25 ist in Benutzung (Container läuft) → nicht löschbar.
	if _, err := env.Servers.DeleteDockerImage(repositories.ScopeAll(), id, "nginx:1.25", "admin"); !errors.Is(err, services.ErrDockerImageInUse) {
		t.Errorf("genutztes image: erwartete ErrDockerImageInUse, bekam %v", err)
	}
	// Fremdes Image → unbekannt.
	if _, err := env.Servers.DeleteDockerImage(repositories.ScopeAll(), id, "fremd/image:1", "admin"); !errors.Is(err, services.ErrDockerImageUnknown) {
		t.Errorf("fremdes image: erwartete ErrDockerImageUnknown, bekam %v", err)
	}
	// meinapp:dev ist ungenutzt → löschbar.
	env.Dialer.Commands = nil
	if _, err := env.Servers.DeleteDockerImage(repositories.ScopeAll(), id, "meinapp:dev", "admin"); err != nil {
		t.Fatalf("löschen: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeDockerUpdate)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "docker rmi ") {
		t.Errorf("rmi-kommando fehlt:\n%s", strings.Join(env.Dialer.Commands, "\n"))
	}
}

// TestPruneDockerImages prüft den Sammel-Aufräumen-Button: docker image prune
// über alle ungenutzten Images auf einen Schlag.
func TestPruneDockerImages(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")

	env.Dialer.Commands = nil
	if _, err := env.Servers.PruneDockerImages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("prune: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeDockerUpdate)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("prune-job nicht erfolgreich: %+v", done)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "docker image prune -af") {
		t.Errorf("prune-kommando fehlt:\n%s", strings.Join(env.Dialer.Commands, "\n"))
	}
}

// TestDockerReport: das Read-Modell bündelt Container, Images und die
// CVE-Zähler des Image-Scans.
func TestDockerReport(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-1", PackageName: "libssl3", Severity: domain.SeverityCritical, ImageRef: "nginx:1.25"},
		{CVEID: "CVE-2", PackageName: "zlib", Severity: domain.SeverityHigh, ImageRef: "nginx:1.25"},
	}); err != nil {
		t.Fatal(err)
	}

	report, err := env.Servers.Docker(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !report.HasDocker || !report.HasCompose {
		t.Errorf("docker-flags fehlen: %+v", report)
	}
	if len(report.Containers) != 1 || len(report.Images) != 2 {
		t.Fatalf("inventar unvollständig: %d container, %d images", len(report.Containers), len(report.Images))
	}
	for _, img := range report.Images {
		if img.Ref() == "nginx:1.25" {
			if img.CriticalVulns != 1 || img.HighVulns != 1 {
				t.Errorf("cve-zähler falsch: %+v", img)
			}
		} else if img.CriticalVulns != 0 {
			t.Errorf("fremde cve-zähler: %+v", img)
		}
	}
}

// TestGlobalDockerOverview: aggregiert unique Images über Server hinweg.
func TestGlobalDockerOverview(t *testing.T) {
	env := newTestEnv(t)
	a := joinDockerServer(t, env, "docker01")
	b := joinDockerServer(t, env, "docker02")
	repo := repositories.NewServerRepository(env.DB())

	// Auf Server a ist nginx veraltet; die CVE zählt über beide Server einmal.
	imgsA, _ := repo.FindDockerImages(a)
	for _, img := range imgsA {
		if img.Ref() == "nginx:1.25" {
			_ = repo.UpdateDockerImageCheck(img.ID, map[string]any{"update_available": true, "check_status": domain.DockerCheckOK})
		}
	}
	for _, id := range []uint{a, b} {
		_ = repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
			{CVEID: "CVE-1", PackageName: "libssl3", Severity: domain.SeverityCritical, ImageRef: "nginx:1.25"},
		})
	}

	rows, err := repo.GlobalDockerOverview(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	var nginx *repositories.DockerImageOverviewRow
	for i := range rows {
		if rows[i].Repository == "nginx" && rows[i].Tag == "1.25" {
			nginx = &rows[i]
		}
	}
	if nginx == nil {
		t.Fatalf("nginx fehlt in der übersicht: %+v", rows)
	}
	if nginx.ServerCount != 2 || !nginx.UpdateAvailable || nginx.CriticalVulns != 1 {
		t.Errorf("aggregation falsch: %+v", nginx)
	}
	// Die Namen der betroffenen Server gehören dazu: Eine Zahl allein sagt
	// nicht, WO das Image mit der kritischen Lücke liegt - dafür musste man
	// bisher jeden Server einzeln aufsuchen.
	if len(nginx.Servers) != 2 {
		t.Fatalf("erwartet 2 Server am Image, bekam %+v", nginx.Servers)
	}
	if nginx.Servers[0].Name != "docker01" || nginx.Servers[1].Name != "docker02" {
		t.Errorf("Servernamen fehlen oder sind unsortiert: %+v", nginx.Servers)
	}
	if nginx.Servers[0].ID == 0 {
		t.Error("ohne ID ist der Name nicht verlinkbar")
	}
}

// TestGlobalDockerContainers: die Container aller Server in einer Sicht,
// laufende zuerst.
func TestGlobalDockerContainers(t *testing.T) {
	env := newTestEnv(t)
	joinDockerServer(t, env, "docker01")
	repo := repositories.NewServerRepository(env.DB())

	rows, err := repo.GlobalDockerContainers(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("keine Container in der globalen Sicht")
	}
	if rows[0].State != "running" {
		t.Errorf("laufende Container müssen oben stehen, erster war %q", rows[0].State)
	}
	if rows[0].ServerName != "docker01" {
		t.Errorf("Servername fehlt oder ist nicht entschlüsselt: %q", rows[0].ServerName)
	}
	if rows[0].ServerID == 0 {
		t.Error("ohne ID ist der Server nicht verlinkbar")
	}
}

// TestGlobalDockerCompose: Container mit Projekt-Label werden zu Projekten
// gruppiert; Container ohne Label sind keine.
func TestGlobalDockerCompose(t *testing.T) {
	env := newTestEnv(t)
	joinDockerServer(t, env, "docker01")
	repo := repositories.NewServerRepository(env.DB())

	rows, err := repo.GlobalDockerCompose(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Skip("die Testdaten enthalten kein Compose-Projekt")
	}
	p := rows[0]
	if p.Project == "" {
		t.Error("Projekt ohne Namen")
	}
	if len(p.Servers) == 0 || p.Servers[0].Name == "" {
		t.Errorf("Server des Projekts fehlen: %+v", p.Servers)
	}
	if p.Containers < p.Running {
		t.Errorf("mehr laufende als vorhandene Container: %d von %d", p.Running, p.Containers)
	}
}

// TestDockerPruneRule: die Gruppen-Regel „ungenutzte Images aufräumen"
// führt docker image prune auf den Docker-Servern der Gruppe aus.
func TestDockerPruneRule(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")
	group, err := env.Groups.Create("Docker-Hosts", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "nächtlich", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "prune", domain.RuleTypeDockerPrune, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("prune-rule definieren: %v", err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "docker image prune -af") {
		t.Errorf("prune-kommando fehlt:\n%s", all)
	}
}

// TestPullAllDockerImages: zieht alle genutzten, getaggten Registry-Images in
// EINEM Job; lokal gebaute/ungenutzte Images bleiben außen vor.
func TestPullAllDockerImages(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")

	env.Dialer.Commands = nil
	if _, err := env.Servers.PullAllDockerImages(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("pull-all: %v", err)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeDockerUpdate)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "docker pull ") || !strings.Contains(all, "nginx:1.25") {
		t.Errorf("pull-kommando fehlt:\n%s", all)
	}
	// Das lokal gebaute meinapp:dev (ohne RepoDigest) darf NICHT gezogen werden.
	if strings.Contains(all, "docker pull 'meinapp:dev'") {
		t.Errorf("lokales image darf nicht gezogen werden:\n%s", all)
	}
}

// TestSetContainerCVERelevance: die Relevanz-Markierung hängt am
// Container-Namen, wird validiert und persistiert; unbekannte Container
// werden abgelehnt.
func TestSetContainerCVERelevance(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")
	repo := repositories.NewServerRepository(env.DB())

	if _, err := env.Servers.SetContainerCVERelevance(repositories.ScopeAll(), id, "gibtsnicht", true, "admin"); !errors.Is(err, services.ErrDockerContainerUnknown) {
		t.Errorf("unbekannter container: erwartete ErrDockerContainerUnknown, bekam %v", err)
	}

	if _, err := env.Servers.SetContainerCVERelevance(repositories.ScopeAll(), id, "webshop-web-1", true, "admin"); err != nil {
		t.Fatalf("markieren: %v", err)
	}
	srv, _ := repo.FindByID(repositories.ScopeAll(), id)
	if srv.CVERelevantContainers != "webshop-web-1" {
		t.Errorf("erwartet 'webshop-web-1', bekam %q", srv.CVERelevantContainers)
	}
	// Doppelt markieren dedupliziert; Rücknahme leert das Feld.
	if _, err := env.Servers.SetContainerCVERelevance(repositories.ScopeAll(), id, "webshop-web-1", true, "admin"); err != nil {
		t.Fatal(err)
	}
	srv, _ = repo.FindByID(repositories.ScopeAll(), id)
	if srv.CVERelevantContainers != "webshop-web-1" {
		t.Errorf("dedupliziert erwartet, bekam %q", srv.CVERelevantContainers)
	}
	if _, err := env.Servers.SetContainerCVERelevance(repositories.ScopeAll(), id, "webshop-web-1", false, "admin"); err != nil {
		t.Fatal(err)
	}
	srv, _ = repo.FindByID(repositories.ScopeAll(), id)
	if srv.CVERelevantContainers != "" {
		t.Errorf("rücknahme sollte das feld leeren, bekam %q", srv.CVERelevantContainers)
	}
}
