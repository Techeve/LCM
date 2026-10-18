package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestDNSTestRuleOwnDomains: Eine DNS-Test-Regel kann eigene Domains prüfen;
// ohne bleibt es bei denen aus den Einstellungen. Ein ungültiger Eintrag wird
// abgelehnt statt still verworfen.
func TestDNSTestRuleOwnDomains(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, _ := env.Groups.DefineSchedule(scope, group.ID, "Nacht", "0 3 * * *", 0, "admin")

	if _, err := env.Groups.DefineRule(scope, group.ID, "dns", domain.RuleTypeDNSTest, "intranet.local; rm -rf", &sched.ID, false, "admin"); !errors.Is(err, services.ErrInvalidDNSTestDomain) {
		t.Fatalf("ungültige domain angenommen: %v", err)
	}
	rule, err := env.Groups.DefineRule(scope, group.ID, "dns", domain.RuleTypeDNSTest, "intranet.example, git.example.org", &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Command != "intranet.example\ngit.example.org" {
		t.Errorf("abgelegt als %q", rule.Command)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")
	waitServerJob(t, env, serverID, domain.RuleTypeDNSTest)
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "intranet.example") || strings.Contains(all, "deb.debian.org") {
		t.Errorf("geprüft wurden nicht die eigenen domains:\n%s", all)
	}
}
