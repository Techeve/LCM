package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestUmbenennenAendertDenNamen: Die Möglichkeit fehlte in der Oberfläche
// vollständig; die Schnittstelle konnte es bereits. Der Test hält fest, dass
// sie es auch weiterhin tut - samt Blindindex, über den der Name gesucht wird.
func TestUmbenennenAendertDenNamen(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	if _, err := env.Servers.UpdateSettings(repositories.ScopeAll(), id,
		services.ServerSettingsInput{Name: "web01-neu"}, "admin"); err != nil {
		t.Fatalf("umbenennen: %v", err)
	}

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.Name != "web01-neu" {
		t.Errorf("Name nicht geändert: %q", server.Name)
	}
	// Der Blindindex muss mitgezogen sein - sonst fände die Suche den Server
	// weiter unter dem ALTEN Namen und unter dem neuen gar nicht. Geprüft über
	// den Weg, der ihn benutzt: Ein zweiter Server darf jetzt "web01" heißen,
	// nicht aber "web01-neu".
	if _, err := env.Servers.UpdateSettings(repositories.ScopeAll(),
		joinTestServer(t, env, "db01"), services.ServerSettingsInput{Name: "web01"}, "admin"); err != nil {
		t.Errorf("der freigewordene Name wird weiter blockiert: %v", err)
	}
}

// TestUmbenennenAufVergebenenNamenScheitertSauber ist die eigentliche Lücke:
// Auf dem Namens-Blindindex liegt ein Unique-Index, geprüft wurde er beim
// Anlegen aber nur dort. Über die Einstellungen lief ein Namensdoppel bis in
// die Datenbank und kam als roher Constraint-Fehler zurück.
func TestUmbenennenAufVergebenenNamenScheitertSauber(t *testing.T) {
	env := newTestEnv(t)
	ersteID := joinTestServer(t, env, "web01")
	joinTestServer(t, env, "db01")

	_, err := env.Servers.UpdateSettings(repositories.ScopeAll(), ersteID,
		services.ServerSettingsInput{Name: "db01"}, "admin")
	if !errors.Is(err, services.ErrServerNameTaken) {
		t.Fatalf("erwartet ErrServerNameTaken, bekam %v", err)
	}

	// Und der alte Name muss unangetastet geblieben sein.
	server, err := env.Servers.Get(repositories.ScopeAll(), ersteID)
	if err != nil {
		t.Fatal(err)
	}
	if server.Name != "web01" {
		t.Errorf("nach dem abgelehnten Umbenennen heißt der Server %q", server.Name)
	}
}

// TestUmbenennenAufDenEigenenNamenIstKeinFehler: Speichert jemand den Dialog,
// ohne den Namen anzufassen, darf das nicht als Namensdoppel gelten.
func TestUmbenennenAufDenEigenenNamenIstKeinFehler(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	if _, err := env.Servers.UpdateSettings(repositories.ScopeAll(), id,
		services.ServerSettingsInput{Name: "web01", Port: 22}, "admin"); err != nil {
		t.Errorf("derselbe Name wurde abgelehnt: %v", err)
	}
}

// TestHostnameWirdErfasst: Der Hostname ist der Name, unter dem sich das
// System SELBST kennt - er kann von Anzeigename und Adresse abweichen, und
// genau darin liegt sein Nutzen.
func TestHostnameWirdErfasst(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)
	env.Dialer.Responses["hostnamectl --static"] = sshx.FakeResponse{Output: "web01.intern\n"}

	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.Hostname != "web01.intern" {
		t.Errorf("Hostname nicht erfasst: %q", server.Hostname)
	}
}

// TestHostnameUeberlebtEinenGestoertenScan: Bricht das Kommando weg, liefert
// der Scan einen leeren Wert. Der darf den einmal erfassten Namen nicht
// löschen - dieselbe Regel wie bei os_id (siehe scanFields).
func TestHostnameUeberlebtEinenGestoertenScan(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)
	env.Dialer.Responses["hostnamectl --static"] = sshx.FakeResponse{Output: "web01.intern\n"}
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	// Jetzt bricht die Erhebung weg.
	env.Dialer.Responses["hostnamectl --static"] = sshx.FakeResponse{Output: "", ExitCode: 1}
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.Hostname != "web01.intern" {
		t.Errorf("der Hostname wurde von einem leeren Scan überschrieben: %q", server.Hostname)
	}
}

// TestSchluesselaustauschWirdErfasst geht den ganzen Weg: Die Verbindung
// meldet ihr ausgehandeltes Verfahren, der Scan schreibt es weg, die
// Bewertung liest es zurück.
//
// Der Wert stammt als einziger NICHT aus einem Kommando, sondern aus der
// Verbindung selbst - ein Grund mehr, ihn eigens zu prüfen.
func TestSchluesselaustauschWirdErfasst(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)
	env.Dialer.Kex = "curve25519-sha256"

	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.KexAlgorithm != "curve25519-sha256" {
		t.Fatalf("Verfahren nicht erfasst: %q", server.KexAlgorithm)
	}

	// Und es muss als Hinweis ankommen - klassisch heißt: mitgeschnittener
	// Verkehr wäre später lesbar.
	_, hinweise, _, err := env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	var gefunden bool
	for _, h := range hinweise {
		if h.Key == "kexClassic" {
			gefunden = true
		}
	}
	if !gefunden {
		t.Error("die klassische Verbindung erzeugt keinen Hinweis")
	}

	// Nach einem Upgrade der Gegenstelle verschwindet der Hinweis wieder.
	env.Dialer.Kex = "mlkem768x25519-sha256"
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")
	_, hinweise, _, err = env.Servers.Status(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hinweise {
		if h.Key == "kexClassic" {
			t.Error("der Hinweis steht noch, obwohl die Verbindung jetzt quantensicher ist")
		}
	}
}

// TestAgentServerWirdNichtBewertet: Der Agent-Transport läuft über MQTT und
// hat keinen SSH-Handshake. Ein leerer Wert darf den zuletzt erfassten nicht
// löschen - sonst verlöre ein Server seine Aussage, sobald er auf den Agenten
// umgestellt wird.
func TestAgentServerWirdNichtBewertet(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	vollstaendigeErkennung(env)
	env.Dialer.Kex = "mlkem768x25519-sha256"
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	env.Dialer.Kex = "" // meldet nichts mehr
	env.Executor.RunRule(findSystemSyncRule(t, env), "admin")

	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if server.KexAlgorithm != "mlkem768x25519-sha256" {
		t.Errorf("der erfasste Wert wurde von einer stummen Verbindung überschrieben: %q", server.KexAlgorithm)
	}
}
