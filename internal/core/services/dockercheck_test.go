package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/registry"
	"LCM/internal/storage/repositories"
)

// TestDockerCheckUpdatesImages: der zentrale Update-Check schreibt das
// Registry-Ergebnis auf alle Server-Zeilen einer Referenz (dedupliziert:
// ein Registry-Call pro Referenz), markiert private Images als "nicht
// prüfbar" und lässt lokale Images unangetastet.
func TestDockerCheckUpdatesImages(t *testing.T) {
	env := newTestEnv(t)
	a := joinTestServer(t, env, "web01")
	b := joinTestServer(t, env, "web02")
	repo := repositories.NewServerRepository(env.DB())

	// nginx:1.25 läuft auf BEIDEN Servern (Server b hat bereits den neuen
	// Stand), dazu ein privates und ein lokal gebautes Image.
	seed := func(id uint, digest string, extra ...domain.DockerImage) {
		imgs := append([]domain.DockerImage{
			{Repository: "nginx", Tag: "1.25", ImageID: "sha256:x", RepoDigest: digest, InUse: true},
		}, extra...)
		if err := repo.ReplaceDockerImages(id, imgs); err != nil {
			t.Fatal(err)
		}
	}
	seed(a, "sha256:alt",
		domain.DockerImage{Repository: "acme/privat", Tag: "1.0", RepoDigest: "sha256:p", InUse: true},
		domain.DockerImage{Repository: "meinapp", Tag: "dev", CheckStatus: domain.DockerCheckLocal, InUse: true},
	)
	seed(b, "sha256:neu")

	env.Registry.Results = map[string]registry.Result{
		"nginx:1.25":      {Digest: "sha256:neu", Status: domain.DockerCheckOK},
		"acme/privat:1.0": {Status: domain.DockerCheckUnauthorized, Err: "privates image - anonym nicht prüfbar"},
	}

	env.Executor.RunDockerCheck("test")

	// Dedup: eine Referenz = genau ein Registry-Call, egal wie viele Server.
	if n := env.Registry.Calls("nginx:1.25"); n != 1 {
		t.Errorf("nginx:1.25 sollte genau 1x geprüft werden, wurde %dx", n)
	}

	imgsA, _ := repo.FindDockerImages(a)
	byRef := map[string]domain.DockerImage{}
	for _, img := range imgsA {
		byRef[img.Ref()] = img
	}
	if img := byRef["nginx:1.25"]; !img.UpdateAvailable || img.CandidateDigest != "sha256:neu" || img.CheckStatus != domain.DockerCheckOK {
		t.Errorf("nginx auf web01 sollte ein update haben: %+v", img)
	}
	if img := byRef["acme/privat:1.0"]; img.CheckStatus != domain.DockerCheckUnauthorized || img.UpdateAvailable {
		t.Errorf("privates image sollte 'unauthorized' sein: %+v", img)
	}
	if img := byRef["meinapp:dev"]; img.CheckStatus != domain.DockerCheckLocal || img.LastCheckedAt != nil {
		t.Errorf("lokales image sollte unangetastet bleiben: %+v", img)
	}

	// Server b hat den neuen Digest bereits → kein Update.
	imgsB, _ := repo.FindDockerImages(b)
	if len(imgsB) != 1 || imgsB[0].UpdateAvailable || imgsB[0].CheckStatus != domain.DockerCheckOK {
		t.Errorf("web02 ist aktuell - kein update erwartet: %+v", imgsB)
	}

	var job domain.Job
	if err := env.DB().Where("type = ?", domain.RuleTypeDockerCheck).Order("rowid desc").First(&job).Error; err != nil {
		t.Fatalf("kein docker-check-job: %v", err)
	}
	if job.Status != domain.JobStatusSuccess {
		t.Errorf("job sollte erfolgreich sein, war %q (output: %s)", job.Status, job.Output)
	}
	if !strings.Contains(job.Output, "2 Image-Tags geprüft") || !strings.Contains(job.Output, "1 mit verfügbarem Update") ||
		!strings.Contains(job.Output, "1 nicht prüfbar") || !strings.Contains(job.Output, "1 lokale") {
		t.Errorf("zusammenfassung unvollständig: %q", job.Output)
	}
}

