package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestGesperrterLcmBenutzerBleibtNachSyncGesperrt ist der Kern der Sache:
// Eine Sperre auf einem verteilten Konto muss den nächsten Benutzer-Sync
// überleben. Täte sie das nicht, wäre der Knopf eine Lüge - der Betreiber
// hätte gesperrt und LCM hätte es kurz darauf stillschweigend aufgehoben.
func TestGesperrterLcmBenutzerBleibtNachSyncGesperrt(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	server := joinPlainServer(t, env, "blocksrv")

	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatalf("zuordnen: %v", err)
	}

	// Sperren ist bei einem verteilten Konto erlaubt.
	if _, err := env.Prov.SetServerUserDisabled(server, "deploy", true, "admin"); err != nil {
		t.Fatalf("sperren: %v", err)
	}
	users, err := env.Prov.ListServerUsers(server)
	if err != nil {
		t.Fatal(err)
	}
	if !findUser(users, "deploy").Blocked {
		t.Fatal("Sperre wurde nicht gemerkt")
	}

	// Jetzt der Sync: Er darf das Konto NICHT wieder provisionieren.
	env.Dialer.Commands = nil
	out, err := env.Prov.SyncUsers(server, "admin")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	alle := strings.Join(env.Dialer.Commands, "\n")
	if strings.Contains(alle, "install -d -m 700 -o deploy") {
		t.Errorf("der Sync hat das gesperrte Konto wieder provisioniert:\n%s", alle)
	}
	if !strings.Contains(alle, "usermod -L deploy") {
		t.Errorf("der Sync hat das gesperrte Konto nicht gesperrt gehalten:\n%s", alle)
	}
	if !strings.Contains(out, "gesperrt") {
		t.Errorf("das Job-Protokoll benennt die Sperre nicht: %s", out)
	}

	// Freigeben nimmt die Merkung zurück, der Sync provisioniert wieder.
	if _, err := env.Prov.SetServerUserDisabled(server, "deploy", false, "admin"); err != nil {
		t.Fatalf("freigeben: %v", err)
	}
	env.Dialer.Commands = nil
	if _, err := env.Prov.SyncUsers(server, "admin"); err != nil {
		t.Fatalf("sync nach freigabe: %v", err)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "install -d -m 700 -o deploy") {
		t.Error("nach der Freigabe wurde das Konto nicht wieder provisioniert")
	}
}

// TestVerwaltetesKontoNichtEndgueltigEntfernbar: Sperren ja, Löschen nein -
// Letzteres legte der nächste Sync ohnehin wieder an.
func TestVerwaltetesKontoNichtEndgueltigEntfernbar(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	server := joinPlainServer(t, env, "rmsrv")

	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Prov.RemoveServerUser(server, "deploy", "admin"); !errors.Is(err, services.ErrServerUserManaged) {
		t.Errorf("verwaltetes Konto sollte nicht entfernbar sein, bekam: %v", err)
	}
}

// TestServiceUserBleibtUnantastbar: auch wenn er über LCM verteilt würde.
func TestServiceUserBleibtUnantastbar(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.Responses["getent passwd"] = sshx.FakeResponse{Output: scanResponse}
	server := joinPlainServer(t, env, "svcsrv")

	for _, name := range []string{"root", server.ServiceUser} {
		if _, err := env.Prov.SetServerUserDisabled(server, name, true, "admin"); !errors.Is(err, services.ErrServerUserProtected) {
			t.Errorf("%s ließ sich sperren (Fehler: %v)", name, err)
		}
	}
	// Und die Sperre darf auch nicht in der Datenbank gelandet sein.
	blocked, err := repositories.NewServerRepository(env.DB()).BlockedServerUsers(server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 0 {
		t.Errorf("Sperren trotz Schutz gemerkt: %v", blocked)
	}
}

func findUser(users []domain.ServerUser, name string) domain.ServerUser {
	for _, u := range users {
		if u.Username == name {
			return u
		}
	}
	return domain.ServerUser{}
}
