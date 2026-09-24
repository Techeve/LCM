package services_test

import (
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// TestCleanupShortensRoutineLogs: Health-Check-Ausgaben verschwinden nach
// der kurzen Routine-Frist, ihre Einträge bleiben. Andere Protokolle und
// frische Health-Checks bleiben unberührt, und ein zweiter Lauf fasst die
// schon gekürzten Einträge nicht erneut an.
func TestCleanupShortensRoutineLogs(t *testing.T) {
	env := newTestEnv(t)
	old := time.Now().AddDate(0, 0, -10)
	fresh := time.Now().AddDate(0, 0, -2)

	oldHealth := routineTestJob(t, env, domain.RuleTypeHealth, old)
	freshHealth := routineTestJob(t, env, domain.RuleTypeHealth, fresh)
	oldUpdate := routineTestJob(t, env, domain.RuleTypeUpdate, old)
	healthSession := routineTestSession(t, env, domain.HealthCheckPurpose, old)
	syncSession := routineTestSession(t, env, "sync", old)

	env.Executor.RunCleanup("test")

	if got := jobOutput(t, env, oldHealth); got != domain.RoutineOutputRemoved {
		t.Errorf("alter health-check: ausgabe %q, erwartet den hinweis", got)
	}
	for _, id := range []string{freshHealth, oldUpdate} {
		if got := jobOutput(t, env, id); got != "ausgabe" {
			t.Errorf("job %s: ausgabe %q, erwartet unverändert", id, got)
		}
	}
	if n := commandCount(t, env, healthSession); n != 0 {
		t.Errorf("health-check-sitzung hat noch %d kommandos", n)
	}
	if n := commandCount(t, env, syncSession); n != 1 {
		t.Errorf("sync-sitzung hat %d kommandos, erwartet 1", n)
	}

	env.Executor.RunCleanup("test")
	if report := latestCleanupReport(t, env); !strings.Contains(report, "0 job-ausgaben") {
		t.Errorf("zweiter lauf hat erneut gekürzt: %s", report)
	}
}

func TestRoutineRetentionRejectsZero(t *testing.T) {
	svc, _ := newSettingsService(t)
	in := mailInput(func(in *services.GlobalSettingsInput) { in.RoutineLogRetentionDays = ip(0) })
	if _, err := svc.UpdateGlobal(in, "test"); err == nil {
		t.Fatal("0 tage für routine-protokolle wurde angenommen")
	}
}

func routineTestJob(t *testing.T, env *testEnv, jobType string, created time.Time) string {
	t.Helper()
	job := &domain.Job{Type: jobType, Name: jobType, Status: domain.JobStatusSuccess, Output: "ausgabe", CreatedAt: created}
	if err := env.db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	return job.ID
}

func routineTestSession(t *testing.T, env *testEnv, purpose string, created time.Time) string {
	t.Helper()
	sess := &domain.SSHSession{Purpose: purpose, OpenedAt: created, CreatedAt: created, CommandCount: 1,
		Commands: []domain.SSHCommand{{Seq: 1, Command: "nft list ruleset", Output: "table inet filter {}"}}}
	if err := env.db.Create(sess).Error; err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

func jobOutput(t *testing.T, env *testEnv, id string) string {
	t.Helper()
	var job domain.Job
	if err := env.db.First(&job, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return job.Output
}

func commandCount(t *testing.T, env *testEnv, sessionID string) int64 {
	t.Helper()
	var n int64
	env.db.Model(&domain.SSHCommand{}).Where("ssh_session_id = ?", sessionID).Count(&n)
	return n
}

func latestCleanupReport(t *testing.T, env *testEnv) string {
	t.Helper()
	var job domain.Job
	if err := env.db.Where("type = ?", domain.RuleTypeCleanup).Order("created_at DESC").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job.Output
}
