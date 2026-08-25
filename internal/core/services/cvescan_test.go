package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/trivy"
	"LCM/internal/storage/repositories"
)

// TestCVEScanServerStoresAndEscalates prüft den Einzel-Server-Scan: die Funde
// landen in der DB, die Zusammenfassung stimmt und ein kritischer Fund
// eskaliert die Ampel auf Rot - auch bei erreichbarem Server.
func TestCVEScanServerStoresAndEscalates(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Scanner.ScanFunc = func(target trivy.Target) ([]domain.Vulnerability, error) {
		if len(target.Packages) == 0 {
			t.Error("Scanner ohne Pakete aufgerufen")
		}
		return []domain.Vulnerability{
			{CVEID: "CVE-2023-0286", PackageName: "openssl", InstalledVersion: "3.0.11",
				FixedVersion: "3.0.14", Severity: domain.SeverityCritical, PkgManager: "apt"},
			{CVEID: "CVE-2023-44487", PackageName: "nginx", InstalledVersion: "1.22.1",
				Severity: domain.SeverityHigh, PkgManager: "apt"},
		}, nil
	}

	env.Executor.RunCVEScanServer(id, "admin")

	report, err := env.Servers.Vulnerabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatalf("Vulnerabilities: %v", err)
	}
	if len(report.Vulnerabilities) != 2 {
		t.Fatalf("erwartet 2 Funde, bekam %d", len(report.Vulnerabilities))
	}
	// Kritischster zuerst.
	if report.Vulnerabilities[0].Severity != domain.SeverityCritical {
		t.Errorf("erster Fund sollte kritisch sein, war %q", report.Vulnerabilities[0].Severity)
	}
	if report.Summary[domain.SeverityCritical] != 1 || report.Summary[domain.SeverityHigh] != 1 {
		t.Errorf("Zusammenfassung falsch: %+v", report.Summary)
	}
	if report.LastScanAt == nil {
		t.Error("last_scan_at nicht gesetzt")
	}
	if !report.ScannerAvailable {
		t.Error("scanner_available sollte true sein")
	}

	// Kritischer CVE → Ampel rot (obwohl erreichbar).
	status, insights, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != domain.ServerStatusRed {
		t.Errorf("erwartet rot wegen kritischem CVE, bekam %q", status)
	}
	foundCrit := false
	for _, in := range insights {
		if in.Severity == "critical" && strings.Contains(in.Message, "kritische") {
			foundCrit = true
		}
	}
	if !foundCrit {
		t.Errorf("kritischer CVE-Insight fehlt: %+v", insights)
	}
}

// TestCVEScanGracefulDegrade: fehlt Trivy, endet der Scan-Job mit einem klaren
// Hinweis statt mit einem Fehler.
func TestCVEScanGracefulDegrade(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	env.Scanner.IsAvailable = false

	env.Executor.RunCVEScan("scheduler")

	var job domain.Job
	if err := env.DB().Where("type = ?", domain.RuleTypeCVEScan).Order("rowid desc").First(&job).Error; err != nil {
		t.Fatalf("kein CVE-Scan-Job: %v", err)
	}
	if job.Status != domain.JobStatusSuccess {
		t.Errorf("Job sollte erfolgreich sein (graceful degrade), war %q", job.Status)
	}
	if !strings.Contains(job.Output, "Trivy nicht verfügbar") {
		t.Errorf("Hinweis auf fehlendes Trivy erwartet, Output: %q", job.Output)
	}
}

