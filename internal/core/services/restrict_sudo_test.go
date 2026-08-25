package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestRestrictSudoPersistsAndWritesWhitelist: das nachträgliche Einschränken
// ersetzt die sudoers durch die Whitelist (visudo-geprüft, atomar), legt die
// Shim-Wrapper an und persistiert restricted_sudo=true. Das Umschalt-Skript
// läuft noch im Voll-Modus (sudo sh -c), damit visudo/mv durchgehen.
func TestRestrictSudoPersistsAndWritesWhitelist(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01") // joint als root → Voll-Modus
	repo := repositories.NewServerRepository(env.DB())

	env.Dialer.Commands = nil
	out, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("einschränken fehlgeschlagen: %v\n%s", err, out)
	}

	srv, _ := repo.FindByID(repositories.ScopeAll(), id)
	if !srv.RestrictedSudo {
		t.Error("restricted_sudo sollte nach dem Einschränken true sein")
	}

	all := strings.Join(env.Dialer.Commands, "\n")
	// Strukturelle Marker der eingeschränkten Provisionierung (die Datei-Inhalte
	// selbst sind base64-kodiert; diese Literale sind eindeutig genug):
	// sudoers per visudo geprüft + atomar getauscht, Shim-Verzeichnis angelegt.
	for _, marker := range []string{"visudo -cf /etc/sudoers.d/", "mv /etc/sudoers.d/", ".lcm/sudo-bin", "base64 -d"} {
		if !strings.Contains(all, marker) {
			t.Errorf("Marker %q fehlt im Umschalt-Skript:\n%s", marker, all)
		}
	}
	// Das Skript lief NOCH im Voll-Modus (sudo sh -c), sonst würde visudo/mv
	// scheitern (nicht auf der Whitelist).
	if !strings.Contains(all, "sudo sh -c") {
		t.Errorf("Umschalt-Skript sollte im Voll-Modus (sudo sh -c) laufen:\n%s", all)
	}
	// Der LCM-Helper wird mitinstalliert (atomar nach /usr/local/sbin).
	if !strings.Contains(all, "mv /usr/local/sbin/lcm-helper.tmp /usr/local/sbin/lcm-helper") {
		t.Errorf("LCM-Helper-Installation fehlt im Umschalt-Skript:\n%s", all)
	}
}

// TestRestrictedActionsUseHelper: die im eingeschränkten Modus wieder
// erlaubten Kernfunktionen laufen über den validierenden LCM-Helper statt
// über Root-Shell-Skripte - Repository einrichten, apt-Cache-Anbindung,
// SSH-Härtung und Benutzer-Sync.
func TestRestrictedActionsUseHelper(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true)

	env.Dialer.Commands = nil
	if _, err := env.Servers.AddKnownRepository(repositories.ScopeAll(), id, "docker", "admin"); err != nil {
		t.Fatalf("AddKnownRepository (restricted): %v", err)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "lcm-helper repo-add") {
		t.Errorf("repo-add sollte über den helper laufen:\n%s", all)
	}
	// Restricted-Ausführung: PATH-Shim statt sudo-Root-Shell.
	if !strings.Contains(all, ".lcm/sudo-bin") {
		t.Errorf("helper-aufruf sollte über den PATH-Shim laufen:\n%s", all)
	}

	env.Dialer.Commands = nil
	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("HardenSSH (restricted): %v", err)
	}
	if all := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(all, "lcm-helper ssh-harden") {
		t.Errorf("ssh-härtung sollte über den helper laufen:\n%s", all)
	}
}

// TestRestrictSudoAlreadyRestrictedBlocked: ein bereits eingeschränkter Server
// liefert ErrRestrictedSudo (nichts zu tun / nicht erlaubt).
func TestRestrictSudoAlreadyRestrictedBlocked(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true)

	if _, err := env.Servers.RestrictSudo(repositories.ScopeAll(), id, "admin"); err != services.ErrRestrictedSudo {
		t.Errorf("erwartet ErrRestrictedSudo, bekam %v", err)
	}
}
