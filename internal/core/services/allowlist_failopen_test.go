package services_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Tests zum Fail-open-Block R2-083/R2-071/R2-075/R2-072: leere oder nicht
// auflösbare Quell-Einschränkungen dürfen weder Ports still öffnen noch
// still schließen - und eine referenzierte Allowlist ist nicht löschbar.

// TestLeereSourceIPsWirdAbgewiesen (R2-083): eine AUSDRÜCKLICH leere
// source_ips-Liste ohne Allowlist öffnete den Port für jeden (fail-open).
// Jetzt: 400 statt „ALLOW Anywhere".
func TestLeereSourceIPsWirdAbgewiesen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "fw-fo01")

	_, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true,
		`[{"port":8080,"proto":"tcp","ip_version":"any","source_ips":[]}]`, domain.FirewallSSHSources{}, "admin")
	if !errors.Is(err, services.ErrInvalidFirewallRules) {
		t.Fatalf("leere source_ips muss abgewiesen werden, bekam: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "8080") {
		t.Errorf("die Meldung soll den Port benennen: %v", err)
	}

	// Feld WEGGELASSEN = bewusst ohne Einschränkung - bleibt zulässig.
	if _, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true,
		`[{"port":8080,"proto":"tcp","ip_version":"any"}]`, domain.FirewallSSHSources{}, "admin"); err != nil {
		t.Errorf("Regel ohne source_ips-Feld muss weiter möglich sein: %v", err)
	}
}

// TestUnbekannteAllowlistWirdAbgewiesen (R2-071 Fall 2, Konfigurationszeit):
// eine Regel mit toter Allowlist-ID lief mit „success" durch und der Port
// verschwand wortlos. Jetzt: 400 mit Nennung der ID.
func TestUnbekannteAllowlistWirdAbgewiesen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "fw-fo02")

	_, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true,
		`[{"port":8080,"proto":"tcp","allowlist_ids":[4711]}]`, domain.FirewallSSHSources{}, "admin")
	if !errors.Is(err, services.ErrInvalidFirewallRules) || !strings.Contains(err.Error(), "4711") {
		t.Fatalf("tote Allowlist-ID muss mit Nennung abgewiesen werden, bekam: %v", err)
	}
}

// TestLeereAllowlistWirdAbgewiesen (R2-071 Fall 1, Konfigurationszeit): eine
// existierende, aber leere Allowlist ließ die Regel still verschwinden.
func TestLeereAllowlistWirdAbgewiesen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "fw-fo03")
	empty, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "leer"}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	_, err = env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true,
		fmt.Sprintf(`[{"port":9090,"proto":"tcp","allowlist_ids":[%d]}]`, empty.ID), domain.FirewallSSHSources{}, "admin")
	if !errors.Is(err, services.ErrInvalidFirewallRules) || !strings.Contains(err.Error(), "GESCHLOSSEN") {
		t.Fatalf("leere Allowlist muss zur Konfigurationszeit abgewiesen werden, bekam: %v", err)
	}
}

// TestReferenzierteAllowlistNichtLoeschbar (R2-072): das Löschen einer
// referenzierten Liste war der Auslöser des zeitversetzten, unbemerkten
// Portausfalls. Jetzt: Löschsperre mit Nennung der Verweise.
func TestReferenzierteAllowlistNichtLoeschbar(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "fw-fo04")
	// Fake-Zielsystem: ufw vorhanden und nach dem Anwenden aktiv.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}
	list, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "admin-netz", Entries: "192.168.10.0/24"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true,
		fmt.Sprintf(`[{"port":8080,"proto":"tcp","allowlist_ids":[%d]}]`, list.ID), domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatalf("firewall anwenden: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("firewall-job fehlgeschlagen: %s", done.Output)
	}

	err = env.Settings.DeleteIPAllowlist(list.ID, "admin")
	if !errors.Is(err, services.ErrAllowlistInUse) {
		t.Fatalf("referenzierte Liste muss die Löschsperre treffen, bekam: %v", err)
	}
	if !strings.Contains(err.Error(), "fw-fo04") || !strings.Contains(err.Error(), "8080") {
		t.Errorf("die Sperre soll Server und Port benennen: %v", err)
	}

	// Referenz lösen → Löschen möglich.
	job2, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true,
		`[{"port":8080,"proto":"tcp"}]`, domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, env, job2.ID)
	if err := env.Settings.DeleteIPAllowlist(list.ID, "admin"); err != nil {
		t.Errorf("ohne Verweise muss Löschen gehen: %v", err)
	}
}
