package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// Der automatische Benutzer-Abgleich läuft nebenläufig - die Oberfläche soll
// nicht auf die Verbindungsaufnahme zu womöglich vielen Servern warten.
// Die Tests warten deshalb, bis alle Abgleiche im Hintergrund fertig sind
// (settle), und prüfen erst dann. Ein Pollen mit Frist hing an der Last des
// Runners, und ein noch laufender Abgleich aus der Vorbereitung las FailKey
// mitten im Umschalten.

// settle wartet, bis alle Benutzer-Abgleiche im Hintergrund fertig sind.
func (env *testEnv) settle() { env.Prov.WaitReconcilesForTest() }

// requireRan bricht ab, wenn kein Kommando mit diesem Teilstring lief.
func (env *testEnv) requireRan(t *testing.T, substr string) {
	t.Helper()
	if !env.ranCommand(substr) {
		t.Fatalf("%q nicht ausgeführt: %v", substr, env.recordedCommands())
	}
}

// requireFailedEntry bricht ab, wenn für username kein Eintrag mit
// Fehlversuch im Rückstand liegt ("" = der vollständige Abgleich).
func (env *testEnv) requireFailedEntry(t *testing.T, serverID uint, username string) {
	t.Helper()
	entries, err := env.Pending.ForServer(serverID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if entries[i].Username == username && entries[i].Attempts > 0 {
			return
		}
	}
	t.Fatalf("kein Fehlversuch für %q im Rückstand: %+v", username, entries)
}

// recordedCommands liefert die Mitschrift des Fake-Dialers als Kopie. Alle
// Zugriffe laufen ueber Recorded() statt direkt auf die Slices: Der
// Benutzer-Abgleich laeuft nebenlaeufig weiter, waehrend die Tests hier
// lesen - ein direkter Slice-Zugriff waere ein Race.
func (env *testEnv) recordedCommands() []string {
	commands, _ := env.Dialer.Recorded()
	return commands
}

// ranCommand meldet, ob ein Kommando mit diesem Teilstring ausgefuehrt wurde.
func (env *testEnv) ranCommand(substr string) bool {
	commands, _ := env.Dialer.Recorded()
	return strings.Contains(strings.Join(commands, "\n"), substr)
}

