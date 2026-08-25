package services

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestWeightedSeverity prüft die CVE-Gewichtung zählender Funde: gelistete/
// lauschende Pakete eine Stufe rauf, Grenzen werden nicht verlassen; zählende
// Docker-Funde (relevanter Container) behalten ihre volle Schwere.
func TestWeightedSeverity(t *testing.T) {
	weightList := []string{"nginx", "postgresql"}
	listening := []string{"openssh-server"}

	cases := []struct {
		name string
		fact repositories.VulnFact
		want string
	}{
		// Zählende Docker-Funde: volle Schwere (keine Senkung mehr - nicht
		// relevante Docker-Funde zählen gar nicht, siehe vulnCounts).
		{"docker critical bleibt critical", repositories.VulnFact{Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, PackageName: "libssl3"}, domain.SeverityCritical},
		{"docker high bleibt high", repositories.VulnFact{Severity: domain.SeverityHigh, Source: domain.VulnSourceDocker, PackageName: "libssl3"}, domain.SeverityHigh},
		// Hochgewichtungs-Liste: eine Stufe rauf.
		{"os nginx high → critical", repositories.VulnFact{Severity: domain.SeverityHigh, Source: domain.VulnSourceOS, PackageName: "nginx"}, domain.SeverityCritical},
		{"os nginx critical bleibt critical (Obergrenze)", repositories.VulnFact{Severity: domain.SeverityCritical, Source: domain.VulnSourceOS, PackageName: "nginx"}, domain.SeverityCritical},
		// Präfix-Match: postgresql-14 zählt wie postgresql.
		{"prefix postgresql-14", repositories.VulnFact{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "postgresql-14"}, domain.SeverityHigh},
		// Liste gilt auch im Container.
		{"docker nginx high → critical", repositories.VulnFact{Severity: domain.SeverityHigh, Source: domain.VulnSourceDocker, PackageName: "nginx"}, domain.SeverityCritical},
		// Lauschende Dienste: nur OS-Quelle.
		{"os sshd lauscht → rauf", repositories.VulnFact{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "openssh-server"}, domain.SeverityHigh},
		{"docker openssh lauscht NICHT (Container)", repositories.VulnFact{Severity: domain.SeverityMedium, Source: domain.VulnSourceDocker, PackageName: "openssh-server"}, domain.SeverityMedium},
		// Unlistet + OS: unverändert.
		{"os curl medium bleibt", repositories.VulnFact{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "curl"}, domain.SeverityMedium},
		// Unbekannte Schwere bleibt unbekannt.
		{"unknown bleibt unknown", repositories.VulnFact{Severity: "??", Source: domain.VulnSourceOS, PackageName: "nginx"}, domain.SeverityUnknown},
	}
	for _, c := range cases {
		if got := weightedSeverity(c.fact, weightList, listening); got != c.want {
			t.Errorf("%s: erwartet %q, bekam %q", c.name, c.want, got)
		}
	}
}

// TestWeightedVulnSummaryDockerRelevance: Docker-Funde zählen nur, wenn ihr
// Image zu einem als relevant markierten Container gehört; countedVulns folgt
// derselben Regel (Basis des „Sehr gut"-Kriteriums).
func TestWeightedVulnSummaryDockerRelevance(t *testing.T) {
	facts := []repositories.VulnFact{
		{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "curl", FixedVersion: "8.1"},
		{Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, PackageName: "libssl3", ImageRef: "nginx:1.25", FixedVersion: "3.0.9"},
		{Severity: domain.SeverityHigh, Source: domain.VulnSourceDocker, PackageName: "zlib1g", ImageRef: "redis:7", FixedVersion: "1.3"},
	}

	// Ohne relevante Container: nur der OS-Fund zählt.
	sum := weightedVulnSummary(facts, nil, nil, nil)
	if sum[domain.SeverityCritical] != 0 || sum[domain.SeverityHigh] != 0 || sum[domain.SeverityMedium] != 1 {
		t.Errorf("ohne relevanz sollten docker-funde nicht zählen: %v", sum)
	}
	if n := countedVulns(facts, nil); n != 1 {
		t.Errorf("countedVulns ohne relevanz: erwartet 1, bekam %d", n)
	}

	// nginx:1.25 relevant: sein kritischer Fund zählt voll, redis weiter nicht.
	refs := map[string]bool{"nginx:1.25": true}
	sum = weightedVulnSummary(facts, nil, nil, refs)
	if sum[domain.SeverityCritical] != 1 || sum[domain.SeverityHigh] != 0 {
		t.Errorf("relevanter container sollte voll zählen: %v", sum)
	}
	if n := countedVulns(facts, refs); n != 2 {
		t.Errorf("countedVulns mit relevanz: erwartet 2, bekam %d", n)
	}
}

