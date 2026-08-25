package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

func TestLinuxUserValidation(t *testing.T) {
	env := newTestEnv(t)

	// Ungültiger Linux-Benutzername (beginnt mit Ziffer).
	if _, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "1abc", FullName: "", Email: "", Shell: "", Sudo: false}, "admin"); !errors.Is(err, services.ErrInvalidLinuxUsername) {
		t.Errorf("ungültiger username nicht abgelehnt: %v", err)
	}
	// Gültig.
	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "Deploy Bot", Email: "", Shell: "/bin/bash", Sudo: true}, "admin")
	if err != nil {
		t.Fatalf("gültiger linux-user abgelehnt: %v", err)
	}
	// Doppelt.
	if _, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "", Email: "", Shell: "", Sudo: false}, "admin"); !errors.Is(err, services.ErrLinuxUsernameTaken) {
		t.Errorf("doppelter username nicht abgelehnt: %v", err)
	}

	// SSH-Key: ungültig + gültig.
	if _, err := env.LinuxUsers.AddSSHKey(u.ID, "bad", "nicht-ssh", "admin"); !errors.Is(err, services.ErrInvalidSSHKey) {
		t.Errorf("ungültiger key nicht abgelehnt: %v", err)
	}
	valid := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuvwxyz012345 user@host"
	if _, err := env.LinuxUsers.AddSSHKey(u.ID, "laptop", valid, "admin"); err != nil {
		t.Fatalf("gültiger key abgelehnt: %v", err)
	}
	got, _ := env.LinuxUsers.Get(u.ID)
	if len(got.SSHKeys) != 1 {
		t.Errorf("key wurde nicht gespeichert (%d)", len(got.SSHKeys))
	}
}

func TestActivationLinkRoundtrip(t *testing.T) {
	env := newTestEnv(t)
	// Neuer LCM-Manager-User, dessen Passwort per Aktivierungslink gesetzt wird.
	user, err := env.Users.CreateUser("newbie", "", "Kompass3-Seegras!Pfad", "New", "Bie", []string{domain.RoleManager}, "test-admin")
	if err != nil {
		t.Fatal(err)
	}

	token, _, err := env.Activation.Generate(user.ID, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("kein token erzeugt")
	}

	// Schwaches Passwort wird abgelehnt.
	if _, err := env.Activation.Consume(token, "kurz"); !errors.Is(err, services.ErrWeakPassword) {
		t.Errorf("schwaches passwort nicht abgelehnt: %v", err)
	}
	// Gültige Einlösung.
	activated, err := env.Activation.Consume(token, "mein-neues-sicheres-passwort")
	if err != nil {
		t.Fatalf("einlösung fehlgeschlagen: %v", err)
	}
	if activated.MustChangePassword {
		t.Error("must_change_password sollte nach aktivierung false sein")
	}
	// Login mit dem neuen Passwort funktioniert.
	if _, _, err := env.Auth.Login("newbie", "mein-neues-sicheres-passwort"); err != nil {
		t.Errorf("login nach aktivierung fehlgeschlagen: %v", err)
	}
	// Zweite Einlösung desselben Tokens scheitert.
	if _, err := env.Activation.Consume(token, "noch-ein-passwort-123"); !errors.Is(err, services.ErrActivationExpired) {
		t.Errorf("verbrauchtes token erneut einlösbar: %v", err)
	}
}

func TestLinuxUserProvisionedToServer(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	server, _ := env.Servers.Get(repositories.ScopeAll(), serverID)

	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deployer", FullName: "Deploy", Email: "", Shell: "/bin/bash", Sudo: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	valid := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuvwxyz012345 deployer@host"
	if _, err := env.LinuxUsers.AddSSHKey(lu.ID, "laptop", valid, "admin"); err != nil {
		t.Fatal(err)
	}

	// Linux-User direkt dem Server zuordnen - löst Verteilung aus (FakeDialer
	// antwortet mit exit 0). Kein Fehler erwartet.
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatalf("zuordnung/verteilung fehlgeschlagen: %v", err)
	}
}

// TestLinuxUserDeleteBlockedUntilDeprovisioned ist der Regressionstest für
// die Sicherheitsvorgabe: ein Benutzer, der noch auf Servern provisioniert
// ist, kann NICHT gelöscht werden - erst nach dem aktiven Entfernen (userdel)
// von allen Servern.
func TestLinuxUserDeleteBlockedUntilDeprovisioned(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	server, _ := env.Servers.Get(repositories.ScopeAll(), serverID)

	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "tempuser", FullName: "Temp", Email: "", Shell: "/bin/bash", Sudo: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	// Löschen muss blockiert sein, solange der User dem Server zugeordnet ist.
	if err := env.LinuxUsers.Delete(lu.ID, "admin"); !errors.Is(err, services.ErrLinuxUserStillOnServers) {
		t.Fatalf("löschen trotz server-zuordnung nicht blockiert: %v", err)
	}

	// Von allen Servern entfernen: muss ein userdel auf dem Zielserver auslösen.
	env.Dialer.Commands = nil
	results, err := env.LinuxUsers.RemoveFromAllServers(lu.ID, "admin")
	if err != nil {
		t.Fatalf("remove-from-all-servers: %v", err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Errorf("erwartetes deprovision-ergebnis für 1 server: %+v", results)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "userdel -r tempuser") {
		t.Errorf("kein userdel auf dem zielserver ausgeführt: %v", env.Dialer.Commands)
	}

	// Jetzt ist der User löschbar.
	if err := env.LinuxUsers.Delete(lu.ID, "admin"); err != nil {
		t.Fatalf("löschen nach deprovisionierung fehlgeschlagen: %v", err)
	}
	if _, err := env.LinuxUsers.Get(lu.ID); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("user sollte gelöscht sein, bekam %v", err)
	}
}

