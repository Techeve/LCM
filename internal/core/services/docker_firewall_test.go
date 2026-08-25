package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestDockerPortsQualifyFirewallStatus deckt BUG-023 ab. Der Befund selbst ist
// Docker-Absicht: das DNAT liegt in nat/PREROUTING, also vor der INPUT-Kette,
// in der ufw filtert. Falsch war allein LCMs Aussage "Firewall aktiv, nur
// 22/443 offen" für einen Host, dessen Container-Ports von außen erreichbar
// sind. LCM greift NICHT ein - es benennt den Zustand.
func TestDockerPortsQualifyFirewallStatus(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if err := repo.UpdateFields(id, map[string]any{"firewall_active": true, "has_docker": true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceDockerContainers(id, []domain.DockerContainer{
		{ContainerID: "a1", Name: "uptime-kuma", Image: "louislam/uptime-kuma:1", State: "running",
			Ports: "0.0.0.0:3001->3001/tcp, :::3001->3001/tcp"},
		{ContainerID: "b2", Name: "intern-db", Image: "postgres:16", State: "running",
			Ports: "127.0.0.1:5432->5432/tcp"},
		{ContainerID: "c3", Name: "gestoppt", Image: "nginx:1", State: "exited",
			Ports: "0.0.0.0:8080->80/tcp"},
	}); err != nil {
		t.Fatal(err)
	}

	// Nur der laufende, extern gebundene Container taucht auf.
	exposures, err := env.Servers.DockerPortExposures(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(exposures) != 2 { // 3001 auf IPv4 und IPv6
		t.Fatalf("erwartete 2 externe Bindungen, bekam %d: %+v", len(exposures), exposures)
	}
	for _, e := range exposures {
		if e.Container != "uptime-kuma" || e.HostPort != "3001" {
			t.Errorf("unerwartete Bindung: %+v", e)
		}
	}

	// Der Status nennt Port UND Container - und bleibt dabei ein Info-Befund,
	// der die Ampelfarbe nicht verschlechtert (sonst wäre jeder Docker-Host
	// dauerhaft gelb und das Signal wertlos).
	status, insights, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	var found *domain.StatusInsight
	for i := range insights {
		if strings.Contains(insights[i].Message, "Docker-Port") {
			found = &insights[i]
		}
	}
	if found == nil {
		t.Fatalf("die Firewall-Aussage wird nicht qualifiziert: %+v", insights)
	}
	if found.Severity != "info" {
		t.Errorf("Severity = %q, erwartet \"info\" (darf die Ampel nicht färben)", found.Severity)
	}
	if !strings.Contains(found.Message, "3001") || !strings.Contains(found.Message, "uptime-kuma") {
		t.Errorf("Port oder Container fehlen in der Meldung: %q", found.Message)
	}
	if !strings.Contains(found.Message, "nicht verändert") {
		t.Errorf("es soll klar sein, dass LCM nicht eingreift: %q", found.Message)
	}
	if status == domain.ServerStatusRed {
		t.Error("ein veröffentlichter Docker-Port darf den Server nicht rot machen")
	}

	// Ohne aktive Firewall gibt es keine falsche Zusage - also auch keinen Hinweis.
	if err := repo.UpdateFields(id, map[string]any{"firewall_active": false}); err != nil {
		t.Fatal(err)
	}
	_, insights2, _, _ := env.Servers.Status(repositories.ScopeAll(), id)
	for _, in := range insights2 {
		if strings.Contains(in.Message, "Docker-Port") {
			t.Error("ohne aktive Firewall behauptet LCM nichts - der Hinweis erübrigt sich")
		}
	}
}

// TestExternallyReachableContainersCountForCVEs: LCM gewichtet CVEs von
// Paketen, die auf erreichbaren Ports lauschen, längst eine Stufe hoch - für
// Container fehlte diese Logik, obwohl die Frage dort schärfer ist:
// Docker-Funde zählen sonst GAR NICHT, außer jemand hakt den Container von
// Hand an. Ausgerechnet die von außen erreichbaren Container blieben damit
// unbewertet. Sie zählen jetzt automatisch mit.
func TestExternallyReachableContainersCountForCVEs(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if err := repo.UpdateFields(id, map[string]any{"has_docker": true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceDockerContainers(id, []domain.DockerContainer{
		{ContainerID: "a1", Name: "public-web", Image: "nginx:1.25", State: "running",
			Ports: "0.0.0.0:8080->80/tcp"},
		{ContainerID: "b2", Name: "intern-db", Image: "postgres:16", State: "running",
			Ports: "127.0.0.1:5432->5432/tcp"},
	}); err != nil {
		t.Fatal(err)
	}
	// Je ein kritischer Fund pro Image - keiner der Container ist von Hand markiert.
	if err := repo.ReplaceVulnerabilities(id, domain.VulnSourceDocker, []domain.Vulnerability{
		{CVEID: "CVE-PUBLIC", Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, ImageRef: "nginx:1.25", FixedVersion: "1.0"},
		{CVEID: "CVE-INTERN", Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, ImageRef: "postgres:16", FixedVersion: "1.0"},
	}); err != nil {
		t.Fatal(err)
	}

	status, insights, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	// Der erreichbare Container schlägt durch - der nur lokal gebundene nicht.
	if status != domain.ServerStatusRed {
		t.Errorf("ein kritischer CVE im extern erreichbaren Container muss zählen, Status = %q", status)
	}
	var cve string
	for _, in := range insights {
		if strings.Contains(in.Message, "kritische Sicherheitslücke") {
			cve = in.Message
		}
	}
	if cve == "" {
		t.Fatalf("kein CVE-Befund in den Insights: %+v", insights)
	}
	if !strings.HasPrefix(cve, "1 ") {
		t.Errorf("erwartet genau 1 gewerteter Fund (nur der erreichbare Container), bekam: %q", cve)
	}
}
