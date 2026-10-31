package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestRuleOrderDecidesExecution: Neue Regeln reihen sich hinten an; nach dem
// Umsortieren läuft der Zeitplan in der neuen Reihenfolge.
func TestRuleOrderDecidesExecution(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, _ := env.Groups.DefineSchedule(scope, group.ID, "Nacht", "0 3 * * *", 0, "admin")
	var ids []uint
	for i, cmd := range []string{"echo eins", "echo zwei", "echo drei"} {
		rule, err := env.Groups.DefineRule(scope, group.ID, cmd, domain.RuleTypeScript, cmd, &sched.ID, false, "admin")
		if err != nil {
			t.Fatal(err)
		}
		if rule.Position != i {
			t.Errorf("%s: position %d, erwartet %d", cmd, rule.Position, i)
		}
		ids = append(ids, rule.ID)
	}

	// drei, eins, zwei
	if err := env.Groups.ReorderRules(scope, group.ID, []uint{ids[2], ids[0], ids[1]}, "admin"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := repositories.NewGroupRepository(env.DB()).FindSchedule(scope, sched.ID)
	env.Dialer.Commands = nil
	env.Executor.RunSchedule(loaded, services.ActorScheduler)

	all := strings.Join(env.Dialer.Commands, "\n")
	drei, eins, zwei := strings.Index(all, "echo drei"), strings.Index(all, "echo eins"), strings.Index(all, "echo zwei")
	if drei < 0 || !(drei < eins && eins < zwei) {
		t.Fatalf("ausführung nicht in der neuen reihenfolge:\n%s", all)
	}
}

// TestRuleOrderRejectsIncompleteOrMixed: Die Liste muss genau die Regeln
// eines Auslösers nennen.
func TestRuleOrderRejectsIncompleteOrMixed(t *testing.T) {
	env := newTestEnv(t)
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	a, _ := env.Groups.DefineSchedule(scope, group.ID, "A", "0 3 * * *", 0, "admin")
	b, _ := env.Groups.DefineSchedule(scope, group.ID, "B", "0 4 * * *", 0, "admin")
	r1, _ := env.Groups.DefineRule(scope, group.ID, "1", domain.RuleTypeScript, "true", &a.ID, false, "admin")
	r2, _ := env.Groups.DefineRule(scope, group.ID, "2", domain.RuleTypeScript, "true", &a.ID, false, "admin")
	r3, _ := env.Groups.DefineRule(scope, group.ID, "3", domain.RuleTypeScript, "true", &b.ID, false, "admin")

	for name, ids := range map[string][]uint{
		"unvollständig": {r1.ID},
		"gemischt":      {r1.ID, r2.ID, r3.ID},
		"doppelt":       {r1.ID, r1.ID},
		"leer":          {},
		"fremd":         {r1.ID, 99999},
	} {
		if err := env.Groups.ReorderRules(scope, group.ID, ids, "admin"); !errors.Is(err, services.ErrRuleOrderInvalid) {
			t.Errorf("%s: angenommen (%v)", name, err)
		}
	}
}
