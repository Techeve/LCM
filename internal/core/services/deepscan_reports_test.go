package services_test

import (
	"encoding/json"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Deep-Scan-Läufe wurden bis hierher nicht aufbewahrt: jeder Lauf ersetzte
// den Befundbestand. Damit ließ sich die eine Frage nicht beantworten, die
// bei einem Sicherheits-Audit zählt - habe ich seit dem letzten Mal etwas
// erreicht? Ein Befund sah nach dem zweiten Lauf genauso aus wie nach dem
// ersten, ob er nun neu war oder seit Wochen offen.

// saveRun ist die Kurzform „ein Lauf mit diesen Befunden".
func saveRun(t *testing.T, repo *repositories.ServerRepository, serverID uint, titles ...string) *domain.DeepScanReport {
	t.Helper()
	findings := make([]domain.DeepScanFinding, 0, len(titles))
	for _, title := range titles {
		findings = append(findings, domain.DeepScanFinding{
			Category: domain.DeepScanMisconfig, Severity: domain.DeepScanWarning, Tool: "lynis", Title: title,
		})
	}
	rep := &domain.DeepScanReport{ServerID: serverID, Tools: "lynis"}
	if err := repo.SaveDeepScanReport(rep, findings); err != nil {
		t.Fatalf("lauf speichern: %v", err)
	}
	return rep
}

// TestErsterLaufKenntKeineNeuenBefunde: ohne Vorgänger ist nichts „neu" -
// „12 neue Befunde" beim allerersten Scan wäre eine erfundene Aussage.
func TestErsterLaufKenntKeineNeuenBefunde(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	rep := saveRun(t, repo, id, "Befund A", "Befund B")
	if rep.NewFindings != 0 || rep.ResolvedFindings != 0 {
		t.Errorf("erster Lauf: erwartet 0 neu / 0 behoben, bekam %d/%d", rep.NewFindings, rep.ResolvedFindings)
	}
	if rep.Warnings != 2 {
		t.Errorf("Warnungen falsch gezählt: %d", rep.Warnings)
	}
	detail, err := repo.FindDeepScanReport(id, rep.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range detail.Findings {
		if f.IsNew {
			t.Errorf("Befund %q ist im ersten Lauf als neu markiert", f.Title)
		}
	}
}

// TestZweiterLaufWeistFortschrittAus: das ist der Kern - was ist dazugekommen,
// was ist verschwunden, und WAS genau war es.
func TestZweiterLaufWeistFortschrittAus(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	saveRun(t, repo, id, "Bleibt offen", "Wird behoben")
	second := saveRun(t, repo, id, "Bleibt offen", "Kommt neu dazu")

	if second.NewFindings != 1 {
		t.Errorf("erwartet 1 neuer Befund, bekam %d", second.NewFindings)
	}
	if second.ResolvedFindings != 1 {
		t.Errorf("erwartet 1 behobener Befund, bekam %d", second.ResolvedFindings)
	}
	var resolved []string
	if err := json.Unmarshal([]byte(second.ResolvedTitles), &resolved); err != nil {
		t.Fatalf("behobene Titel nicht lesbar: %v", err)
	}
	if len(resolved) != 1 || resolved[0] != "Wird behoben" {
		t.Errorf("die behobenen Titel benennen die Sache nicht: %v", resolved)
	}

	detail, err := repo.FindDeepScanReport(id, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range detail.Findings {
		want := f.Title == "Kommt neu dazu"
		if f.IsNew != want {
			t.Errorf("Befund %q: is_new=%v, erwartet %v", f.Title, f.IsNew, want)
		}
	}
}

// TestJuengsterLaufIstDerAktuelleStand: Ampel, Insights und Alarme lesen
// weiterhin „die Befunde des Servers" - das muss der letzte Lauf sein, nicht
// die Summe aller Läufe.
func TestJuengsterLaufIstDerAktuelleStand(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	saveRun(t, repo, id, "Alt 1", "Alt 2", "Alt 3")
	saveRun(t, repo, id, "Nur noch dieser")

	current, err := repo.FindDeepScanFindings(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].Title != "Nur noch dieser" {
		t.Errorf("aktueller Stand ist nicht der jüngste Lauf: %d Befunde, %v", len(current), current)
	}
}

// TestHistorieBleibtErhalten: die Läufe stehen einzeln und datiert nebeneinander,
// neueste zuerst - sonst gäbe es nichts zu vergleichen.
func TestHistorieBleibtErhalten(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	saveRun(t, repo, id, "Lauf 1")
	saveRun(t, repo, id, "Lauf 2")
	saveRun(t, repo, id, "Lauf 3")

	reports, err := repo.FindDeepScanReports(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 3 {
		t.Fatalf("erwartet 3 Läufe in der Historie, bekam %d", len(reports))
	}
	for i := 1; i < len(reports); i++ {
		if reports[i-1].CreatedAt.Before(reports[i].CreatedAt) {
			t.Error("die Historie ist nicht neueste-zuerst sortiert")
		}
	}
}

// TestFremderReportLiefertKeineBefunde: die Report-ID kommt aus der URL. Ohne
// Prüfung gegen den Server wären die Befunde eines fremden Servers über eine
// erratene ID lesbar.
func TestFremderReportLiefertKeineBefunde(t *testing.T) {
	env := newTestEnv(t)
	a := joinTestServer(t, env, "web01")
	b := joinTestServer(t, env, "web02")
	repo := repositories.NewServerRepository(env.DB())

	repA := saveRun(t, repo, a, "Geheimer Befund von A")

	if _, err := repo.FindDeepScanReport(b, repA.ID); err == nil {
		t.Error("der Bericht von Server A war über Server B abrufbar")
	}
}

// TestHistorieWirdBegrenzt: bei täglichem Scan wüchse die Befundtabelle sonst
// unbegrenzt; die Bereinigung behält die jüngsten Läufe und räumt deren
// Befunde mit ab.
func TestHistorieWirdBegrenzt(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	for i := 0; i < 5; i++ {
		saveRun(t, repo, id, "Dauerbefund")
	}
	if _, err := repo.CleanupDeepScanReports(2); err != nil {
		t.Fatal(err)
	}
	reports, err := repo.FindDeepScanReports(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Errorf("erwartet 2 verbliebene Läufe, bekam %d", len(reports))
	}
	// Und die Befunde der ausgemusterten Läufe dürfen nicht zurückbleiben.
	var orphans int64
	env.DB().Model(&domain.DeepScanFinding{}).
		Where("report_id NOT IN (?)", env.DB().Model(&domain.DeepScanReport{}).Select("id")).
		Count(&orphans)
	if orphans != 0 {
		t.Errorf("%d verwaiste Befunde nach der Bereinigung", orphans)
	}
}