// TestCVEScanAllServers prüft den System-Job über alle Server.
func TestCVEScanAllServers(t *testing.T) {
	env := newTestEnv(t)
	a := joinTestServer(t, env, "web01")
	b := joinTestServer(t, env, "web02")

	env.Scanner.ScanFunc = func(trivy.Target) ([]domain.Vulnerability, error) {
		return []domain.Vulnerability{
			{CVEID: "CVE-2023-3446", PackageName: "openssl", Severity: domain.SeverityHigh, PkgManager: "apt"},
		}, nil
	}

	env.Executor.RunCVEScan("scheduler")

	for _, id := range []uint{a, b} {
		rep, _ := env.Servers.Vulnerabilities(repositories.ScopeAll(), id)
		if len(rep.Vulnerabilities) != 1 {
			t.Errorf("Server %d: erwartet 1 Fund, bekam %d", id, len(rep.Vulnerabilities))
		}
	}

	var job domain.Job
	env.DB().Where("type = ?", domain.RuleTypeCVEScan).Order("rowid desc").First(&job)
	if !strings.Contains(job.Output, "2 Server gescannt") {
		t.Errorf("Zusammenfassung erwartet '2 Server gescannt', Output: %q", job.Output)
	}
}

// TestGlobalVulnerabilitiesOrdered prüft die globale CVE-Sicht: kritischste
// zuerst.
func TestGlobalVulnerabilitiesOrdered(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-A", PackageName: "z", Severity: domain.SeverityLow},
		{CVEID: "CVE-B", PackageName: "a", Severity: domain.SeverityCritical},
		{CVEID: "CVE-C", PackageName: "m", Severity: domain.SeverityHigh},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.GlobalVulnerabilities(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("erwartet 3, bekam %d", len(rows))
	}
	if rows[0].Severity != domain.SeverityCritical || rows[2].Severity != domain.SeverityLow {
		t.Errorf("Sortierung nach Schwere falsch: %q … %q", rows[0].Severity, rows[2].Severity)
	}
}

// TestGlobalVulnerabilitiesPage prüft die serverseitige Pagination: Seiten-Slice,
// Gesamtzahl, Schwere-Summary (über ALLE) und kritischste-zuerst-Sortierung.
func TestGlobalVulnerabilitiesPage(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	var vulns []domain.Vulnerability
	for i := 0; i < 5; i++ {
		vulns = append(vulns, domain.Vulnerability{CVEID: "CVE-C-" + string(rune('a'+i)), PackageName: "p", Severity: domain.SeverityCritical})
	}
	for i := 0; i < 7; i++ {
		vulns = append(vulns, domain.Vulnerability{CVEID: "CVE-L-" + string(rune('a'+i)), PackageName: "p", Severity: domain.SeverityLow})
	}
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceOS, vulns); err != nil {
		t.Fatal(err)
	}

	// Seite 1 (Größe 4): 4 Treffer, Total 12, Summary vollständig, kritische zuerst.
	p1, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Total != 12 {
		t.Errorf("Total: erwartet 12, bekam %d", p1.Total)
	}
	if len(p1.Items) != 4 {
		t.Errorf("Seite 1: erwartet 4 Einträge, bekam %d", len(p1.Items))
	}
	if p1.Summary[domain.SeverityCritical] != 5 || p1.Summary[domain.SeverityLow] != 7 {
		t.Errorf("Summary falsch: %+v", p1.Summary)
	}
	for _, it := range p1.Items {
		if it.Severity != domain.SeverityCritical {
			t.Errorf("Seite 1 sollte nur kritische enthalten, bekam %q", it.Severity)
		}
	}

	// Letzte Seite: Rest.
	p3, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 3, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p3.Items) != 4 || p3.Items[len(p3.Items)-1].Severity != domain.SeverityLow {
		t.Errorf("Seite 3 unerwartet: %d Einträge", len(p3.Items))
	}
}

// TestGlobalVulnerabilitiesPageSourceFilter prüft den Quelle-Filter: "os"
// blendet Docker-CVEs aus, "docker" zeigt nur Container-Funde.
func TestGlobalVulnerabilitiesPageSourceFilter(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-OS-1", PackageName: "openssl", Severity: domain.SeverityHigh},
		{CVEID: "CVE-OS-2", PackageName: "bash", Severity: domain.SeverityLow},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-DK-1", PackageName: "libc", Severity: domain.SeverityCritical, ImageRef: "nginx:1.25"},
	}); err != nil {
		t.Fatal(err)
	}

	all, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 {
		t.Fatalf("ohne Filter: erwartet 3, bekam %d", all.Total)
	}

	osOnly, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 50, "os")
	if err != nil {
		t.Fatal(err)
	}
	if osOnly.Total != 2 {
		t.Fatalf("os-Filter: erwartet 2, bekam %d", osOnly.Total)
	}
	for _, it := range osOnly.Items {
		if it.Source == "docker" {
			t.Errorf("os-Filter sollte keine Docker-CVEs enthalten: %+v", it)
		}
	}

	dockerOnly, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 50, "docker")
	if err != nil {
		t.Fatal(err)
	}
	if dockerOnly.Total != 1 || (len(dockerOnly.Items) > 0 && dockerOnly.Items[0].Source != "docker") {
		t.Fatalf("docker-Filter unerwartet: %+v", dockerOnly)
	}
}

