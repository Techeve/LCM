package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestUngenutzteImagesZaehlenNicht ist der Kern dieser Regel: Ein Fund aus
// einem Image, das kein Container verwendet, wird angezeigt - aber er
// verschiebt die Bewertung nicht. Ein Image, das nirgends läuft, hat keine
// Angriffsfläche; mitgezählt würde es die Zahlen nach oben treiben, ohne
// dass jemand etwas tun könnte oder müsste.
func TestUngenutzteImagesZaehlenNicht(t *testing.T) {
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")
	repo := repositories.NewServerRepository(env.DB())

	images, err := repo.FindDockerImages(id)
	if err != nil || len(images) < 2 {
		t.Fatalf("Testdaten brauchen mindestens zwei Images: %v / %d", err, len(images))
	}
	genutzt, ungenutzt := images[0].Ref(), images[1].Ref()
	if err := repo.ReplaceDockerImages(id, []domain.DockerImage{
		{Repository: images[0].Repository, Tag: images[0].Tag, InUse: true},
		{Repository: images[1].Repository, Tag: images[1].Tag, InUse: false},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-GENUTZT", PackageName: "libssl3", Severity: domain.SeverityCritical, ImageRef: genutzt},
		{CVEID: "CVE-UNGENUTZT", PackageName: "libssl3", Severity: domain.SeverityCritical, ImageRef: ungenutzt},
	}); err != nil {
		t.Fatal(err)
	}

	report, err := env.Servers.Vulnerabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}

	// Beide Funde sind SICHTBAR - verschwiegen wird nichts.
	if len(report.Vulnerabilities) != 2 {
		t.Fatalf("beide Funde müssen sichtbar bleiben, waren %d", len(report.Vulnerabilities))
	}
	// Aber nur der genutzte zählt.
	if report.Summary[domain.SeverityCritical] != 1 {
		t.Errorf("erwartet 1 bewerteten kritischen Fund, war %d", report.Summary[domain.SeverityCritical])
	}
	if report.UnusedSummary[domain.SeverityCritical] != 1 {
		t.Errorf("erwartet 1 ausgenommenen kritischen Fund, war %d", report.UnusedSummary[domain.SeverityCritical])
	}
	for _, v := range report.Vulnerabilities {
		if v.CVEID == "CVE-UNGENUTZT" && !v.ImageUnused {
			t.Error("Fund aus ungenutztem Image ist nicht markiert")
		}
		if v.CVEID == "CVE-GENUTZT" && v.ImageUnused {
			t.Error("Fund aus genutztem Image fälschlich als ungenutzt markiert")
		}
	}
}

// TestBetriebssystemFundeBleibenBewertet: Die Ausnahme gilt NUR für
// Docker-Funde. Ein Paket des Betriebssystems ist immer installiert.
func TestBetriebssystemFundeBleibenBewertet(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceOS, []domain.Vulnerability{
		{CVEID: "CVE-OS", PackageName: "openssl", Severity: domain.SeverityHigh},
	}); err != nil {
		t.Fatal(err)
	}
	report, err := env.Servers.Vulnerabilities(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary[domain.SeverityHigh] != 1 {
		t.Errorf("System-Fund muss zählen, Zusammenfassung: %+v", report.Summary)
	}
	if len(report.UnusedSummary) != 0 {
		t.Errorf("ein System-Fund kann nicht aus einem ungenutzten Image stammen: %+v", report.UnusedSummary)
	}
	if report.Vulnerabilities[0].ImageUnused {
		t.Error("System-Fund darf nicht als ungenutzt markiert sein")
	}
}
