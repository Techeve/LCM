package services_test

import (
	"strings"
	"testing"

	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Die SSH-Haertung meldete "gehaertet", ohne die Wirkung zu belegen: auf
// socket-aktiviertem sshd (Debian 13) lieferte `sshd -T` kein Ergebnis, LCM
// protokollierte den Zweifel nur und setzte ssh_hardened trotzdem (R2-014).
// Auf openSUSE Leap 16 wiederum brach die Kette an der nicht vorhandenen
// /etc/ssh/sshd_config ab, und die Meldung nannte einen
// Konfigurationskonflikt, den es gar nicht gab (R2-015).

// TestSSHHaertungBleibtUnbelegtOhneErgebnis: `sshd -T` ohne Ergebnis ist kein
// Beleg für Erfolg. Bis R2-014 meldete LCM hier „gehärtet" und setzte
// ssh_hardened=true, während die Passwort-Anmeldung nachweislich offen blieb;
// der Zweifel stand nur im Log.
func TestSSHHaertungBleibtUnbelegtOhneErgebnis(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{Output: ""}

	_, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin")
	if err == nil {
		t.Fatal("erwartete einen Fehler - die Wirkung ist nicht belegt")
	}
	if !strings.Contains(err.Error(), "nicht überprüfbar") {
		t.Errorf("die Meldung soll den fehlenden Nachweis benennen, bekam: %v", err)
	}
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); srv.SSHHardened {
		t.Error("ssh_hardened darf ohne Nachweis nicht gesetzt sein")
	}
}

// TestSSHHaertungLegtRunSSHDAn: auf socket-aktiviertem sshd (Debian 13)
// existiert /run/sshd nur während einer Verbindung - ohne das Verzeichnis
// bricht `sshd -T` ab und die Prüfung lief ergebnislos ins Leere (R2-014).
func TestSSHHaertungLegtRunSSHDAn(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication no\n",
	}
	env.Dialer.Commands = nil
	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härten: %v", err)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "mkdir -p /run/sshd") {
		t.Error("die Wirkungsprobe legt /run/sshd nicht an - auf socket-aktiviertem sshd liefe sie leer")
	}
	// Die Fehlerausgabe darf nicht mehr verworfen werden.
	if strings.Contains(all, "sshd -T 2>/dev/null") {
		t.Error("die Fehlerausgabe von sshd -T wird verworfen - die Ursache bliebe unsichtbar")
	}
}

// TestSSHKonfigAuchUnterUsrEtc: openSUSE Leap 16 hat ein stateless /etc und
// liefert sshd_config unter /usr/etc/ssh. Dort brach die Kette am cp ab, das
// Drop-in wurde nie geschrieben - und LCM meldete einen
// Konfigurationskonflikt, den es gar nicht gab (R2-015).
func TestSSHKonfigAuchUnterUsrEtc(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication no\n",
	}
	env.Dialer.Commands = nil
	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härten: %v", err)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "/usr/etc/ssh/sshd_config") {
		t.Error("die sshd_config wird nur unter /etc/ssh gesucht - auf openSUSE Leap 16 liegt sie unter /usr/etc/ssh")
	}
}

// TestSSHReloadMaskiertKeineFehler: `A && B && C || D` bindet die Shell von
// links gleichrangig - schlug ein früherer Schritt fehl, sprang die
// Ausführung in den ersten ||-Zweig, und ein dort gelingender Reload setzte
// den Exit-Code wieder auf 0. Der Fehlschlag der Schreibkette blieb damit
// unsichtbar (R2-014, Nebenbeobachtung).
func TestSSHReloadMaskiertKeineFehler(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication no\n",
	}
	env.Dialer.Commands = nil
	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härten: %v", err)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	// Die Kaskade steht in einer { if …; }-Gruppe: ohne Gruppierung würde ein
	// gelingender ||-Zweig den Fehlschlag der Schreibkette davor maskieren.
	if !strings.Contains(all, "{ if systemctl is-active --quiet ssh.socket") {
		t.Error("die Reload-Kaskade steht nicht in einer Gruppe - sie würde Fehler davor maskieren")
	}
}

// TestSSHReloadSchontSocketAktiviertenDienst: Bei socket-aktiviertem sshd
// scheitert ein Reload am systemd-eigenen Socket (zweiter sshd, failed,
// /run/sshd weg - live auf debian13 reproduziert). Die Socket-Prüfung muss
// deshalb VOR der Reload-Kaskade stehen. Und weil Debian 13 den Socket mit
// Accept=no betreibt (EIN dauerhafter ssh.service bedient alle Verbindungen),
// darf der Socket-Zweig nicht bloß überspringen: er muss den aktiven Dienst
// NEU STARTEN, sonst bleibt jede Konfigurationsänderung wirkungslos im alten
// Daemon liegen (beim 2FA-Test so aufgetreten).
func TestSSHReloadSchontSocketAktiviertenDienst(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["^passwordauthentication"] = sshx.FakeResponse{
		Output: "passwordauthentication no\n",
	}
	env.Dialer.Commands = nil
	if _, err := env.Servers.HardenSSH(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("härten: %v", err)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "is-active --quiet ssh.socket") {
		t.Error("die Socket-Aktivierung wird nicht geprüft - auf Debian 13 zerlegt der Reload den sshd-Dienst")
	}
	if !strings.Contains(all, "then systemctl restart ssh;") {
		t.Error("der Socket-Zweig startet den aktiven Dienst nicht neu - auf Debian 13 (Accept=no) bliebe die Konfiguration im alten Daemon liegen")
	}
	socket := strings.Index(all, "is-active --quiet ssh.socket")
	reload := strings.Index(all, "systemctl reload sshd")
	if socket < 0 || reload < 0 || socket > reload {
		t.Errorf("die Socket-Prüfung muss VOR der Reload-Kaskade stehen (socket=%d reload=%d)", socket, reload)
	}
}