// TestGlobalVulnerabilitiesHidesIgnoredDockerFindings: Ist für einen Server
// „CVEs aus Container-Images ignorieren" gesetzt, tauchen seine Docker-Funde
// in der Sicherheitsübersicht gar nicht mehr auf - auch nicht über den
// ausdrücklichen Quellen-Filter „docker". Die OS-Funde desselben Servers
// bleiben unberührt: Abgeschaltet ist der Container-Teil, nicht der Server.
func TestGlobalVulnerabilitiesHidesIgnoredDockerFindings(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-OS-1", PackageName: "openssl", Severity: domain.SeverityHigh},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-DK-1", PackageName: "libc", Severity: domain.SeverityCritical, ImageRef: "nginx:1.25"},
	}); err != nil {
		t.Fatal(err)
	}

	// Vorher: beide Funde sind sichtbar.
	before, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if before.Total != 2 {
		t.Fatalf("vor dem schalter: erwartet 2, bekam %d", before.Total)
	}

	on := true
	if _, err := env.Servers.UpdateSettings(repositories.ScopeAll(), id,
		services.ServerSettingsInput{DockerCVEsIgnored: &on}, "admin"); err != nil {
		t.Fatalf("einstellung setzen: %v", err)
	}

	after, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Total != 1 {
		t.Fatalf("nach dem schalter sollte nur der OS-Fund bleiben, bekam %d", after.Total)
	}
	if len(after.Items) > 0 && after.Items[0].Source == "docker" {
		t.Errorf("docker-fund trotz schalter sichtbar: %+v", after.Items[0])
	}
	// Auch der ausdrückliche Docker-Filter zeigt nichts mehr - sonst wäre die
	// Ruhe auf einen Klick beschränkt.
	dockerOnly, err := repo.GlobalVulnerabilitiesPage(repositories.ScopeAll(), 1, 50, "docker")
	if err != nil {
		t.Fatal(err)
	}
	if dockerOnly.Total != 0 {
		t.Errorf("docker-filter sollte leer sein, bekam %d", dockerOnly.Total)
	}
	// Und die Summary darf den Fund ebenfalls nicht mehr zählen.
	if n := after.Summary[domain.SeverityCritical]; n != 0 {
		t.Errorf("summary zählt den ignorierten fund noch: %d", n)
	}
}

// TestRemainingVulnsSentenceNamesItsScope: Nach einem Paket-Update meldete LCM
// z.B. "0 kritische, 0 hohe Sicherheitslücken verbleibend" - fachlich richtig
// (Docker zählt für Ampel und Alarme bewusst nicht mit), als Abschlusssatz
// aber irreführend, weil er seinen Bezugsrahmen nicht nannte und damit 23
// kritische Container-Funde verschwieg (BUG-022).
func TestRemainingVulnsSentenceNamesItsScope(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}

	// Ohne Docker-Funde: schlichter Satz, aber mit benanntem Bezug.
	plain := services.RemainingVulnsSentenceForTest(repo, server, 0, 0)
	if !strings.Contains(plain, "Betriebssystem") {
		t.Errorf("der Bezugsrahmen fehlt: %q", plain)
	}
	if strings.Contains(plain, "Container-Images") {
		t.Errorf("ohne Docker-Funde soll kein Container-Zusatz erscheinen: %q", plain)
	}

	// Mit kritischen Docker-Funden werden sie ausdrücklich genannt.
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-1", Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, ImageRef: "nginx:1"},
		{CVEID: "CVE-2", Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, ImageRef: "nginx:1"},
		{CVEID: "CVE-3", Severity: domain.SeverityHigh, Source: domain.VulnSourceDocker, ImageRef: "nginx:1"},
	}); err != nil {
		t.Fatal(err)
	}
	withDocker := services.RemainingVulnsSentenceForTest(repo, server, 0, 0)
	if !strings.Contains(withDocker, "2 kritische") || !strings.Contains(withDocker, "Container-Images") {
		t.Errorf("kritische Docker-Funde werden verschwiegen: %q", withDocker)
	}
}

