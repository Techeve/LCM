package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// aptProxyTestGroup legt Server + Gruppe an und konfiguriert die Cache-URL.
func aptProxyTestGroup(t *testing.T, env *testEnv) (serverID uint, groupID uint) {
	t.Helper()
	serverID = joinTestServer(t, env, "web01")
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}
	return serverID, group.ID
}

// TestAptProxyScheduledRule: die geplante Gruppen-Regel bindet die Server an
// den Cache an (Drop-in + apt-Update) und setzt das Server-Feld.
func TestAptProxyScheduledRule(t *testing.T) {
	env := newTestEnv(t)
	serverID, groupID := aptProxyTestGroup(t, env)
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), groupID, "nächtlich", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "apt-cache", domain.RuleTypeAptProxy, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("apt-proxy-rule definieren: %v", err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, `Acquire::http::Proxy "http://cache.local:3142";`) ||
		!strings.Contains(all, "02lcm-apt-cache") {
		t.Errorf("anbindungs-skript fehlt:\n%s", all)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), serverID)
	if !server.AptProxyActive {
		t.Error("apt_proxy_active nach regel-lauf nicht gesetzt")
	}
}

// TestAptProxyEnforceRule: als Grundsatz-Regel wird beim Health-Ping der
// Ist-Zustand geprüft - nur bei Abweichung wird neu angebunden.
func TestAptProxyEnforceRule(t *testing.T) {
	env := newTestEnv(t)
	serverID, groupID := aptProxyTestGroup(t, env)
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "apt-cache-pflicht", domain.RuleTypeAptProxy, "", nil, true, "admin")
	if err != nil {
		t.Fatalf("enforce-rule definieren: %v", err)
	}

	// Health-Rule anlegen (Enforce läuft beim Health-Ping mit).
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), groupID, "ping", "*/15 * * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	health, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "ping", domain.RuleTypeHealth, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Fall 1: Drop-in fehlt → Abweichung → Anbindungs-Skript läuft.
	env.Dialer.Responses["cat /etc/apt/apt.conf.d/02lcm-apt-cache"] = sshx.FakeResponse{Output: ""}
	env.Dialer.Commands = nil
	env.Executor.RunRule(health, "admin")
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "printf") || !strings.Contains(all, "02lcm-apt-cache") {
		t.Errorf("drift nicht durchgesetzt:\n%s", all)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), serverID)
	if !server.AptProxyActive {
		t.Error("apt_proxy_active nach durchsetzung nicht gesetzt")
	}

	// Fall 2: Drop-in zeigt bereits auf die aktuelle URL → kein Eingriff.
	env.Dialer.Responses["cat /etc/apt/apt.conf.d/02lcm-apt-cache"] = sshx.FakeResponse{
		Output: "Acquire::http::Proxy \"http://cache.local:3142\";\nAcquire::https::Proxy \"http://cache.local:3142\";\n",
	}
	env.Dialer.Commands = nil
	env.Executor.RunRule(health, "admin")
	for _, cmd := range env.Dialer.Commands {
		if strings.Contains(cmd, "printf") && strings.Contains(cmd, "02lcm-apt-cache") {
			t.Errorf("regel hat trotz konformem zustand neu angebunden: %s", cmd)
		}
	}
	_ = rule
}

// TestAptProxyRuleValidation: apt-proxy ist als Grundsatz- UND als geplante
// Regel zulässig; andere Typen bleiben als Grundsatz-Regel verboten.
func TestAptProxyRuleValidation(t *testing.T) {
	env := newTestEnv(t)
	_, groupID := aptProxyTestGroup(t, env)

	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "pflicht", domain.RuleTypeAptProxy, "", nil, true, "admin"); err != nil {
		t.Errorf("apt-proxy als grundsatz-regel abgelehnt: %v", err)
	}
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "update-pflicht", domain.RuleTypeUpdate, "", nil, true, "admin"); !errors.Is(err, services.ErrEnforceRuleType) {
		t.Errorf("update als grundsatz-regel nicht abgelehnt: %v", err)
	}
}