// TestDockerCVEsIgnoredOverridesRelevance: Der Server-Schalter „CVEs aus
// Container-Images ignorieren" sticht jede Einzel-Markierung. Ohne ihn zählte
// der hier markierte Container mit voller Schwere - mit ihm zählt nichts
// mehr. Geprüft wird das an dockerRelevantRefs, weil dort die Entscheidung
// fällt: Ein leeres Ergebnis bedeutet „kein Docker-Fund zählt".
//
// Das Repository wird nicht gebraucht (nil): Der Schalter greift, bevor
// Container und Images überhaupt geladen werden - genau das ist gewollt.
func TestDockerCVEsIgnoredOverridesRelevance(t *testing.T) {
	marked := &domain.Server{CVERelevantContainers: "web,db", DockerCVEsIgnored: true}
	if refs := dockerRelevantRefs(nil, marked); refs != nil {
		t.Errorf("der schalter muss auch markierte container übersteuern, bekam: %v", refs)
	}

	// Gegenprobe: Ohne den Schalter darf er nicht greifen - dann entscheidet
	// wie bisher die Markierung (hier über das Repository, deshalb nur die
	// Abgrenzung des Sonderfalls).
	off := &domain.Server{CVERelevantContainers: "web", DockerCVEsIgnored: false}
	if off.DockerCVEsIgnored {
		t.Fatal("testaufbau falsch")
	}

	// Und die Wirkung auf die Bewertung: ohne zählende Refs bleibt von den
	// Docker-Funden nichts übrig.
	facts := []repositories.VulnFact{
		{Severity: domain.SeverityCritical, Source: domain.VulnSourceDocker, PackageName: "libssl3", ImageRef: "nginx:1.25", FixedVersion: "3.0.9"},
	}
	if sum := weightedVulnSummary(facts, nil, nil, nil); sum[domain.SeverityCritical] != 0 {
		t.Errorf("ignorierte docker-funde dürfen die ampel nicht färben: %v", sum)
	}
}

// TestCVEHighWeightList prüft die Listen-Auflösung der globalen Einstellung.
func TestCVEHighWeightList(t *testing.T) {
	empty := &domain.GlobalSettings{}
	if list := empty.CVEHighWeightList(); len(list) == 0 {
		t.Error("leeres Feld sollte die Standardliste liefern")
	} else if !matchesPackageList("nginx", list) || !matchesPackageList("postgresql-14", list) {
		t.Errorf("Standardliste sollte nginx und postgresql-14 treffen: %v", list)
	}

	disabled := &domain.GlobalSettings{CVEHighWeightPackages: "-"}
	if list := disabled.CVEHighWeightList(); list != nil {
		t.Errorf("\"-\" sollte die Liste deaktivieren, bekam %v", list)
	}

	custom := &domain.GlobalSettings{CVEHighWeightPackages: "Foo, bar\nbaz"}
	list := custom.CVEHighWeightList()
	if len(list) != 3 || !matchesPackageList("FOO-1.2", list) {
		t.Errorf("eigene Liste (normalisiert, Präfix-Match): %v", list)
	}
}

// TestRaisedVulnPackagesErklaertDieAbweichung: Der Fall, der wie ein
// hängengebliebener Status aussieht - die Ampel meldet hohe Funde, unter
// Sicherheit stehen nur mittlere.
//
// Ursache ist die Gewichtung: nginx steht auf der Hochgewichtungs-Liste,
// openssh-server lauscht auf einem erreichbaren Port; beider mittlere Funde
// zählen deshalb als hoch. In der Liste bleibt die Roh-Schwere stehen. Genau
// diese beiden Pakete muss die Erklärung benennen - curl (unbeteiligt) nicht.
func TestRaisedVulnPackagesErklaertDieAbweichung(t *testing.T) {
	weightList := []string{"nginx"}
	listening := []string{"openssh-server"}
	facts := []repositories.VulnFact{
		{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "nginx", FixedVersion: "1.24"},
		{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "openssh-server", FixedVersion: "9.2"},
		{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "curl", FixedVersion: "8.1"},
	}

	// Die Ampel sieht zwei hohe Funde …
	sum := weightedVulnSummary(facts, weightList, listening, nil)
	if sum[domain.SeverityHigh] != 2 {
		t.Fatalf("erwartet 2 hochgewichtete funde, bekam %v", sum)
	}
	// … obwohl kein einziger Fund roh als „hoch" eingetragen ist.
	for _, f := range facts {
		if domain.NormalizeSeverity(f.Severity) == domain.SeverityHigh {
			t.Fatal("testaufbau: kein fund darf roh 'hoch' sein")
		}
	}

	got := raisedVulnPackages(facts, weightList, listening, nil)
	want := []string{"nginx", "openssh-server"}
	if len(got) != len(want) {
		t.Fatalf("erwartet %v, bekam %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("erwartet %v, bekam %v", want, got)
			break
		}
	}
}

// TestRaisedVulnPackagesSchweigtOhneHochstufung: Wo nichts verschoben wird,
// gibt es auch nichts zu erklären - der Hinweis darf nicht bei jedem Server
// mitlaufen.
func TestRaisedVulnPackagesSchweigtOhneHochstufung(t *testing.T) {
	facts := []repositories.VulnFact{
		{Severity: domain.SeverityHigh, Source: domain.VulnSourceOS, PackageName: "curl", FixedVersion: "8.1"},
		{Severity: domain.SeverityCritical, Source: domain.VulnSourceOS, PackageName: "bash", FixedVersion: "5.2"},
	}
	if got := raisedVulnPackages(facts, nil, nil, nil); got != nil {
		t.Errorf("ohne hochstufung erwartet nil, bekam %v", got)
	}
	// Unbehebbares zählt nicht in die Bewertung und darf deshalb auch nicht
	// als Erklärung auftauchen.
	unfixable := []repositories.VulnFact{
		{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "nginx"},
	}
	if got := raisedVulnPackages(unfixable, []string{"nginx"}, nil, nil); got != nil {
		t.Errorf("unbehebbare funde gehören nicht in die erklärung, bekam %v", got)
	}
}