// TestCVEScanUeberspringtUnerreichbare: ein Scan über die zuletzt gespeicherte
// Paketliste eines nicht erreichbaren Servers schriebe last_cve_scan_at auf
// „jetzt" - eine tagesaktuelle Prüfzeit für einen Host, den niemand erreicht
// hat (R2-017). Solche Server werden übersprungen und im Protokoll benannt;
// ihr alter Stand bleibt unangetastet.
func TestCVEScanUeberspringtUnerreichbare(t *testing.T) {
	env := newTestEnv(t)
	erreichbar := joinTestServer(t, env, "web01")
	route := joinTestServer(t, env, "web02")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", route).
		Update("reachable", false).Error; err != nil {
		t.Fatal(err)
	}

	env.Scanner.ScanFunc = func(trivy.Target) ([]domain.Vulnerability, error) {
		return []domain.Vulnerability{
			{CVEID: "CVE-2026-1", PackageName: "openssl", Severity: domain.SeverityHigh, PkgManager: "apt"},
		}, nil
	}

	// Flottenlauf: der erreichbare wird gescannt, der andere benannt übersprungen.
	env.Executor.RunCVEScan("scheduler")

	var job domain.Job
	env.DB().Where("type = ?", domain.RuleTypeCVEScan).Order("rowid desc").First(&job)
	if !strings.Contains(job.Output, "1 Server gescannt (0 fehlgeschlagen)") {
		t.Errorf("nur der erreichbare Server zählt als gescannt, Output: %q", job.Output)
	}
	if !strings.Contains(job.Output, "web02") || !strings.Contains(job.Output, "nicht erreichbar") {
		t.Errorf("der übersprungene Server muss im Protokoll benannt sein: %q", job.Output)
	}

	repOK, _ := env.Servers.Vulnerabilities(repositories.ScopeAll(), erreichbar)
	if len(repOK.Vulnerabilities) != 1 || repOK.LastScanAt == nil {
		t.Errorf("erreichbarer Server nicht gescannt: %d Funde", len(repOK.Vulnerabilities))
	}
	repWeg, _ := env.Servers.Vulnerabilities(repositories.ScopeAll(), route)
	if len(repWeg.Vulnerabilities) != 0 {
		t.Errorf("unerreichbarer Server darf keinen neuen CVE-Bestand bekommen: %d Funde", len(repWeg.Vulnerabilities))
	}
	if repWeg.LastScanAt != nil {
		t.Errorf("last_cve_scan_at darf für den unerreichbaren Server nicht gesetzt werden: %v", repWeg.LastScanAt)
	}

	// Einzel-Scan: der Job schlägt mit einer Meldung fehl, die den Grund nennt -
	// kein stiller Erfolg über veraltete Daten.
	env.Executor.RunCVEScanServer(route, "admin")
	var single domain.Job
	env.DB().Where("type = ? AND server_id = ?", domain.RuleTypeCVEScan, route).Order("rowid desc").First(&single)
	if single.Status != domain.JobStatusFailed {
		t.Errorf("Einzel-Scan eines unerreichbaren Servers muss fehlschlagen, war %q", single.Status)
	}
	if !strings.Contains(single.Output, "nicht erreichbar") || !strings.Contains(single.Output, "übersprungen") {
		t.Errorf("Fehlermeldung nennt den Grund nicht: %q", single.Output)
	}
}
