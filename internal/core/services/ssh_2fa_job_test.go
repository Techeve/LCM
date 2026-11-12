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

// TestSSH2FAAbbruchRolltZurueck: Scheitert das Einrichten mittendrin (auf
// RHEL 9 an `sshd -t`), war der Passwort-Stack schon stillgelegt und das
// Drop-in lag da. Ohne Rückweg nahm der laufende sshd danach jedes Passwort,
// und beim nächsten Start wäre er wegen des Drop-ins ausgeblieben.
func TestSSH2FAAbbruchRolltZurueck(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	env.Dialer.Responses["AuthenticationMethods publickey,keyboard-interactive:pam"] = sshx.FakeResponse{
		Output: "AuthenticationMethods cannot be satisfied by enabled authentication methods", ExitCode: 255,
	}
	server := joinPlainServer(t, env, "tfa-rhel")
	env.Dialer.Reset()

	if _, err := env.Servers.ConfigureSSH2FA(repositories.ScopeAll(), server.ID, true, "admin"); err != nil {
		t.Fatalf("ssh-2fa starten: %v", err)
	}
	done := waitServerJob(t, env, server.ID, domain.RuleTypeScript)
	if done.Status == domain.JobStatusSuccess || !strings.Contains(done.Output, "ROLLBACK") {
		t.Fatalf("abbruch ohne rückweg: %s\n%s", done.Status, done.Output)
	}
	commands, _ := env.Dialer.Recorded()
	rolledBack := false
	for _, cmd := range commands {
		rolledBack = rolledBack || strings.Contains(cmd, "rm -f /etc/ssh/sshd_config.d/40-lcm-2fa.conf /etc/ssh/sshd_config.d/55-lcm-2fa.conf")
	}
	if !rolledBack {
		t.Error("drop-in und pam wurden nicht zurückgerollt")
	}
}
