package services

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Die Warteschleife nach einem Neustart, direkt geprüft - ohne Job, Datenbank
// und ohne einen Server tatsächlich neu zu starten.

// TestPollRebootReturnWartetBisDerServerAntwortet: Solange der Server stumm
// bleibt, wird weiter angeklopft; meldet er sich, endet die Schleife mit der
// Wartedauer statt mit einem Fehler.
func TestPollRebootReturnWartetBisDerServerAntwortet(t *testing.T) {
	attempts := 0
	dial := func() error {
		attempts++
		if attempts < 4 {
			return errors.New("connection refused")
		}
		return nil
	}

	waited, err := pollRebootReturn(dial, time.Millisecond, time.Millisecond, 2*time.Second)
	if err != nil {
		t.Fatalf("der server hat geantwortet, das darf kein fehler sein: %v", err)
	}
	if attempts != 4 {
		t.Errorf("erwartet 4 versuche bis zur antwort, bekam %d", attempts)
	}
	if waited < 0 {
		t.Errorf("die wartedauer sollte messbar sein, bekam %v", waited)
	}
}

// TestPollRebootReturnMeldetZeitueberschreitung: Kommt der Server nicht
// zurück, endet die Schleife mit einem Fehler, der das Zeitfenster UND den
// letzten Verbindungsfehler nennt - ein bloßes „fehlgeschlagen" ließe offen,
// ob überhaupt jemand nachgesehen hat.
func TestPollRebootReturnMeldetZeitueberschreitung(t *testing.T) {
	dial := func() error { return errors.New("no route to host") }

	_, err := pollRebootReturn(dial, time.Millisecond, time.Millisecond, 30*time.Millisecond)
	if err == nil {
		t.Fatal("ein server, der nicht zurückkommt, muss einen fehler ergeben")
	}
	if !strings.Contains(err.Error(), "nicht wieder erreichbar") {
		t.Errorf("die meldung nennt den vorfall nicht: %v", err)
	}
	if !strings.Contains(err.Error(), "no route to host") {
		t.Errorf("der letzte verbindungsfehler fehlt: %v", err)
	}
}

// TestPollRebootReturnHaeltDieAnlaufsperreEin: Die Anlaufsperre ist der
// Grund, warum die Schleife überhaupt verlässlich ist - ohne sie träfe der
// erste Versuch den noch laufenden Server und meldete einen Neustart als
// abgeschlossen, der noch gar nicht begonnen hat.
func TestPollRebootReturnHaeltDieAnlaufsperreEin(t *testing.T) {
	var firstAttempt time.Time
	dial := func() error {
		if firstAttempt.IsZero() {
			firstAttempt = time.Now()
		}
		return nil
	}

	settle := 40 * time.Millisecond
	start := time.Now()
	if _, err := pollRebootReturn(dial, settle, time.Millisecond, time.Second); err != nil {
		t.Fatalf("unerwarteter fehler: %v", err)
	}
	if waited := firstAttempt.Sub(start); waited < settle {
		t.Errorf("der erste versuch kam nach %v - die anlaufsperre von %v wurde nicht eingehalten", waited, settle)
	}
}

// TestPollRebootReturnVersuchtMindestensEinmal: Auch bei einem Zeitfenster
// von null wird angeklopft, bevor aufgegeben wird. Sonst hinge das Ergebnis
// an der Reihenfolge zweier Zeitvergleiche statt am Server.
func TestPollRebootReturnVersuchtMindestensEinmal(t *testing.T) {
	attempts := 0
	dial := func() error {
		attempts++
		return nil
	}
	if _, err := pollRebootReturn(dial, 0, time.Millisecond, 0); err != nil {
		t.Fatalf("unerwarteter fehler: %v", err)
	}
	if attempts == 0 {
		t.Error("es wurde kein einziger verbindungsversuch unternommen")
	}
}