// setupGruppenBenutzer legt Server, Gruppe und einen der Gruppe zugeordneten
// Linux-Benutzer an - die Ausgangslage der folgenden Tests.
func setupGroupUser(t *testing.T, env *testEnv, serverName, username string) (*domain.Server, *domain.ServerGroup, uint) {
	t.Helper()
	serverID := joinTestServer(t, env, serverName)
	server, err := env.Servers.Get(repositories.ScopeAll(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	group, err := env.Groups.Create("abgleich-"+serverName, "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	lu, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: username, FullName: "Test", Email: "", Shell: "/bin/bash", Sudo: false}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	env.settle() // Abgleiche aus der Vorbereitung (Server tritt der Gruppe bei)
	return server, group, lu.ID
}

// Eine Gruppen-Zuordnung wirkte früher erst beim nächsten geplanten Sync - der
// Benutzer stand in LCM als berechtigt, konnte sich aber stundenlang nicht
// anmelden.
func TestGruppenZuordnungRichtetKontoSofortEin(t *testing.T) {
	env := newTestEnv(t)
	_, group, userID := setupGroupUser(t, env, "web01", "deployer")

	env.Dialer.Reset()
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "id -u deployer")
}

// Und die Gegenrichtung: Wird die Zuordnung gelöst, muss das Konto vom Server
// verschwinden - sonst bleibt ein Zugang bestehen, den LCM nicht mehr führt.
func TestEntzogeneGruppenZuordnungEntferntKonto(t *testing.T) {
	env := newTestEnv(t)
	_, group, userID := setupGroupUser(t, env, "web02", "leaver")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "id -u leaver")

	env.Dialer.Reset()
	if err := env.LinuxUsers.RemoveFromGroup(repositories.ScopeAll(), userID, group.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "userdel -r leaver")
}

// Wer über eine zweite Zuordnung weiterhin berechtigt ist, darf sein Konto
// NICHT verlieren - sonst nähme das Lösen einer Gruppe jemandem den Zugang,
// den er über die direkte Zuordnung behalten sollte.
func TestWeiterhinBerechtigterBenutzerBehaeltKonto(t *testing.T) {
	env := newTestEnv(t)
	server, group, userID := setupGroupUser(t, env, "web03", "bleiber")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.Prov.AssignLinuxUserToServer(server, userID, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "id -u bleiber")

	env.Dialer.Reset()
	if err := env.LinuxUsers.RemoveFromGroup(repositories.ScopeAll(), userID, group.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	if len(env.recordedCommands()) == 0 {
		t.Fatal("kein Abgleich nach dem Lösen der Gruppe")
	}
	if env.ranCommand("userdel -r bleiber") {
		t.Errorf("Konto entfernt, obwohl direkt zugeordnet: %v", env.recordedCommands())
	}
}

// Verlässt ein Server die Gruppe, verliert er die Konten, zu denen ihn nur
// diese Gruppe berechtigt hat.
func TestServerVerlaesstGruppeUndVerliertKonto(t *testing.T) {
	env := newTestEnv(t)
	server, group, userID := setupGroupUser(t, env, "web04", "gastuser")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "id -u gastuser")

	env.Dialer.Reset()
	if err := env.Groups.RemoveServer(repositories.ScopeAll(), group.ID, server.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "userdel -r gastuser")
}

// Der Kern des Rückstands: Ein Server, der im Moment der Änderung nicht
// erreichbar ist, darf den Auftrag nicht verlieren. Er wird nachgeholt, sobald
// der Server wieder antwortet.
func TestNichtErreichbarerServerHoltDenAbgleichNach(t *testing.T) {
	env := newTestEnv(t)
	server, group, userID := setupGroupUser(t, env, "web05", "spaeter")

	env.Dialer.FailKey = errors.New("connection refused")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	// Der Fehlversuch steht am Eintrag - und der Eintrag bleibt liegen.
	env.settle()
	if entries, err := env.Pending.ForServer(server.ID); err != nil || len(entries) != 1 {
		t.Fatalf("Rückstand: %+v, %v", entries, err)
	}
	env.requireFailedEntry(t, server.ID, "")

	// Server wieder erreichbar: Der Rückstand wird abgearbeitet und geleert.
	env.Dialer.FailKey = nil
	env.Dialer.Reset()
	if _, err := env.Prov.DrainServer(server, "test"); err != nil {
		t.Fatalf("Nachholen fehlgeschlagen: %v", err)
	}
	if !env.ranCommand("id -u spaeter") {
		t.Errorf("Konto nicht nachträglich eingerichtet: %v", env.recordedCommands())
	}
	entries, err := env.Pending.ForServer(server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Rückstand nicht geleert: %+v", entries)
	}
}

// Der Abgleich von Hand zählt wie der Health-Check: Danach darf „Abgleich steht
// aus" nicht mehr stehen, nur weil niemand den Rückstand geleert hat.
func TestManuellerAbgleichLeertDenRueckstand(t *testing.T) {
	env := newTestEnv(t)
	server, group, userID := setupGroupUser(t, env, "web05", "spaeter")

	env.Dialer.FailKey = errors.New("connection refused")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireFailedEntry(t, server.ID, "")

	env.Dialer.FailKey = nil
	env.Dialer.Reset()
	if _, err := env.Prov.SyncUsers(server, "admin"); err != nil {
		t.Fatalf("Abgleich fehlgeschlagen: %v", err)
	}
	entries, err := env.Pending.ForServer(server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Rückstand nicht geleert: %+v", entries)
	}
}

// Auch ein entzogener Zugang darf nicht im Rückstand verhungern: Er ist der
// Grund, warum es den Rückstand überhaupt gibt.
func TestEntzogenerZugangBleibtImRueckstand(t *testing.T) {
	env := newTestEnv(t)
	server, group, userID := setupGroupUser(t, env, "web06", "weguser")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "id -u weguser")

	env.Dialer.FailKey = errors.New("host down")
	if err := env.LinuxUsers.RemoveFromGroup(repositories.ScopeAll(), userID, group.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireFailedEntry(t, server.ID, "weguser")

	env.Dialer.FailKey = nil
	env.Dialer.Reset()
	if _, err := env.Prov.DrainServer(server, "test"); err != nil {
		t.Fatalf("Nachholen fehlgeschlagen: %v", err)
	}
	if !env.ranCommand("userdel -r weguser") {
		t.Errorf("Konto nicht nachträglich entfernt: %v", env.recordedCommands())
	}
}

// Ein neuer SSH-Schlüssel muss ohne Zutun auf den Servern landen - sonst
// erklärt LCM dem Mitarbeiter einen Zugang, den es nicht eingerichtet hat.
func TestNeuerSchluesselWirdSofortVerteilt(t *testing.T) {
	env := newTestEnv(t)
	_, group, userID := setupGroupUser(t, env, "web07", "keyuser")
	if err := env.LinuxUsers.AssignToGroup(repositories.ScopeAll(), userID, group.ID, nil, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "id -u keyuser")

	env.Dialer.Reset()
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuvwxyz012345 keyuser@laptop"
	if _, err := env.LinuxUsers.AddSSHKey(userID, "laptop", key, "admin"); err != nil {
		t.Fatal(err)
	}
	env.settle()
	env.requireRan(t, "AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnopqrstuvwxyz012345")
}

// Eine Änderung, die während eines laufenden Abgleichs dazukommt, landet im
// schon offenen Eintrag. Der laufende Abgleich hat den alten Stand verteilt -
// er darf den Eintrag danach nicht löschen, sonst geht die Änderung verloren
// (CI-Flake: die Gruppen-Zuordnung kam nie auf dem Server an).
func TestErneutEingestellterAuftragUeberlebtDieLaufendeAbarbeitung(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web08")
	env.settle()

	if err := env.Pending.Queue(serverID, ""); err != nil {
		t.Fatal(err)
	}
	read, err := env.Pending.ForServer(serverID) // die laufende Abarbeitung liest
	if err != nil || len(read) != 1 {
		t.Fatalf("Rückstand: %+v, %v", read, err)
	}
	if err := env.Pending.Queue(serverID, ""); err != nil { // Änderung währenddessen
		t.Fatal(err)
	}
	if err := env.Pending.Clear(&read[0]); err != nil { // Abarbeitung fertig
		t.Fatal(err)
	}
	left, err := env.Pending.ForServer(serverID)
	if err != nil || len(left) != 1 {
		t.Fatalf("erneut eingestellter Auftrag verloren: %+v, %v", left, err)
	}

	if err := env.Pending.Clear(&left[0]); err != nil {
		t.Fatal(err)
	}
	if left, _ = env.Pending.ForServer(serverID); len(left) != 0 {
		t.Errorf("unveränderter Auftrag nicht gelöscht: %+v", left)
	}
}
