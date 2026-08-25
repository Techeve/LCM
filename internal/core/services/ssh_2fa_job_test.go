package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestSSH2FAAktivierenMitAussperrProbe: der komplette Job-Durchlauf inklusive
// der Aussperr-Probe - sie öffnet eine ZWEITE Verbindung, während die
// Job-Verbindung noch offen ist. Regression: die Probe lief zunächst über den
// Exec-Slot des ConnLimiters, den die Job-Verbindung selbst hielt - sie
// schlug damit IMMER fehl und rollte jede Aktivierung zurück (im Langzeittest
// so aufgetreten). Die Probe muss über den Lese-Slot gehen.
func TestSSH2FAAktivierenMitAussperrProbe(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	// google-authenticator ist nach der Installation auffindbar.
	env.Dialer.Responses["command -v google-authenticator"] = sshx.FakeResponse{Output: ""}
	server := joinPlainServer(t, env, "tfasrv")

	job, err := env.Servers.ConfigureSSH2FA(repositories.ScopeAll(), server.ID, true, "admin")
	if err != nil {
		t.Fatalf("ssh-2fa starten: %v", err)
	}
	done := waitServerJob(t, env, server.ID, domain.RuleTypeScript)
	if done.ID != job.ID {
		t.Fatalf("unerwarteter job: %+v", done)
	}
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("ssh-2fa-job nicht erfolgreich: %s - %s", done.Status, done.Output)
	}
	if strings.Contains(done.Output, "ROLLBACK") {
		t.Fatalf("aussperr-probe hat faelschlich zurueckgerollt:\n%s", done.Output)
	}

	// Zustand persistiert.
	fresh, err := env.Servers.Get(repositories.ScopeAll(), server.ID)
	if err != nil || !fresh.SSH2FAEnabled {
		t.Fatalf("ssh_2fa_enabled nicht gesetzt (%v, %+v)", err, fresh.SSH2FAEnabled)
	}

	// Und wieder entfernen.
	job, err = env.Servers.ConfigureSSH2FA(repositories.ScopeAll(), server.ID, false, "admin")
	if err != nil {
		t.Fatalf("ssh-2fa entfernen: %v", err)
	}
	done = waitServerJob(t, env, server.ID, domain.RuleTypeScript)
	if done.ID != job.ID || done.Status != domain.JobStatusSuccess {
		t.Fatalf("entfernen nicht erfolgreich: %+v", done)
	}
	fresh, _ = env.Servers.Get(repositories.ScopeAll(), server.ID)
	if fresh.SSH2FAEnabled {
		t.Fatal("ssh_2fa_enabled nach dem Entfernen noch gesetzt")
	}
}
