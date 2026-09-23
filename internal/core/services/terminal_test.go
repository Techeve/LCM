package services_test

import (
	"errors"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// schalteKonsole legt den globalen Schalter der Web-Konsole um.
func schalteKonsole(t *testing.T, env *testEnv, an bool) {
	t.Helper()
	if err := env.DB().Model(&domain.GlobalSettings{}).Where("id = 1").
		Update("terminal_enabled", an).Error; err != nil {
		t.Fatalf("Konsolen-Schalter: %v", err)
	}
}

// TestKonsoleFolgtDemGlobalenSchalter: Der Betreiber muss die Fähigkeit ganz
// aus dem Haus nehmen können - dann führt auch ein versehentlich vergebenes
// Recht zu nichts.
func TestKonsoleFolgtDemGlobalenSchalter(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	schalteKonsole(t, env, false)
	if _, err := env.Servers.OpenTerminal(repositories.ScopeAll(), id, "admin", "", 80, 24); !errors.Is(err, services.ErrTerminalDisabled) {
		t.Errorf("bei abgeschalteter Konsole erwartet ErrTerminalDisabled, bekam %v", err)
	}
}

// TestKonsoleJeServerAbschaltbar: Der globale Schalter nimmt die Fähigkeit
// aus dem ganzen Haus, die Berechtigung regelt WER sie benutzt - hier geht es
// um einzelne Maschinen, auf denen ein Shell-Zugriff aus der Oberfläche nicht
// erwünscht ist.
func TestKonsoleJeServerAbschaltbar(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	schalteKonsole(t, env, true)

	// Vorbedingung: ohne Sperre geht es (der Fake-Server nimmt die Sitzung
	// nicht an, aber die Prüfung davor muss durchlaufen).
	if _, err := env.Servers.OpenTerminal(repositories.ScopeAll(), id, "admin", "", 80, 24); errors.Is(err, services.ErrTerminalNotPossible) {
		t.Fatal("Vorbedingung: die Konsole ist schon ohne Sperre nicht möglich")
	}

	if err := env.Servers.SetConsoleDisabled(repositories.ScopeAll(), id, true, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.OpenTerminal(repositories.ScopeAll(), id, "admin", "", 80, 24); !errors.Is(err, services.ErrTerminalNotPossible) {
		t.Errorf("bei gesperrter Konsole erwartet ErrTerminalNotPossible, bekam %v", err)
	}

	// Und wieder freischalten.
	if err := env.Servers.SetConsoleDisabled(repositories.ScopeAll(), id, false, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.OpenTerminal(repositories.ScopeAll(), id, "admin", "", 80, 24); errors.Is(err, services.ErrTerminalNotPossible) {
		t.Error("nach dem Freischalten bleibt die Konsole gesperrt")
	}
}

// TestKonsoleNichtAufJedemServertyp: Wo es keine Shell gibt, soll eine klare
// Absage kommen statt eines Verbindungsversuchs, der ins Leere läuft.
func TestKonsoleNichtAufJedemServertyp(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	schalteKonsole(t, env, true)

	faelle := []struct {
		name string
		feld string
		wert any
	}{
		{"Demo-Server", "is_demo", true},
		{"Synology DSM", "os_id", domain.OSIDSynologyDSM},
		{"in Wartung", "maintenance", true},
	}
	for _, f := range faelle {
		if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
			Update(f.feld, f.wert).Error; err != nil {
			t.Fatal(err)
		}
		_, err := env.Servers.OpenTerminal(repositories.ScopeAll(), id, "admin", "", 80, 24)
		if !errors.Is(err, services.ErrTerminalNotPossible) {
			t.Errorf("%s: erwartet ErrTerminalNotPossible, bekam %v", f.name, err)
		}
		// Zurücksetzen für den nächsten Fall.
		if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
			Update(f.feld, map[string]any{"is_demo": false, "maintenance": false, "os_id": "debian"}[f.feld]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// TestFahrkarteGiltNurEinmal: Sie ist der Nachweis anstelle des Anmelde-Tokens,
// den der WebSocket nicht mitschicken kann. Ein zweites Einlösen muss
// scheitern - sonst wäre sie ein Dauerschlüssel, sobald sie einmal in einem
// Protokoll steht.
func TestFahrkarteGiltNurEinmal(t *testing.T) {
	tickets := services.NewTerminalTickets()

	token, err := tickets.Issue("admin", 7, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if actor, ok := tickets.Redeem(token, 7, "203.0.113.9"); !ok || actor != "admin" {
		t.Fatalf("erstes Einlösen fehlgeschlagen: actor=%q ok=%v", actor, ok)
	}
	if _, ok := tickets.Redeem(token, 7, "203.0.113.9"); ok {
		t.Error("die Fahrkarte wurde ein zweites Mal angenommen")
	}
}

// TestFahrkarteGiltNurFuerIhrenServer: Ohne die Bindung könnte man sich mit
// der Fahrkarte für einen unwichtigen Server eine Shell auf einem wichtigen
// holen.
func TestFahrkarteGiltNurFuerIhrenServer(t *testing.T) {
	tickets := services.NewTerminalTickets()

	token, err := tickets.Issue("admin", 7, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tickets.Redeem(token, 8, "203.0.113.9"); ok {
		t.Error("die Fahrkarte galt für einen fremden Server")
	}
	// Und sie ist dadurch nicht verbraucht - der richtige Server geht noch.
	if _, ok := tickets.Redeem(token, 7, "203.0.113.9"); !ok {
		t.Error("ein Fehlversuch auf einem fremden Server hat die Fahrkarte entwertet")
	}
}

// TestUnbekannteFahrkarteWirdAbgewiesen: die Gegenprobe zum Raten.
func TestUnbekannteFahrkarteWirdAbgewiesen(t *testing.T) {
	tickets := services.NewTerminalTickets()
	if _, ok := tickets.Redeem("ausgedacht", 1, "203.0.113.9"); ok {
		t.Error("eine erfundene Fahrkarte wurde angenommen")
	}
}

// TestKonsolenRechtIstNichtImVerwalterRecht: Wer Server konfigurieren darf,
// bekommt damit NICHT automatisch eine Root-Shell auf allen.
func TestKonsolenRechtIstNichtImVerwalterRecht(t *testing.T) {
	for _, p := range domain.ManagerPermissions() {
		if p == domain.PermServersConsole {
			t.Fatal("servers:console steckt in den Verwalter-Rechten - es gehört allein zu admin")
		}
	}
}

// TestFahrkarteGiltNurVonIhrerAdresse: Eine mitgelesene Fahrkarte (Proxy-Log,
// Schulterblick) nützt von einem anderen Rechner aus nichts.
func TestFahrkarteGiltNurVonIhrerAdresse(t *testing.T) {
	tickets := services.NewTerminalTickets()
	token, err := tickets.Issue("admin", 7, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tickets.Redeem(token, 7, "198.51.100.7"); ok {
		t.Error("die Fahrkarte galt von einer fremden Adresse")
	}
	if _, ok := tickets.Redeem(token, 7, "203.0.113.9"); !ok {
		t.Error("von der eigenen Adresse muss sie gelten")
	}
}
