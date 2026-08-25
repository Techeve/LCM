package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// Tests zu R2-039/R2-040: Deaktivierung und Deprovisionierung müssen auf
// dem Zielsystem WIRKEN - und Fehlschläge müssen als Fehlschläge sichtbar
// sein, statt hinter „Erfolg" zu verschwinden.

// lockTestSetup: Server joinen, Linux-Benutzer mit Key anlegen und zuordnen.
func lockTestSetup(t *testing.T) (*testEnv, uint, *domain.LinuxUser) {
	t.Helper()
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "lock01")
	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "r2ops", FullName: "R2 Ops", Email: "r2ops@example.org", Shell: "/bin/bash", Sudo: true}, "admin")
	if err != nil {
		t.Fatalf("linux-user anlegen: %v", err)
	}
	if _, err := env.LinuxUsers.AddSSHKey(u.ID, "laptop", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyFakeKeyFakeKeyFakeKeyFakeKeyFakeKey test", "admin"); err != nil {
		t.Fatalf("key hinterlegen: %v", err)
	}
	server, err := env.Servers.Get(repositories.ScopeAll(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, u.ID, "admin"); err != nil {
		t.Fatalf("zuordnen: %v", err)
	}
	return env, serverID, u
}

// TestDeaktivierterBenutzerWirdGesperrt (R2-039): Nach active=false muss der
// Sync den Benutzer auf dem Zielsystem sperren - Passwort-Sperre,
// LCM-Schlüsselblock raus, sudoers-Datei weg - und das auch sagen. Vorher
// filterte der Sync Deaktivierte nur aus der Soll-Liste und meldete
// „keine linux-benutzer für diesen server" (grün, wirkungslos).
func TestDeaktivierterBenutzerWirdGesperrt(t *testing.T) {
	env, serverID, u := lockTestSetup(t)
	server, _ := env.Servers.Get(repositories.ScopeAll(), serverID)

	// Vorher: aktiver Benutzer wird provisioniert.
	out, err := env.Prov.SyncUsers(server, "admin")
	if err != nil {
		t.Fatalf("sync (aktiv): %v", err)
	}
	if !strings.Contains(out, "provisioniert: "+u.Username) {
		t.Fatalf("aktiver Benutzer sollte provisioniert werden: %q", out)
	}

	// Deaktivieren + erneuter Sync.
	if _, err := env.LinuxUsers.Update(u.ID, services.LinuxUserUpdateInput{Active: bp(false)}, "admin"); err != nil {
		t.Fatalf("deaktivieren: %v", err)
	}
	env.Dialer.Commands = nil
	out, err = env.Prov.SyncUsers(server, "admin")
	if err != nil {
		t.Fatalf("sync (deaktiviert): %v", err)
	}
	if !strings.Contains(out, "gesperrt (deaktiviert): "+u.Username) {
		t.Fatalf("der Sync muss die Sperre benennen, sagte: %q", out)
	}
	// Das ausgeführte Kommando muss die drei Sperr-Schritte enthalten.
	joined := strings.Join(env.Dialer.Commands, "\n")
	for _, must := range []string{"usermod -L", "passwd -l", "authorized_keys", "rm -f /etc/sudoers.d/lcm-" + u.Username} {
		if !strings.Contains(joined, must) {
			t.Errorf("Sperr-Kommando ohne %q:\n%s", must, joined)
		}
	}
	if strings.Contains(out, "keine linux-benutzer") {
		t.Error("die alte Leere-Liste-Meldung darf für zugeordnete Deaktivierte nicht mehr erscheinen")
	}
}

