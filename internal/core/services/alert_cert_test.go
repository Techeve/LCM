package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// TestAlertTLSCertExpiring: Der Alarm folgt dem Hinweis der Erneuerung -
// einmal und ohne Serverbezug; ohne Hinweis bleibt er still.
func TestAlertTLSCertExpiring(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web-01", nil)
	warning := ""
	env.alerts.WithCertWarning(func() string { return warning })
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Zertifikat", Type: domain.AlertTypeTLSCertExpiring, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := env.alerts.ListEvents(10); len(ev) != 0 {
		t.Fatalf("ohne Hinweis kein Alarm erwartet, bekam %d", len(ev))
	}

	warning = "Noch ohne: agent-07."
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 || events[0].ServerID != nil {
		t.Fatalf("erwartete genau 1 Alarm ohne Serverbezug, bekam %d: %+v", len(events), events)
	}
	if events[0].Type != domain.AlertTypeTLSCertExpiring || !strings.Contains(events[0].Description, "Zertifikat") {
		t.Errorf("Alarm %q / %q", events[0].Type, events[0].Description)
	}
}
