package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// rebootIfNeededRule legt Server, Gruppe, Zeitplan und die Bedarfs-Regel an.
func rebootIfNeededRule(t *testing.T, env *testEnv) (uint, *domain.Rule) {
	t.Helper()
	serverID := joinTestServer(t, env, "web01")
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Wartungsfenster", "0 3 * * 0", "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Neustart bei Bedarf",
		domain.RuleTypeRebootIfNeeded, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("regel definieren: %v", err)
	}
	return serverID, rule
}

// TestRebootIfNeededSkipsWhenNotRequired ist der Sinn der Regel: Ein Neustart
// ohne Anlass kostet jedes Mal eine Auszeit. Meldet das System keinen Bedarf,
// wird nichts umgelegt - und der Job sagt, warum.
func TestRebootIfNeededSkipsWhenNotRequired(t *testing.T) {
	env := newTestEnv(t)
	serverID, rule := rebootIfNeededRule(t, env)

	// Das System fordert keinen Neustart an.
	env.Dialer.Responses = map[string]sshx.FakeResponse{"reboot-required": {Output: "no\n"}}
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if strings.Contains(all, "reboot") && strings.Contains(all, "nohup") {
		t.Errorf("ohne Bedarf darf kein Neustart abgesetzt werden:\n%s", all)
	}
	done := waitServerJob(t, env, serverID, domain.RuleTypeRebootIfNeeded)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("der übersprungene Lauf ist kein Fehler: %+v", done)
	}
	if !strings.Contains(done.Output, "Kein Neustart nötig") {
		t.Errorf("die Begründung fehlt im Protokoll:\n%s", done.Output)
	}
}

// TestRebootIfNeededRebootsWhenRequired: Meldet das System Bedarf, verhält
// sich die Regel wie der planmäßige Neustart.
func TestRebootIfNeededRebootsWhenRequired(t *testing.T) {
	env := newTestEnv(t)
	serverID, rule := rebootIfNeededRule(t, env)

	env.Dialer.Responses = map[string]sshx.FakeResponse{"reboot-required": {Output: "yes\n"}}
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "nohup") || !strings.Contains(all, "reboot") {
		t.Errorf("bei gemeldetem Bedarf muss neu gestartet werden:\n%s", all)
	}
	done := waitServerJob(t, env, serverID, domain.RuleTypeRebootIfNeeded)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
}
