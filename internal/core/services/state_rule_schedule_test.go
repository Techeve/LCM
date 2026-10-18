package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestPermSyncRunsOnSchedule: „Rechte-Soll halten" lässt sich an einen
// Zeitplan hängen und läuft dort als eigener Job - früher nur als
// Grundsatz-Regel erlaubt, am Zeitplan endete sie mit „kein kommando".
func TestPermSyncRunsOnSchedule(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(scope, group.ID, "Nacht", "0 3 * * *", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{domain.RuleTypePermSync, domain.RuleTypeACLSetup} {
		rule, err := env.Groups.DefineRule(scope, group.ID, typ, typ, "", &sched.ID, false, "admin")
		if err != nil {
			t.Fatalf("%s am zeitplan abgelehnt: %v", typ, err)
		}
		env.Executor.RunRule(rule, "admin")
		job := waitServerJob(t, env, serverID, typ)
		if strings.Contains(job.Output, "kein kommando") {
			t.Errorf("%s ohne ausführungspfad: %q", typ, job.Output)
		}
	}
}
