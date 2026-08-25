package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// lcmHostServer legt einen Server an und macht ihn zum LCM-Host: LCM erkennt
// den eigenen Rechner an Loopback-Adresse und Standard-SSH-Port.
func lcmHostServer(t *testing.T, env *testEnv) uint {
	t.Helper()
	id := joinTestServer(t, env, "lcm-host")
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.UpdateFields(id, map[string]any{"host": "localhost"}); err != nil {
		t.Fatalf("host auf localhost setzen: %v", err)
	}
	return id
}

// TestSelfUpdateCompletesInterruptedJob deckt den gemeldeten Fall ab: Wer über
// den LCM-Host Pakete aktualisiert, aktualisiert damit LCM selbst - der Dienst
// startet neu und verliert seinen eigenen Job. Beim nächsten Start wurde
// dieser Job als Fehler gemeldet, obwohl er durchgelaufen ist.
func TestSelfUpdateCompletesInterruptedJob(t *testing.T) {
	env := newTestEnv(t)
	serverID := lcmHostServer(t, env)

	job, err := env.Jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Alle Pakete aktualisieren", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	// Neustart in einer neuen Version - genau der Sonderfall.
	repo := repositories.NewServerRepository(env.DB())
	self := services.SelfUpdateOnRestart(repo, "1.15.0", "1.15.1")
	if self == nil {
		t.Fatal("Versionswechsel auf dem LCM-Host sollte erkannt werden")
	}
	env.Jobs.FailInterruptedOnStartup(self)

	var reloaded domain.Job
	if err := env.DB().First(&reloaded, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	if reloaded.Status != domain.JobStatusSuccess {
		t.Errorf("das eigene Update sollte als erfolgreich abgeschlossen werden, ist %q", reloaded.Status)
	}
	if reloaded.ExitCode == nil || *reloaded.ExitCode != 0 {
		t.Errorf("exit code = %v, erwartet 0", reloaded.ExitCode)
	}
	if reloaded.FinishedAt == nil {
		t.Error("abgeschlossener job hat kein finished_at")
	}
	// Die Begründung gehört ins Protokoll: Ohne sie sähe der Eintrag aus wie
	// ein Lauf, der einfach nichts ausgegeben hat.
	if !strings.Contains(reloaded.Output, "1.15.0 → 1.15.1") {
		t.Errorf("Protokoll nennt den Versionswechsel nicht: %q", reloaded.Output)
	}

	// Die Server-Sperre ist frei - auch der Sonderfall muss sie freigeben.
	if _, err := env.Jobs.Start(&serverID, nil, domain.RuleTypeUpdate, "Nächstes Update", "admin"); err != nil {
		t.Fatalf("nach recovery sollte ein neuer job starten: %v", err)
	}
}

// TestSelfUpdateLeavesOtherJobsFailed grenzt den Sonderfall ab. Er darf nur
// für Paket-Jobs auf dem LCM-Host gelten: Ein unterbrochener Lauf auf einem
// anderen Server hat sein Ziel NICHT nachweislich erreicht - ihn als
// erfolgreich zu melden, wäre eine Lüge über einen fremden Rechner.
func TestSelfUpdateLeavesOtherJobsFailed(t *testing.T) {
	env := newTestEnv(t)
	lcmHostServer(t, env)
	otherID := joinTestServer(t, env, "web01")

	onOther, err := env.Jobs.Start(&otherID, nil, domain.RuleTypeUpdate, "Update @ web01", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	repo := repositories.NewServerRepository(env.DB())
	env.Jobs.FailInterruptedOnStartup(services.SelfUpdateOnRestart(repo, "1.15.0", "1.15.1"))

	var reloaded domain.Job
	if err := env.DB().First(&reloaded, "id = ?", onOther.ID).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	if reloaded.Status != domain.JobStatusFailed {
		t.Errorf("job auf einem anderen server sollte failed bleiben, ist %q", reloaded.Status)
	}
}

// TestSelfUpdateOnlyForPackageJobs: Auch auf dem LCM-Host zählt nur, was
// Pakete einspielt. Ein Firewall-Lauf, den der Neustart erwischt hat, ist
// weiterhin ein Fehler - er hat mit dem Update nichts zu tun.
func TestSelfUpdateOnlyForPackageJobs(t *testing.T) {
	env := newTestEnv(t)
	serverID := lcmHostServer(t, env)

	job, err := env.Jobs.Start(&serverID, nil, domain.RuleTypeFirewall, "Firewall anwenden", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	repo := repositories.NewServerRepository(env.DB())
	env.Jobs.FailInterruptedOnStartup(services.SelfUpdateOnRestart(repo, "1.15.0", "1.15.1"))

	var reloaded domain.Job
	if err := env.DB().First(&reloaded, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("job laden: %v", err)
	}
	if reloaded.Status != domain.JobStatusFailed {
		t.Errorf("firewall-job sollte failed bleiben, ist %q", reloaded.Status)
	}
}

// TestSelfUpdateOnRestartOnlyOnVersionChange: Ohne Versionswechsel gibt es den
// Sonderfall nicht. Ein Neustart aus anderem Grund (Absturz, manueller
// Restart) lässt den unterbrochenen Job weiterhin als Fehler stehen - dort
// ist ja auch tatsächlich nichts fertig geworden.
func TestSelfUpdateOnRestartOnlyOnVersionChange(t *testing.T) {
	env := newTestEnv(t)
	lcmHostServer(t, env)
	repo := repositories.NewServerRepository(env.DB())

	if self := services.SelfUpdateOnRestart(repo, "1.15.1", "1.15.1"); self != nil {
		t.Error("gleiche Version ist kein Selbst-Update")
	}
	if self := services.SelfUpdateOnRestart(repo, "", "1.15.1"); self != nil {
		t.Error("Erstinstallation ohne Vorversion ist kein Selbst-Update")
	}
}

// TestSelfUpdateWithoutLcmHost: Steht der eigene Rechner nicht in der
// Verwaltung, gibt es keinen Job, der zuzuordnen wäre - der Regelfall gilt.
func TestSelfUpdateWithoutLcmHost(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if self := services.SelfUpdateOnRestart(repo, "1.15.0", "1.15.1"); self != nil {
		t.Error("ohne LCM-Host in der Verwaltung gibt es keinen Sonderfall")
	}
}
