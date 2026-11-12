package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// scriptRuleOn legt auf einem frischen Server eine Skript-Regel an.
func scriptRuleOn(t *testing.T, env *testEnv, cmd string) (uint, *domain.Rule) {
	t.Helper()
	serverID := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, _ := env.Groups.DefineSchedule(scope, group.ID, "Nacht", "0 3 * * *", 0, "admin")
	rule, err := env.Groups.DefineRule(scope, group.ID, "Skript", domain.RuleTypeScript, cmd, &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return serverID, rule
}

// TestScriptRuleRunsAsRoot: Die Oberfläche verspricht für Skripte Root -
// früher lief das Kommando als Service-User ohne sudo.
func TestScriptRuleRunsAsRoot(t *testing.T) {
	env := newTestEnv(t)
	_, rule := scriptRuleOn(t, env, "touch /etc/lcm-skript")
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, services.ActorScheduler)

	for _, c := range env.Dialer.Commands {
		if strings.Contains(c, "touch /etc/lcm-skript") {
			if !strings.HasPrefix(c, "sudo sh -c ") {
				t.Fatalf("skript läuft nicht als root:\n%s", c)
			}
			return
		}
	}
	t.Fatalf("skript nicht ausgeführt:\n%s", strings.Join(env.Dialer.Commands, "\n"))
}

// TestScriptRuleRestrictedSkippedWithReason: Im eingeschränkten Modus gibt es
// keine Root-Shell - das Skript läuft nicht, und der Job sagt warum.
func TestScriptRuleRestrictedSkippedWithReason(t *testing.T) {
	env := newTestEnv(t)
	serverID, rule := scriptRuleOn(t, env, "touch /etc/lcm-skript")
	setServerFields(t, env, serverID, map[string]any{"restricted_sudo": true})
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, services.ActorScheduler)

	if all := strings.Join(env.Dialer.Commands, "\n"); strings.Contains(all, "touch /etc/lcm-skript") {
		t.Fatalf("skript im eingeschränkten modus ausgeführt:\n%s", all)
	}
	job := latestJob(t, env, serverID, domain.RuleTypeScript)
	if !strings.Contains(job.Output, "übersprungen") || !strings.Contains(job.Output, "Root-Shell") {
		t.Fatalf("job nennt den grund nicht: %q", job.Output)
	}
}
