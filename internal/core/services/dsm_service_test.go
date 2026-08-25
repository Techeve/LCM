package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// hasDSMInsight sucht eine Teilzeichenkette in den Ampel-Hinweisen (das
// Pendant zu hasInsight im internen Testpaket).
func hasDSMInsight(insights []domain.StatusInsight, substr string) bool {
	for _, i := range insights {
		if strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}

// TestDSMGeraeteTypSperrtShellAktionen (Synology DSM): DSM ist ein reiner
// API-Gerätetyp - jede Aktion, die eine POSIX-Shell oder Paketverwaltung
// voraussetzt, muss mit einer benennenden Meldung abgewiesen werden statt in
// einen kryptischen Fehler zu laufen.
func TestDSMGeraeteTypSperrtShellAktionen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "nas01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Updates(map[string]any{"os_id": domain.OSIDSynologyDSM, "os_name": "Synology DSM"}).Error; err != nil {
		t.Fatal(err)
	}
	scope := repositories.ScopeAll()

	if _, err := env.Servers.ConfigureFirewall(scope, id, true, "22", domain.FirewallSSHSources{}, "admin"); !errors.Is(err, services.ErrDSMUnsupported) {
		t.Errorf("Firewall: erwartete ErrDSMUnsupported, got %v", err)
	}
	if _, err := env.Servers.HardenSSH(scope, id, "admin"); !errors.Is(err, services.ErrDSMUnsupported) {
		t.Errorf("HardenSSH: erwartete ErrDSMUnsupported, got %v", err)
	}
	if _, err := env.Servers.SetTimezone(scope, id, "Europe/Berlin", "admin"); !errors.Is(err, services.ErrDSMUnsupported) {
		t.Errorf("SetTimezone: erwartete ErrDSMUnsupported, got %v", err)
	}
	if _, err := env.Servers.RestrictSudo(scope, id, "admin"); !errors.Is(err, services.ErrDSMUnsupported) {
		t.Errorf("RestrictSudo: erwartete ErrDSMUnsupported, got %v", err)
	}
}

// TestDSMAmpelBewertetUpdateUndAdvisor: ohne Paket-/CVE-Sicht sind die
// DSM-Aktualität und die Befunde des DSM-Security-Advisors die tragenden
// Kriterien der Ampel.
func TestDSMAmpelBewertetUpdateUndAdvisor(t *testing.T) {
	base := domain.Server{
		Reachable: true, OSID: domain.OSIDSynologyDSM, OSName: "Synology DSM",
		OSVersion: "DSM 7.3.2-86009",
	}

	aktuell := base
	status, insights := aktuell.TrafficLight(domain.TrafficLightInput{})
	if status == domain.ServerStatusYellow {
		t.Errorf("aktuelles DSM ohne Befunde darf nicht gelb sein: %+v", insights)
	}

	veraltet := base
	veraltet.DSMUpdateAvailable = true
	veraltet.DSMLatestVersion = "DSM 7.3.3"
	_, insights = veraltet.TrafficLight(domain.TrafficLightInput{})
	if !hasDSMInsight(insights, "DSM 7.3.3") {
		t.Errorf("verfügbares DSM-Update wird nicht mit Version gemeldet: %+v", insights)
	}

	unsicher := base
	unsicher.DSMSecurityRisks = 3
	_, insights = unsicher.TrafficLight(domain.TrafficLightInput{})
	if !hasDSMInsight(insights, "Security-Advisors") {
		t.Errorf("Advisor-Befunde fehlen in der Ampel: %+v", insights)
	}
}
