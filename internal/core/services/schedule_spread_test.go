package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestScheduleSpreadMustEndBeforeNextRun: Ein Fenster, das in den nächsten
// Lauf hineinreicht, ließe zwei Läufe sich überholen.
func TestScheduleSpreadMustEndBeforeNextRun(t *testing.T) {
	env := newTestEnv(t)
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	scope := repositories.ScopeAll()
	for _, tc := range []struct {
		cron   string
		spread int
		ok     bool
	}{
		{"0 3 * * *", 120, true},     // täglich, zwei Stunden
		{"0 3 * * *", 1440, false},   // so lang wie der Abstand
		{"*/15 * * * *", 14, true},   // knapp unter dem Takt
		{"*/15 * * * *", 15, false},  // reicht in den nächsten Lauf
		{"0 3 * * 1-5", 1440, false}, // werktags: das Wochenende zählt nicht
		{"0 3 * * *", -1, false},
	} {
		_, err := env.Groups.DefineSchedule(scope, group.ID, "s", tc.cron, tc.spread, "admin")
		if got := err == nil; got != tc.ok {
			t.Errorf("%s / %d min: angenommen = %v, erwartet %v (%v)", tc.cron, tc.spread, got, tc.ok, err)
		}
		if err != nil && !errors.Is(err, services.ErrInvalidSpread) {
			t.Errorf("%s / %d min: falscher fehler %v", tc.cron, tc.spread, err)
		}
	}

	sched, _ := env.Groups.DefineSchedule(scope, group.ID, "s", "0 * * * *", 30, "admin")
	tooLong := 60
	if _, err := env.Groups.UpdateSchedule(scope, sched.ID, "", "", &tooLong, "admin"); !errors.Is(err, services.ErrInvalidSpread) {
		t.Errorf("änderung auf ein zu langes fenster angenommen: %v", err)
	}
}

// TestSpreadRunsAllRulesPerServer: Im Fenster arbeitet ein Server alle
// Regeln des Zeitplans in ihrer Reihenfolge ab.
func TestSpreadRunsAllRulesPerServer(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(scope, group.ID, "Nacht", "0 3 * * *", 60, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"echo erste-regel", "echo zweite-regel"} {
		if _, err := env.Groups.DefineRule(scope, group.ID, cmd, domain.RuleTypeScript, cmd, &sched.ID, false, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := repositories.NewGroupRepository(env.DB()).FindSchedule(scope, sched.ID)
	if err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunSchedule(loaded, services.ActorScheduler)

	all := strings.Join(env.Dialer.Commands, "\n")
	first, second := strings.Index(all, "erste-regel"), strings.Index(all, "zweite-regel")
	if first < 0 || second < first {
		t.Fatalf("regeln nicht in reihenfolge ausgeführt:\n%s", all)
	}
}
