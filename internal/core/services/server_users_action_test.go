package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
)

// scanAntwort ist eine plausible Ausgabe des Benutzer-Scans.
const scanResponse = "LCMUSER|root|0|/bin/bash|set|0|no|no|\n" +
	"LCMUSER|deploy|1001|/bin/bash|none|1|no|no|\n" +
	"LCMUSER|gast|1002|/bin/sh|set|0|no|no|\n"

func joinPlainServer(t *testing.T, env *testEnv, name string) *domain.Server {
	t.Helper()
	// Der Join prüft Root-Rechte des Service-Users und die Paketverwaltung.
	env.Dialer.Responses["sudo -n id -u"] = sshx.FakeResponse{Output: "0\n"}
	env.Dialer.Responses["apt-get dnf zypper"] = sshx.FakeResponse{Output: "apt-get\n"}
	env.Dialer.Responses["os-release"] = sshx.FakeResponse{Output: "NAME=\"Debian GNU/Linux\"\n"}
	server, err := env.Servers.Join(services.JoinRequest{
		Name: name, Host: name + ".test", Port: 22, LoginUser: "root",
		LoginPassword: "secret", ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join fehlgeschlagen: %v", err)
	}
	return server
}

// TestServerUsersRefreshUndListe: der Refresh erhebt über SSH, speichert und
// markiert LCM-verwaltete Konten; die Liste liest aus dem Bestand.
func TestServerUsersRefreshUndListe(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	server := joinPlainServer(t, env, "usrsrv")

	// "deploy" ist ein von LCM verteilter Benutzer dieses Servers.
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatalf("zuordnen: %v", err)
	}

	users, err := env.Prov.RefreshServerUsers(server, "admin")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("erwartet 3 konten, bekam %d", len(users))
	}
	byName := map[string]domain.ServerUser{}
	for _, u := range users {
		byName[u.Username] = u
	}
	if !byName["deploy"].Managed {
		t.Error("deploy sollte als LCM-verwaltet markiert sein")
	}
	if byName["gast"].Managed || byName["root"].Managed {
		t.Error("gast/root fälschlich als verwaltet markiert")
	}

	// Liste kommt aus dem gespeicherten Bestand (ohne neuen SSH-Kontakt).
	stored, err := env.Prov.ListServerUsers(server)
	if err != nil || len(stored) != 3 {
		t.Fatalf("liste: %v (%d)", err, len(stored))
	}
	if stored[0].Username != "root" {
		t.Errorf("liste nicht UID-sortiert: %+v", stored[0])
	}
}

// TestServerUserGuards: root und der Service-User bleiben unantastbar;
// verteilte Konten sind sperrbar, aber nicht endgültig entfernbar.
func TestServerUserGuards(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	server := joinPlainServer(t, env, "guardsrv")

	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	if _, err := env.Prov.SetServerUserDisabled(server, "root", true, "admin"); !errors.Is(err, services.ErrServerUserProtected) {
		t.Errorf("root nicht geschützt: %v", err)
	}
	if _, err := env.Prov.RemoveServerUser(server, server.ServiceUser, "admin"); !errors.Is(err, services.ErrServerUserProtected) {
		t.Errorf("service-user nicht geschützt: %v", err)
	}
	// Verwaltete Konten sind SPERRBAR (die Sperre wird gemerkt und überlebt
	// den Sync - siehe TestGesperrterLcmBenutzerBleibtNachSyncGesperrt);
	// nur das endgültige Entfernen bleibt ihnen verwehrt.
	if _, err := env.Prov.SetServerUserDisabled(server, "deploy", true, "admin"); err != nil {
		t.Errorf("verwalteter benutzer sollte sperrbar sein: %v", err)
	}
	if _, err := env.Prov.RemoveServerUser(server, "deploy", "admin"); !errors.Is(err, services.ErrServerUserManaged) {
		t.Errorf("verwalteter benutzer darf nicht entfernbar sein: %v", err)
	}
	if _, err := env.Prov.RemoveServerUser(server, "böse;name", "admin"); !errors.Is(err, services.ErrInvalidServerUsername) {
		t.Errorf("ungültiger name nicht abgelehnt: %v", err)
	}
}

