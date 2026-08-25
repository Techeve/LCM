package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// advisoryAlertEnv rüstet die Alarm-Umgebung mit einer festen Befundliste aus.
func advisoryAlertEnv(t *testing.T, findings ...domain.AdvisoryFinding) (*alertEnv, *domain.Server) {
	t.Helper()
	env := newAlertEnv(t)
	server := env.createServer(t, "web01", nil)
	env.alerts.WithAdvisoryFindings(func(uint) ([]domain.AdvisoryFinding, error) {
		return findings, nil
	})
	return env, server
}

// advisoryRule legt eine aktive Frühwarn-Regel mit Mindest-Schwere an.
func advisoryRule(t *testing.T, env *alertEnv, minSeverity string) {
	t.Helper()
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Frühwarnung", Type: domain.AlertTypeAdvisory, Enabled: true,
		MinSeverity: minSeverity,
	}, "admin"); err != nil {
		t.Fatalf("Regel anlegen: %v", err)
	}
}

// TestAdvisoryAlertFeuertAbSchwelle: Ein Befund oberhalb der Schwelle löst
// aus, einer darunter nicht.
func TestAdvisoryAlertFeuertAbSchwelle(t *testing.T) {
	env, _ := advisoryAlertEnv(t, domain.AdvisoryFinding{
		AdvisoryID: "CVE-2026-1", Kind: domain.AdvisoryKindVulnerability,
		PackageName: "openssl", Severity: domain.SeverityCritical,
	})
	advisoryRule(t, env, domain.SeverityHigh)

	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !strings.Contains(summary, "1 alarm(e) ausgelöst") {
		t.Errorf("Alarm sollte auslösen: %q", summary)
	}
}

// TestAdvisoryAlertSchweigtUnterhalbDerSchwelle prüft die Gegenrichtung.
func TestAdvisoryAlertSchweigtUnterhalbDerSchwelle(t *testing.T) {
	env, _ := advisoryAlertEnv(t, domain.AdvisoryFinding{
		AdvisoryID: "CVE-2026-2", Kind: domain.AdvisoryKindVulnerability,
		PackageName: "bash", Severity: domain.SeverityLow,
	})
	advisoryRule(t, env, domain.SeverityHigh)

	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !strings.Contains(summary, "0 alarm(e) ausgelöst") {
		t.Errorf("unterhalb der Schwelle darf nichts auslösen: %q", summary)
	}
}

// TestAdvisoryAlertSchadpaketIgnoriertSchwelle ist der Kern der Sonderregel:
// Ein Schadpaket OHNE jede Schwere-Angabe muss auch bei der strengsten
// Schwelle auslösen. Ohne diese Regel rutschte der schlimmste Fall - Schadcode
// auf dem Server - daran vorbei, dass die Quelle ein Feld nicht füllt.
func TestAdvisoryAlertSchadpaketIgnoriertSchwelle(t *testing.T) {
	env, _ := advisoryAlertEnv(t, domain.AdvisoryFinding{
		AdvisoryID: "MAL-2026-42", Kind: domain.AdvisoryKindMalware,
		PackageName: "leftpad", Severity: "", // bewusst leer
	})
	advisoryRule(t, env, domain.SeverityCritical)

	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !strings.Contains(summary, "1 alarm(e) ausgelöst") {
		t.Fatalf("Schadpaket muss unabhängig von der Schwelle auslösen: %q", summary)
	}
	events, err := env.alerts.ListEvents(10)
	if err != nil || len(events) == 0 {
		t.Fatalf("Event fehlt: %v", err)
	}
	// Der Text muss die Handlung benennen: Ein Schadpaket ist kein
	// Update-Thema, es gehört vom Server.
	if !strings.Contains(events[0].Description, "boesartige") {
		t.Errorf("Meldung benennt das Schadpaket nicht: %q", events[0].Description)
	}
}

// TestAdvisoryAlertIgnoriertBestaetigte: Ein zur Kenntnis genommener Befund
// löst keinen Alarm mehr aus - sonst bliebe bei einem dauerhaft nicht
// behebbaren Befund nur, die ganze Regel abzuschalten.
func TestAdvisoryAlertIgnoriertBestaetigte(t *testing.T) {
	env, _ := advisoryAlertEnv(t, domain.AdvisoryFinding{
		AdvisoryID: "MAL-2026-42", Kind: domain.AdvisoryKindMalware,
		PackageName: "leftpad", AcknowledgedBy: "admin",
	})
	advisoryRule(t, env, domain.SeverityHigh)

	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !strings.Contains(summary, "0 alarm(e) ausgelöst") {
		t.Errorf("bestätigter Befund darf nicht auslösen: %q", summary)
	}
}

// TestAdvisoryAlertOhneVerdrahtungStill: Ist die Frühwarnung nicht
// verdrahtet (schlanke Instanz), feuert der Typ nie - statt zu paniken.
func TestAdvisoryAlertOhneVerdrahtungStill(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web01", nil)
	advisoryRule(t, env, domain.SeverityHigh)

	summary, err := env.alerts.Evaluate("test")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !strings.Contains(summary, "0 alarm(e) ausgelöst") {
		t.Errorf("ohne Verdrahtung darf nichts auslösen: %q", summary)
	}
}
