package services_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestSSHLoggingRecordsSessionsForServerAndJob prüft die Kernanforderung:
// Rule-Ausführungen erzeugen Protokoll-Sessions, die sowohl unter dem Server
// als auch beim Job auffindbar und mit den Kommandos gefüllt sind.
func TestSSHLoggingRecordsSessionsForServerAndJob(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Der Join selbst hat bereits Sessions erzeugt (Provisionierung + Scan).
	sessions, err := env.SSHLogs.ServerSessions(repositories.ScopeAll(), id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) < 2 {
		t.Fatalf("erwartete mindestens 2 join-sessions, bekam %d", len(sessions))
	}
	var joinLinked bool
	for _, s := range sessions {
		if strings.HasPrefix(s.Purpose, "join:") {
			joinLinked = true
			if s.JobID == nil {
				t.Error("join-session nicht mit onboarding-job verknüpft")
			}
			if s.CommandCount == 0 {
				t.Error("join-session ohne protokollierte kommandos")
			}
		}
	}
	if !joinLinked {
		t.Error("keine join-session gefunden")
	}
}

// TestSSHLoggingRedactsPasswords stellt sicher, dass ein per Aktivierung
// gesetztes Linux-Passwort NICHT im Klartext im Protokoll landet.
func TestSSHLoggingRedactsPasswords(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}

	// Linux-User mit gesetztem Passwort anlegen und dem Server zuordnen.
	const secret = "supergeheimes-passwort-123"
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deployer", FullName: "Deployer", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := env.LinuxUsers.GenerateActivation(lu.ID, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.LinuxUsers.ConsumeActivation(token, services.LinuxActivationInput{Password: secret}); err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	// Sync erzeugt das chpasswd-Kommando - im Protokoll muss das Passwort weg sein.
	if _, err := env.Prov.SyncUsers(server, "admin"); err != nil {
		t.Fatal(err)
	}

	sessions, err := env.SSHLogs.ServerSessions(repositories.ScopeAll(), id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var checkedChpasswd bool
	for _, s := range sessions {
		full, err := env.SSHLogs.Session(repositories.ScopeAll(), s.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, cmd := range full.Commands {
			if strings.Contains(cmd.Command, secret) || strings.Contains(cmd.Output, secret) {
				t.Errorf("passwort im klartext im protokoll: %q", cmd.Command)
			}
			// Nur das PASSWORT-setzende chpasswd des Benutzer-Syncs prüfen -
			// das Join-Skript enthält seit R2-046 ein geheimnisfreies
			// "chpasswd -e" (setzt das unbrauchbare '*' auf BusyBox).
			if strings.Contains(cmd.Command, "chpasswd") && strings.Contains(cmd.Command, "deployer") {
				checkedChpasswd = true
				if !strings.Contains(cmd.Command, "«redacted»") {
					t.Errorf("chpasswd-kommando nicht redigiert: %q", cmd.Command)
				}
				// Der übrige Kontext (z.B. das chown des Home-Verzeichnisses)
				// muss erhalten bleiben - Redaction darf nicht überschießen.
				if !strings.Contains(cmd.Command, "chown -R deployer") {
					t.Errorf("redaction hat nützlichen kontext verschluckt: %q", cmd.Command)
				}
			}
		}
	}
	if !checkedChpasswd {
		t.Error("kein chpasswd-kommando im protokoll gefunden (sync hat kein passwort gesetzt?)")
	}
}

// TestSSHLogScopeIsolation: ein Manager ohne Gruppenzuordnung darf die
// Protokolle eines fremden Servers nicht sehen.
func TestSSHLogScopeIsolation(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	if _, err := env.SSHLogs.ServerSessions(repositories.ScopeManager(999), id, 0); err == nil {
		t.Error("tenant-isolation verletzt: manager sieht fremde server-protokolle")
	}
}

// TestRedactSecrets prüft die Redaction-Muster isoliert.
func TestRedactSecretsUnit(t *testing.T) {
	cases := map[string]string{
		`printf '%s:%s' deploy 'geheim' | chpasswd`: "«redacted»",
		`curl -u admin:s3cr3t https://x`:            "«redacted»",
		`export API_KEY=abcdef123456`:               "«redacted»",
		`deb https://user:pw@repo.example/x`:        "«redacted»",
	}
	for in, want := range cases {
		got := services.RedactForTest(in)
		if !strings.Contains(got, want) {
			t.Errorf("redaction fehlt für %q -> %q", in, got)
		}
		if strings.Contains(got, "geheim") || strings.Contains(got, "s3cr3t") ||
			strings.Contains(got, "abcdef123456") {
			t.Errorf("geheimnis nicht entfernt: %q", got)
		}
	}
	// Gegenprobe: sshd-Direktive darf NICHT als Geheimnis verstümmelt werden.
	if got := services.RedactForTest("PasswordAuthentication no"); got != "PasswordAuthentication no" {
		t.Errorf("sshd-direktive fälschlich redigiert: %q", got)
	}
}

// sanity: das domain-modell verknüpft sessions und kommandos.
var _ = domain.SSHSession{}
var _ = sshx.FakeResponse{}

// TestHealthRuleSessionHasStablePurpose stellt sicher, dass die vom
// Health-Rule erzeugte SSH-Session den stabilen Zweck "health-check" trägt -
// nur so lässt sie sich im Protokoll-Tab zuverlässig ausblenden.
func TestHealthRuleSessionHasStablePurpose(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	health := findSystemHealthRule(t, env)

	env.Executor.RunRule(health, "scheduler")

	sessions, err := env.SSHLogs.ServerSessions(repositories.ScopeAll(), id, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sessions {
		if s.Purpose == "health-check" {
			found = true
		}
	}
	if !found {
		var got []string
		for _, s := range sessions {
			got = append(got, s.Purpose)
		}
		t.Errorf("keine session mit purpose \"health-check\"; vorhanden: %v", got)
	}
}

// TestDebugLogEmitsSSHCommands: Auf log_level=debug erzeugte LCM bislang KEINE
// einzige DEBUG-Zeile - Fehler an der SSH-/Provisionierungsstrecke, also genau
// dort, wo LCM arbeitet, waren aus den Logs heraus nicht diagnostizierbar
// (BUG-011). Jetzt geht jedes Kommando mit Exit-Code dorthin, und zwar in der
// bereits redigierten Fassung: Secrets dürfen nie im Log landen.
func TestDebugLogEmitsSSHCommands(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	env := newTestEnv(t)
	joinTestServer(t, env, "web01")

	logged := buf.String()
	if !strings.Contains(logged, "ssh command") {
		t.Fatalf("keine DEBUG-Zeile für SSH-Kommandos im Log:\n%s", truncateForMsg(logged))
	}
	if !strings.Contains(logged, "exit_code") {
		t.Errorf("der Exit-Code fehlt in der DEBUG-Zeile:\n%s", truncateForMsg(logged))
	}
	// Das Login-Passwort des Joins darf nirgends auftauchen.
	if strings.Contains(logged, "secret") {
		t.Error("ein Secret ist ins Debug-Log gelangt")
	}
}

func truncateForMsg(s string) string {
	if len(s) > 1500 {
		return s[:1500] + " …"
	}
	return s
}
