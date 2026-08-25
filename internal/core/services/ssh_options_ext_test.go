package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestChangeSSHPortSuccess: erfolgreicher Portwechsel persistiert den neuen
// Port; die Verifikation dialt tatsächlich den neuen Port.
func TestChangeSSHPortSuccess(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	out, err := env.Servers.ChangeSSHPort(repositories.ScopeAll(), id, 2222, "admin")
	if err != nil {
		t.Fatalf("portwechsel fehlgeschlagen: %v\n%s", err, out)
	}
	srv, _ := repo.FindByID(repositories.ScopeAll(), id)
	if srv.SSHPort != 2222 {
		t.Errorf("erwartet ssh_port 2222, bekam %d", srv.SSHPort)
	}
	dialedNew := false
	for _, p := range env.Dialer.DialedPorts {
		if p == 2222 {
			dialedNew = true
		}
	}
	if !dialedNew {
		t.Errorf("verifikation sollte port 2222 anwählen, ports: %v", env.Dialer.DialedPorts)
	}
}

// TestChangeSSHPortRollback: ist der neue Port nicht erreichbar, bleibt der
// alte Port aktiv und wird NICHT persistiert.
func TestChangeSSHPortRollback(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	env.Dialer.FailKeyPort = 2222 // Verifikation auf dem neuen Port scheitert
	_, err := env.Servers.ChangeSSHPort(repositories.ScopeAll(), id, 2222, "admin")
	if err == nil {
		t.Fatal("erwartete Fehler (neuer Port nicht erreichbar)")
	}
	srv, _ := repo.FindByID(repositories.ScopeAll(), id)
	if srv.SSHPort != 22 {
		t.Errorf("nach Rollback sollte der Port 22 bleiben, bekam %d", srv.SSHPort)
	}
}

// TestChangeSSHPortRejectsInvalid: Portbereich und No-Op.
func TestChangeSSHPortRejectsInvalid(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.ChangeSSHPort(repositories.ScopeAll(), id, 70000, "admin"); err == nil {
		t.Error("port 70000 sollte abgelehnt werden")
	}
	if out, err := env.Servers.ChangeSSHPort(repositories.ScopeAll(), id, 22, "admin"); err != nil || !strings.Contains(out, "bereits") {
		t.Errorf("gleicher Port sollte No-Op sein: %q / %v", out, err)
	}
}

// TestSetSSHRootLoginPersists: Schalter schreibt das Drop-in und persistiert.
func TestSetSSHRootLoginPersists(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	if _, err := env.Servers.SetSSHRootLogin(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatalf("root-login sperren fehlgeschlagen: %v", err)
	}
	srv, _ := repo.FindByID(repositories.ScopeAll(), id)
	if !srv.SSHRootLoginDisabled {
		t.Error("ssh_root_login_disabled sollte true sein")
	}
	found := false
	for _, c := range env.Dialer.Commands {
		if strings.Contains(c, "PermitRootLogin no") {
			found = true
		}
	}
	if !found {
		t.Errorf("kein PermitRootLogin no im Kommando: %v", env.Dialer.Commands)
	}
}

// TestSetSSHRootLoginRestrictedUsesHelper: im eingeschränkten Modus läuft die
// Root-Login-Einstellung über den validierenden LCM-Helper statt über das
// Inline-Skript - und persistiert genauso.
func TestSetSSHRootLoginRestrictedUsesHelper(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true)
	if _, err := env.Servers.SetSSHRootLogin(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatalf("root-login sperren (restricted) fehlgeschlagen: %v", err)
	}
	repo := repositories.NewServerRepository(env.DB())
	srv, _ := repo.FindByID(repositories.ScopeAll(), id)
	if !srv.SSHRootLoginDisabled {
		t.Error("ssh_root_login_disabled sollte true sein")
	}
	found := false
	for _, c := range env.Dialer.Commands {
		if strings.Contains(c, "lcm-helper ssh-options") && strings.Contains(c, "no") {
			found = true
		}
	}
	if !found {
		t.Errorf("kein lcm-helper ssh-options im Kommando: %v", env.Dialer.Commands)
	}
}