// TestDeprovisionEhrlichBeiFehlschlag (R2-040): Schlägt das Entfernen auf
// dem Zielsystem fehl (BusyBox ohne userdel, Konto besteht weiter), muss
// die Antwort ok:false sein und die Zuordnung BESTEHEN bleiben - kein
// verwaistes Konto, das LCM nicht mehr kennt.
func TestDeprovisionEhrlichBeiFehlschlag(t *testing.T) {
	env, serverID, u := lockTestSetup(t)

	// Das Zielsystem lässt das Entfernen scheitern (wie Alpine ohne userdel:
	// das Skript endet dank Nachweis-Prüfung mit exit 1).
	env.Dialer.Responses["if id -u "+u.Username] = sshx.FakeResponse{
		Output: "konto " + u.Username + " besteht weiterhin - entfernen fehlgeschlagen", ExitCode: 1,
	}

	results, err := env.LinuxUsers.RemoveFromAllServers(u.ID, "admin")
	if err != nil {
		t.Fatalf("remove-from-servers: %v", err)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("der Fehlschlag muss als ok:false gemeldet werden: %+v", results)
	}
	if !strings.Contains(results[0].Message, "besteht weiterhin") {
		t.Errorf("die Ursache soll in der Meldung stehen: %q", results[0].Message)
	}
	// Zuordnung bleibt - der Benutzer ist weiterhin sichtbar und verwaltet.
	servers, err := repositories.NewLinuxUserRepository(env.DB()).ServersForUser(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ID != serverID {
		t.Errorf("bei Fehlschlag muss die Zuordnung bestehen bleiben, hat: %d", len(servers))
	}

	// Und der Benutzer darf folglich nicht löschbar sein.
	if err := env.LinuxUsers.Delete(u.ID, "admin"); err == nil {
		t.Error("Löschen darf erst nach vollständiger Deprovisionierung möglich sein")
	}
}

// TestDeprovisionErfolgLoestZuordnung: der Erfolgsweg bleibt unverändert -
// alles entfernt, Zuordnungen weg, Benutzer löschbar.
func TestDeprovisionErfolgLoestZuordnung(t *testing.T) {
	env, _, u := lockTestSetup(t)
	results, err := env.LinuxUsers.RemoveFromAllServers(u.ID, "admin")
	if err != nil {
		t.Fatalf("remove-from-servers: %v", err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("erwarteter Erfolg: %+v", results)
	}
	if servers, _ := repositories.NewLinuxUserRepository(env.DB()).ServersForUser(u.ID); len(servers) != 0 {
		t.Errorf("nach Erfolg dürfen keine Zuordnungen übrig sein: %d", len(servers))
	}
	if err := env.LinuxUsers.Delete(u.ID, "admin"); err != nil {
		t.Errorf("nach vollständiger Deprovisionierung muss Löschen gehen: %v", err)
	}
}

// TestRemoveFromServersTeilmenge (R2-042): server_ids wurde still ignoriert
// und IMMER überall deprovisioniert. Jetzt: nur die genannten Server;
// unbekannte IDs werden benannt; der Rest bleibt zugeordnet.
func TestRemoveFromServersTeilmenge(t *testing.T) {
	env, serverA, u := lockTestSetup(t)
	serverB := joinTestServer(t, env, "lock02")
	srvB, err := env.Servers.Get(repositories.ScopeAll(), serverB)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(srvB, u.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	// Nur Server B entfernen - plus eine nicht zugeordnete ID.
	results, err := env.LinuxUsers.RemoveFromServers(u.ID, []uint{serverB, 9999}, "admin")
	if err != nil {
		t.Fatalf("remove-from-servers: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("erwartete 2 Ergebnisse, bekam %d: %+v", len(results), results)
	}
	byID := map[uint]services.ServerDeprovisionResult{}
	for _, r := range results {
		byID[r.ServerID] = r
	}
	if !byID[serverB].OK {
		t.Errorf("Server B sollte entfernt sein: %+v", byID[serverB])
	}
	if byID[9999].OK || !strings.Contains(byID[9999].Message, "nicht zugeordnet") {
		t.Errorf("unbekannte ID muss benannt abgelehnt werden: %+v", byID[9999])
	}
	// Server A bleibt zugeordnet - der Zugang dort besteht weiter.
	servers, _ := repositories.NewLinuxUserRepository(env.DB()).ServersForUser(u.ID)
	if len(servers) != 1 || servers[0].ID != serverA {
		t.Fatalf("Server A muss zugeordnet bleiben, hat: %+v", servers)
	}
}

// TestRemoveFromServersGruppenZuordnung (R2-042): Eine Gruppen-Zuordnung
// lässt sich nicht je Server lösen - der nächste Sync provisionierte das
// Konto sofort wieder. Klartext-Ablehnung statt Scheinwirkung.
func TestRemoveFromServersGruppenZuordnung(t *testing.T) {
	env, serverA, u := lockTestSetup(t)
	// Direkte Zuordnung lösen, Gruppe mit dem Server + Benutzer aufbauen.
	if err := repositories.NewLinuxUserRepository(env.DB()).RemoveFromServer(u.ID, serverA); err != nil {
		t.Fatal(err)
	}
	group, err := env.Groups.Create("lockgrp", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverA, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), u.ID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}

	results, err := env.LinuxUsers.RemoveFromServers(u.ID, []uint{serverA}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].OK || !strings.Contains(results[0].Message, "Gruppe") {
		t.Fatalf("Gruppen-Zuordnung muss mit Klartext abgelehnt werden: %+v", results)
	}
}