func TestLinuxUserActivationRoundtrip(t *testing.T) {
	env := newTestEnv(t)
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "activ", FullName: "Aktiv User", Email: "a@demo.local", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	token, _, err := env.LinuxUsers.GenerateActivation(lu.ID, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("kein token")
	}

	// Leere Eingabe (weder Passwort noch Key) wird abgelehnt.
	if _, _, err := env.LinuxUsers.ConsumeActivation(token, services.LinuxActivationInput{}); !errors.Is(err, services.ErrEmptyActivation) {
		t.Errorf("leere aktivierung nicht abgelehnt: %v", err)
	}

	// Mitarbeiter setzt Passwort + SSH-Key.
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuvwxyz012345 me@laptop"
	got, priv, err := env.LinuxUsers.ConsumeActivation(token, services.LinuxActivationInput{
		Password: "Kompass3-Seegras!Pfad", KeyName: "Laptop", PublicKey: key,
	})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if priv != "" {
		t.Error("ohne generate_key darf kein privater schlüssel zurückkommen")
	}
	if !got.HasPassword {
		t.Error("has_password sollte true sein")
	}
	if len(got.SSHKeys) != 1 {
		t.Errorf("erwartet 1 key, bekam %d", len(got.SSHKeys))
	}

	// Token ist verbraucht.
	if _, _, err := env.LinuxUsers.ConsumeActivation(token, services.LinuxActivationInput{Password: "x-noch-eins"}); !errors.Is(err, services.ErrActivationExpired) {
		t.Errorf("verbrauchtes token erneut nutzbar: %v", err)
	}
}

func TestLinuxUserActivationGeneratesKeyPair(t *testing.T) {
	env := newTestEnv(t)
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "genkey", FullName: "Gen Key", Email: "g@demo.local", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := env.LinuxUsers.GenerateActivation(lu.ID, 0, "admin")
	if err != nil {
		t.Fatal(err)
	}

	got, priv, err := env.LinuxUsers.ConsumeActivation(token, services.LinuxActivationInput{
		KeyName: "Mein Rechner", GenerateKey: true,
	})
	if err != nil {
		t.Fatalf("consume mit generate_key: %v", err)
	}
	if !strings.Contains(priv, "OPENSSH PRIVATE KEY") {
		t.Errorf("privater schlüssel fehlt/falsches format: %q", priv[:min(40, len(priv))])
	}
	if len(got.SSHKeys) != 1 || !strings.HasPrefix(got.SSHKeys[0].PublicKey, "ssh-ed25519 ") {
		t.Errorf("public key nicht gespeichert: %+v", got.SSHKeys)
	}
	if got.SSHKeys[0].Name != "Mein Rechner" {
		t.Errorf("key-name: %q", got.SSHKeys[0].Name)
	}
}

func TestGenerateSSHKeyStoresOnlyPublicKey(t *testing.T) {
	env := newTestEnv(t)
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "keyadmin", FullName: "", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	key, priv, err := env.LinuxUsers.GenerateSSHKey(lu.ID, "Arbeitsplatz", "admin")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(priv, "OPENSSH PRIVATE KEY") {
		t.Error("privater schlüssel fehlt im rückgabewert")
	}
	if !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") {
		t.Errorf("unerwarteter public key: %q", key.PublicKey)
	}
	// Der private Schlüssel darf NIRGENDS gespeichert sein.
	got, err := env.LinuxUsers.Get(lu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SSHKeys) != 1 || strings.Contains(got.SSHKeys[0].PublicKey, "PRIVATE") {
		t.Errorf("erwartet genau 1 public key ohne private-material: %+v", got.SSHKeys)
	}
}

// TestProvisionedUsersAreUsable deckt BUG-028 ab: dieselbe Ursache wie beim
// Service-User (BUG-007/008), aber für das gesamte Benutzer-Provisioning -
// ein beworbenes Kernfeature. Auf openSUSE und Alpine meldete LCM für jede
// Zuweisung "ok", die angelegten Konten konnten sich aber nicht anmelden:
// useradd fehlt auf BusyBox, und ein frisches Konto hat ein gesperrtes
// Passwortfeld, das OpenSSH bei "UsePAM no" auch mit gültigem Schlüssel
// ablehnt.
func TestProvisionedUsersAreUsable(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "deploy", FullName: "Deploy User", Email: "d@example.com", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	if err := env.Prov.AssignLinuxUserToServer(server, lu.ID, "admin"); err != nil {
		t.Fatalf("Benutzer zuweisen: %v", err)
	}

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "command -v useradd") || !strings.Contains(all, "adduser -D") {
		t.Errorf("Benutzeranlage soll das Werkzeug nach Verfügbarkeit wählen:\n%s", all)
	}
	if !strings.Contains(all, "usermod -p") {
		t.Errorf("Passwortsperre des provisionierten Kontos wird nicht aufgehoben:\n%s", all)
	}
}
