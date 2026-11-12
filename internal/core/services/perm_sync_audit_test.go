package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestPermSyncAuditsOnlyNewDrift: Eine Abweichung, die LCM nicht korrigieren
// kann (eingeschränkter Modus) oder nur meldet, stand bei jedem Lauf erneut im
// Audit. Jetzt nur beim ersten Mal, bei jeder Änderung und bei jedem echten
// Eingriff - der Job nennt sie trotzdem jedes Mal.
func TestPermSyncAuditsOnlyNewDrift(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	group, _ := env.Groups.Create("Web", "", nil, "admin")
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, _ := env.Groups.DefineSchedule(scope, group.ID, "Nacht", "0 3 * * *", 0, "admin")
	rule, err := env.Groups.DefineRule(scope, group.ID, "Rechte", domain.RuleTypePermSync, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	hardened := &domain.HardenedPath{ServerID: serverID, Path: "/srv/daten", Mode: "750", Group: "root"}
	if err := repositories.NewServerRepository(env.DB()).SaveHardenedPath(hardened); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name, mode, fix string
		audits          int64
	}{
		{"erste abweichung", "770", "fehler", 1},
		{"dieselbe noch einmal", "770", "fehler", 1},
		{"abweichung ändert sich", "777", "fehler", 2},
		{"geänderte noch einmal", "777", "fehler", 2},
		{"echter eingriff", "777", "ok", 3},
		{"eingriff noch einmal", "777", "ok", 4},
	}
	for _, s := range steps {
		env.Dialer.Responses["aclField() {"] = sshx.FakeResponse{Output: "LCM-IST|0|" + s.mode + "|root||\n"}
		env.Dialer.Responses["LCM-FIX|"] = sshx.FakeResponse{Output: "LCM-FIX|0|" + s.fix + "\n"}
		env.Executor.RunRule(rule, "admin")

		job := waitServerJob(t, env, serverID, domain.RuleTypePermSync)
		if !strings.Contains(job.Output, "/srv/daten") {
			t.Errorf("%s: job nennt die abweichung nicht: %q", s.name, job.Output)
		}
		var audits int64
		env.DB().Model(&domain.AuditLog{}).Where("action = ?", "rule.enforce").Count(&audits)
		if audits != s.audits {
			t.Fatalf("%s: %d audit-einträge, erwartet %d", s.name, audits, s.audits)
		}
	}
}