// TestServerUserDisableLaeuftUndErhebtNeu: die Deaktivierung führt das
// Sperr-Skript aus (inkl. Ablaufdatum - sonst blieben Key-Logins offen) und
// erhebt den Bestand in derselben Verbindung neu.
func TestServerUserDisableLaeuftUndErhebtNeu(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	server := joinPlainServer(t, env, "disablesrv")

	env.Dialer.Commands = nil
	if _, err := env.Prov.SetServerUserDisabled(server, "gast", true, "admin"); err != nil {
		t.Fatalf("deaktivieren: %v", err)
	}
	var sperrKommando string
	for _, c := range env.Dialer.Commands {
		if strings.Contains(c, "usermod -L gast") {
			sperrKommando = c
		}
	}
	if sperrKommando == "" {
		t.Fatalf("sperr-skript lief nicht: %v", env.Dialer.Commands)
	}
	if !strings.Contains(sperrKommando, "usermod -e 1970-01-02 gast") {
		t.Error("deaktivierung setzt kein ablaufdatum - SSH-Key-Logins blieben offen")
	}
	// Neuerhebung in derselben Aktion: der Bestand ist gefüllt.
	stored, err := env.Prov.ListServerUsers(server)
	if err != nil || len(stored) == 0 {
		t.Fatalf("bestand nach aktion leer: %v (%d)", err, len(stored))
	}
}

// TestZugangsbenutzerZeigtSeineAnmeldungen: LCM meldet sich ohne Terminal an
// (Kommando-Sitzung), und dafür schreibt sshd weder wtmp noch lastlog - auf
// einem echten Server nachgeprüft. Der Zugangsbenutzer stand deshalb dauerhaft
// auf „nie angemeldet", obwohl er das Konto mit den meisten Anmeldungen ist.
// Die Übersicht nimmt für ihn deshalb LCMs eigenes Sitzungsprotokoll.
func TestZugangsbenutzerZeigtSeineAnmeldungen(t *testing.T) {
	env := newTestEnv(t)
	server := joinPlainServer(t, env, "svcsrv")
	// Der Scan liefert den Zugangsbenutzer ohne jede Anmeldung - genau so,
	// wie ihn wtmp/lastlog auf einem echten System zeigen.
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{
		Output: "LCMUSER|root|0|/bin/bash|set|0|no|no|\n" +
			"LCMUSER|" + server.ServiceUser + "|1001|/bin/bash|none|1|no|no|\n",
	}
	users, err := env.Prov.RefreshServerUsers(server, "admin")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	var svc *domain.ServerUser
	for i := range users {
		if users[i].Username == server.ServiceUser {
			svc = &users[i]
		}
	}
	if svc == nil {
		t.Fatalf("Zugangsbenutzer fehlt in der Übersicht: %+v", users)
	}
	// Der Refresh selbst lief über eine protokollierte Sitzung - die zählt.
	if svc.LoginCount == 0 || svc.LastLoginAt == nil {
		t.Fatalf("keine Anmeldungen für den Zugangsbenutzer: count=%d last=%v", svc.LoginCount, svc.LastLoginAt)
	}
	if !svc.LoginsFromLCM {
		t.Error("Herkunft der Anmeldungen nicht als LCM-Protokoll gekennzeichnet")
	}

	// Und die Historie dazu kommt aus derselben Quelle.
	logins, err := env.Prov.ServerUserLogins(server, server.ServiceUser, 50)
	if err != nil {
		t.Fatalf("historie: %v", err)
	}
	if len(logins) == 0 {
		t.Fatal("keine Anmelde-Historie für den Zugangsbenutzer")
	}
	if logins[0].TTY == "" {
		t.Error("Zweck der Sitzung fehlt (steht anstelle des Terminals)")
	}

	// Gegenprobe: ein normales Konto bleibt bei der wtmp-Quelle und damit
	// bei „nie angemeldet" - die Sonderbehandlung greift nur beim Zugang.
	for i := range users {
		if users[i].Username == "root" && (users[i].LoginsFromLCM || users[i].LoginCount != 0) {
			t.Errorf("root fälschlich aus dem LCM-Protokoll gefüllt: %+v", users[i])
		}
	}
}
