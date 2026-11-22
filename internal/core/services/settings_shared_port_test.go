package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
)

func strPtr(s string) *string { return &s }

func TestPublicHostConflictBeiGemeinsamemPort(t *testing.T) {
	svc, _ := newSettingsService(t)
	modeC(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("https://agent.example.org")})

	// Gleicher Name, andere Schreibweise und anderer Port: derselbe Host.
	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{PublicAgentURL: strPtr("https://LCM.example.org:9320")}, "admin")
	var conflict *services.PublicHostConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("Fehler = %v, erwartet *PublicHostConflict", err)
	}
	if conflict.Host != "lcm.example.org" || conflict.First != "UI" || conflict.Second != "Agent" {
		t.Errorf("Konflikt = %+v", conflict)
	}
	if !errors.Is(err, services.ErrSettingInvalid) {
		t.Error("der Konflikt wickelt ErrSettingInvalid nicht ein")
	}
	if got := svc.PublicAgentURL(); got != "https://agent.example.org" {
		t.Errorf("abgelehnte Adresse wurde gespeichert: %q", got)
	}
}

func TestPublicHostConflictOhneGemeinsamenPortErlaubt(t *testing.T) {
	svc, _ := newSettingsService(t)
	if _, err := svc.UpdateGlobal(services.GlobalSettingsInput{
		PublicBaseURL:  strPtr("https://lcm.example.org:9310"),
		PublicAgentURL: strPtr("https://lcm.example.org:9320"),
	}, "admin"); err != nil {
		t.Fatalf("ein Name auf getrennten Ports ohne gemeinsamen Port: %v", err)
	}
}

// Im Modus C zählt der Name von MCP auch bei ausgeschaltetem MCP: Der Name
// entscheidet am gemeinsamen Port, welcher Dienst die Verbindung bekommt.
func TestPublicHostConflictMCPAuchWennAus(t *testing.T) {
	svc, _ := newSettingsService(t)
	modeC(t, svc, services.GlobalSettingsInput{PublicAgentURL: strPtr("https://agent.example.org")})

	_, err := svc.UpdateGlobal(services.GlobalSettingsInput{PublicMCPURL: strPtr("https://lcm.example.org")}, "admin")
	var conflict *services.PublicHostConflict
	if !errors.As(err, &conflict) || conflict.First != "UI" || conflict.Second != "MCP" {
		t.Fatalf("MCP-Adresse mit dem Namen der UI: %v, erwartet Konflikt UI/MCP", err)
	}

	if _, err := svc.UpdateGlobal(services.GlobalSettingsInput{PublicMCPURL: strPtr("https://mcp.example.org")}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMCP(true, "admin"); err != nil {
		t.Errorf("MCP mit eigenem Namen: %v", err)
	}
}

// Ein Stand von früher blockiert das Speichern anderer Felder nicht.
func TestPublicHostConflictAlterStandBlockiertNichtsAnderes(t *testing.T) {
	svc, repo := newSettingsService(t)
	if _, err := svc.UpdateGlobal(services.GlobalSettingsInput{
		PublicBaseURL:  strPtr("https://lcm.example.org"),
		PublicAgentURL: strPtr("https://lcm.example.org:9320"),
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	enableSharedPortDirectly(t, repo) // der gemeinsame Port kommt erst jetzt dazu

	days := 30
	if _, err := svc.UpdateGlobal(services.GlobalSettingsInput{LogRetentionDays: &days}, "admin"); err != nil {
		t.Errorf("anderes Feld: %v", err)
	}
	if _, err := svc.UpdateGlobal(services.GlobalSettingsInput{PublicAgentURL: strPtr("https://lcm.example.org:9320")}, "admin"); err != nil {
		t.Errorf("unveränderte Adresse erneut speichern: %v", err)
	}
	if _, err := svc.UpdateGlobal(services.GlobalSettingsInput{PublicAgentURL: strPtr("https://lcm.example.org:9999")}, "admin"); err == nil {
		t.Error("geänderte Adresse mit demselben Namen wurde akzeptiert")
	}
}
