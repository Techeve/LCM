package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"LCM/internal/remote/wire"
)

// fakeMQTT nimmt die Ergebnisse des Agents entgegen, statt sie zu senden.
type fakeMQTT struct {
	mqtt.Client
	mu      sync.Mutex
	results []wire.Result
}

func (f *fakeMQTT) Publish(_ string, _ byte, _ bool, payload any) mqtt.Token {
	var res wire.Result
	if b, ok := payload.([]byte); ok && json.Unmarshal(b, &res) == nil && !res.Progress {
		f.mu.Lock()
		f.results = append(f.results, res)
		f.mu.Unlock()
	}
	return &mqtt.DummyToken{}
}

func (f *fakeMQTT) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.results)
}

func testClient() (*Client, *fakeMQTT) {
	fake := &fakeMQTT{}
	c := NewClient(&Config{AgentID: "agent-1"}, nil, "test")
	c.mqtt = fake
	return c, fake
}

func commandPayload(t *testing.T, cmd wire.Command) []byte {
	t.Helper()
	b, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func waitResults(t *testing.T, f *fakeMQTT, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d ergebnisse erwartet, %d da", n, f.count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHandleCommandFuehrtDoppelteIDNurEinmalAus: Kommt dasselbe Kommando ein
// zweites Mal an - während es läuft und nachdem es fertig ist -, läuft es
// trotzdem nur einmal. Vorher hängte jede Zustellung eine Zeile an.
func TestHandleCommandFuehrtDoppelteIDNurEinmalAus(t *testing.T) {
	c, fake := testClient()
	log := filepath.Join(t.TempDir(), "laeufe")
	payload := commandPayload(t, wire.Command{ID: "dup", Cmd: "echo lauf >> " + log + "; sleep 0.3"})

	c.handleCommand(payload)
	time.Sleep(50 * time.Millisecond)
	c.handleCommand(payload) // während des Laufs
	waitResults(t, fake, 1)
	c.handleCommand(payload) // nach dem Lauf
	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if runs := strings.Count(string(data), "lauf"); runs != 1 {
		t.Fatalf("kommando %d-mal gelaufen", runs)
	}
	if fake.count() != 1 {
		t.Fatalf("%d ergebnisse gesendet", fake.count())
	}
}

// TestHandleCommandAbbruchVorDemKommando: Trifft der Abbruch vor dem Kommando
// ein, läuft es nicht.
func TestHandleCommandAbbruchVorDemKommando(t *testing.T) {
	c, fake := testClient()
	marker := filepath.Join(t.TempDir(), "gelaufen")
	c.handleCommand(commandPayload(t, wire.Command{ID: "spaet", Cancel: true}))
	c.handleCommand(commandPayload(t, wire.Command{ID: "spaet", Cmd: "touch " + marker}))
	waitResults(t, fake, 1)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("kommando lief trotz vorherigem abbruch")
	}
}

// TestHandleCommandZaehltArbeitBisZurZustellung: Solange ein Kommando läuft,
// wechselt der Agent nicht aufs neue Binary.
func TestHandleCommandZaehltArbeitBisZurZustellung(t *testing.T) {
	c, fake := testClient()
	c.handleCommand(commandPayload(t, wire.Command{ID: "lang", Cmd: "sleep 0.3"}))
	time.Sleep(50 * time.Millisecond)
	if c.work.claimIdle(0) {
		t.Fatal("wechsel während eines laufenden kommandos")
	}
	waitResults(t, fake, 1)
	deadline := time.Now().Add(2 * time.Second)
	for !c.work.claimIdle(0) {
		if time.Now().After(deadline) {
			t.Fatal("nach dem kommando kein wechsel möglich")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