// TestDockerCheckRuleRunsOnce: als Rule des System-Sync-Schedules läuft der
// zentrale Docker-Check EINMAL pro Schedule-Lauf - nicht pro Server (die
// Server werden dafür gar nicht kontaktiert).
func TestDockerCheckRuleRunsOnce(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	joinTestServer(t, env, "web02")

	var rule domain.Rule
	if err := env.DB().Where("type = ?", domain.RuleTypeDockerCheck).First(&rule).Error; err != nil {
		t.Fatalf("docker-check-rule nicht geseedet: %v", err)
	}
	env.Executor.RunRule(&rule, "test")

	var n int64
	env.DB().Model(&domain.Job{}).Where("type = ?", domain.RuleTypeDockerCheck).Count(&n)
	if n != 1 {
		t.Errorf("erwartet genau 1 docker-check-job (zentraler lauf), gefunden: %d", n)
	}
	var job domain.Job
	env.DB().Where("type = ?", domain.RuleTypeDockerCheck).First(&job)
	if job.ServerID != nil {
		t.Errorf("docker-check-job darf an keinem server hängen: %+v", job)
	}
}

// TestDockerCheckScansImagesWithTrivy: der Docker-Check scannt genutzte
// Images dedupliziert mit Trivy, verteilt die Funde auf alle betroffenen
// Server (Quelle "docker") und lässt OS-Funde unangetastet. In die Ampel
// zählen Docker-CVEs nur für als relevant markierte Container.
func TestDockerCheckScansImagesWithTrivy(t *testing.T) {
	env := newTestEnv(t)
	a := joinTestServer(t, env, "web01")
	b := joinTestServer(t, env, "web02")
	repo := repositories.NewServerRepository(env.DB())

	// Bestehender OS-Fund auf Server a - darf der Docker-Scan nicht löschen.
	if err := repo.ReplaceVulnerabilities(a, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-OS-1", PackageName: "openssl", Severity: domain.SeverityMedium, PkgManager: "apt"},
	}); err != nil {
		t.Fatal(err)
	}

	// nginx:1.25 (gleicher Digest) läuft auf beiden Servern; dazu ein
	// ungenutztes Image, das NICHT gescannt werden darf.
	for _, id := range []uint{a, b} {
		if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
			{Repository: "nginx", Tag: "1.25", RepoDigest: "sha256:alt", InUse: true},
			{Repository: "redis", Tag: "7", RepoDigest: "sha256:r", InUse: false},
		}); err != nil {
			t.Fatal(err)
		}
	}
	env.Registry.Results = map[string]registry.Result{
		"nginx:1.25": {Digest: "sha256:alt", Status: domain.DockerCheckOK},
	}
	env.Scanner.ScanImageFunc = func(ref string) ([]domain.Vulnerability, error) {
		return []domain.Vulnerability{
			{CVEID: "CVE-2025-0001", PackageName: "libssl3", Severity: domain.SeverityCritical, FixedVersion: "3.0.9"},
		}, nil
	}

	env.Executor.RunDockerCheck("test")

	// Dedup: ein Trivy-Scan für beide Server, adressiert per repo@digest
	// (exakt der Stand, der auf den Servern läuft - nicht der aktuelle
	// Registry-Stand des Tags); ungenutztes redis nicht gescannt.
	if len(env.Scanner.ImageCalls) != 1 || env.Scanner.ImageCalls[0] != "nginx@sha256:alt" {
		t.Errorf("erwartete genau 1 image-scan für nginx@sha256:alt, bekam %v", env.Scanner.ImageCalls)
	}

	// Beide Server tragen den Docker-Fund; der OS-Fund von a bleibt.
	vulnsA, _ := repo.FindVulnerabilities(a)
	if len(vulnsA) != 2 {
		t.Fatalf("web01: erwartet 2 funde (os+docker), bekam %d: %+v", len(vulnsA), vulnsA)
	}
	var haveOS, haveDocker bool
	for _, v := range vulnsA {
		if v.Source == domain.VulnSourceOS && v.CVEID == "CVE-OS-1" {
			haveOS = true
		}
		if v.Source == domain.VulnSourceDocker && v.ImageRef == "nginx:1.25" {
			haveDocker = true
		}
	}
	if !haveOS || !haveDocker {
		t.Errorf("quellen-trennung verletzt: %+v", vulnsA)
	}
	vulnsB, _ := repo.FindVulnerabilities(b)
	if len(vulnsB) != 1 || vulnsB[0].Source != domain.VulnSourceDocker {
		t.Errorf("web02: erwartet 1 docker-fund, bekam %+v", vulnsB)
	}

	// Docker-CVEs zählen standardmäßig NICHT in die Ampel: der kritische
	// Image-Fund eskaliert NICHT auf Rot (der Testserver ist wegen des
	// überfälligen Paket-Updates aus dem Join-Scan ohnehin gelb).
	status, _, _, err := env.Servers.Status(repositories.ScopeAll(), b)
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.ServerStatusYellow {
		t.Errorf("nicht-relevante image-cve sollte nicht eskalieren (gelb wegen paket-updates), bekam %q", status)
	}

	// Erst ein als CVE-relevant markierter Container macht die Funde seines
	// Images zählbar - kritisch zählt dann mit voller Schwere → Rot.
	if err := repo.ReplaceDockerContainers(b, []domain.DockerContainer{
		{ContainerID: "c1", Name: "web", Image: "nginx:1.25", State: "running"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.SetContainerCVERelevance(repositories.ScopeAll(), b, "web", true, "admin"); err != nil {
		t.Fatal(err)
	}
	status, _, _, err = env.Servers.Status(repositories.ScopeAll(), b)
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.ServerStatusRed {
		t.Errorf("kritische image-cve eines relevanten containers sollte rot ergeben, bekam %q", status)
	}

	// Zweiter Lauf ohne Funde räumt die Docker-Quelle, OS bleibt.
	env.Scanner.ScanImageFunc = func(string) ([]domain.Vulnerability, error) { return nil, nil }
	env.Executor.RunDockerCheck("test")
	vulnsA, _ = repo.FindVulnerabilities(a)
	if len(vulnsA) != 1 || vulnsA[0].Source != domain.VulnSourceOS {
		t.Errorf("nach leerem lauf sollte nur der os-fund bleiben: %+v", vulnsA)
	}
}

// TestDockerCheckKeepsFindingsOnScanError: schlägt der Trivy-Scan eines
// Images fehl, behalten die betroffenen Server ihren bisherigen
// Docker-CVE-Bestand (kein Flackern).
func TestDockerCheckKeepsFindingsOnScanError(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
		{Repository: "nginx", Tag: "1.25", RepoDigest: "sha256:alt", InUse: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-ALT", PackageName: "zlib", Severity: domain.SeverityHigh, ImageRef: "nginx:1.25"},
	}); err != nil {
		t.Fatal(err)
	}
	env.Registry.Results = map[string]registry.Result{
		"nginx:1.25": {Digest: "sha256:alt", Status: domain.DockerCheckOK},
	}
	env.Scanner.ScanImageFunc = func(string) ([]domain.Vulnerability, error) {
		return nil, errors.New("registry timeout")
	}

	env.Executor.RunDockerCheck("test")

	vulns, _ := repo.FindVulnerabilities(id)
	if len(vulns) != 1 || vulns[0].CVEID != "CVE-ALT" {
		t.Errorf("bestand sollte bei scan-fehler erhalten bleiben: %+v", vulns)
	}
	var job domain.Job
	env.DB().Where("type = ?", domain.RuleTypeDockerCheck).Order("rowid desc").First(&job)
	if !strings.Contains(job.Output, "1 fehlgeschlagen") {
		t.Errorf("fehlgeschlagener scan sollte im protokoll stehen: %q", job.Output)
	}
}

// TestTrafficLightOutdatedContainerImage: veraltetes genutztes Image → Gelb.
func TestTrafficLightOutdatedContainerImage(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
		{Repository: "nginx", Tag: "1.25", RepoDigest: "sha256:alt", InUse: true},
	}); err != nil {
		t.Fatal(err)
	}
	env.Registry.Results = map[string]registry.Result{
		"nginx:1.25": {Digest: "sha256:neu", Status: domain.DockerCheckOK},
	}
	env.Executor.RunDockerCheck("test")

	status, insights, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.ServerStatusYellow {
		t.Errorf("veraltetes container-image sollte gelb ergeben, bekam %q", status)
	}
	found := false
	for _, in := range insights {
		if strings.Contains(in.Message, "Container-Image") {
			found = true
		}
	}
	if !found {
		t.Errorf("container-image-insight fehlt: %+v", insights)
	}
}

// TestDockerCheckSkipsDemoServers: Demo-Inventar wird nie gegen echte
// Registries geprüft.
func TestDockerCheckSkipsDemoServers(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("is_demo", true)
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
		{Repository: "nginx", Tag: "1.25", RepoDigest: "sha256:alt", InUse: true},
	}); err != nil {
		t.Fatal(err)
	}

	env.Executor.RunDockerCheck("test")

	if n := env.Registry.Calls("nginx:1.25"); n != 0 {
		t.Errorf("demo-server dürfen nicht geprüft werden, %d calls", n)
	}
}
