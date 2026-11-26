package remote

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/remote/wire"
)

// Tests für die Korrekturen aus der Durchsicht von Paket B/C (23.11.): Ein Aufruf,
// dessen Kommando den Agent nie erreicht hat, wartet nicht 24 Stunden.

// inventoryJSON baut das Inventar eines Agents mit Jobs, der die IDs seiner
// Kommandos meldet (known == nil: er meldet sie nicht, wie ein älterer Stand).
func inventoryJSON(known []string) []byte {
	payload, _ := json.Marshal(wire.Inventory{AgentVersion: "test",
		Capabilities: []string{wire.CapabilityJobs}, KnownJobs: known})
	return payload
}

func requireFails(t *testing.T, done <-chan callOutcome, want error) {
	t.Helper()
	select {
	case r := <-done:
		if !errors.Is(r.err, want) {
			t.Fatalf("erwartet %v, bekommen %+v", want, r)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("der aufruf scheiterte nicht mit %v", want)
	}
}

// TestAufrufScheitertWennAgentKeineJobsMehrMeldet: Ein auf eine ältere Fassung
// zurückgesetzter Agent hat die alten Jobs nicht mehr; der wartende Aufruf
// scheitert sofort, statt 24 Stunden zu warten.
func TestAufrufScheitertWennAgentKeineJobsMehrMeldet(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "apt-get upgrade")
	a.command(t, 0)
	a.disconnect()
	requirePending(t, done, 200*time.Millisecond)

	a.connect(false) // Rollback: kein jobs-v2 mehr
	requireFails(t, done, errAgentDowngraded)
}

// TestAufrufScheitertBeiVerbundenemAgentOhneLebenszeichen: Ein Kommando, das den
// verbundenen Agent nie erreichte (Verbindung riss beim Senden ab), scheitert
// nach der Lebenszeichen-Frist, ohne dass etwas abgebrochen wird. Ein Aufruf, zu
// dem Lebenszeichen kommen, lebt weiter.
func TestAufrufScheitertBeiVerbundenemAgentOhneLebenszeichen(t *testing.T) {
	hub, server, _ := newTestHub(t)
	hub.WithJobSilence(400 * time.Millisecond)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	lost := runAsync(conn, "nie angekommen")
	a.command(t, 0)
	conn2, _ := hub.Conn(server)
	defer func() { _ = conn2.Close() }()
	alive := runAsync(conn2, "läuft")
	cmd := a.command(t, 1)

	for range 8 { // 800 ms, doppelt so lang wie die Frist
		time.Sleep(100 * time.Millisecond)
		a.send(t, wire.Result{ID: cmd.ID, Progress: true})
	}
	requireFails(t, lost, ErrCommandUnknown)
	requirePending(t, alive, 100*time.Millisecond)
	a.send(t, wire.Result{ID: cmd.ID, Output: "fertig"})
	if r := <-alive; r.err != nil || r.out != "fertig" {
		t.Fatalf("ergebnis des lebenden aufrufs: %+v", r)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.cmds {
		if c.Cancel {
			t.Errorf("LCM hielt etwas an: %+v", c)
		}
	}
}

// TestGetrennterAgentBleibtBeiDerLangenFrist: Bei getrenntem Agent gilt nur
// maxSilence; die kurze Frist zählt erst nach der Rückkehr.
func TestGetrennterAgentBleibtBeiDerLangenFrist(t *testing.T) {
	hub, server, _ := newTestHub(t)
	hub.WithJobSilence(100 * time.Millisecond)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "lang")
	a.command(t, 0)
	a.disconnect()
	requirePending(t, done, 600*time.Millisecond) // sechsfache Frist, Agent getrennt

	a.connect(true) // Rückkehr: die Frist läuft von hier an
	requirePending(t, done, 50*time.Millisecond)
	requireFails(t, done, ErrCommandUnknown)
}

// TestInventarLaesstUnbekannteAufrufeSofortScheitern: Meldet der Agent nach der
// Rückkehr seine Kommandos, scheitert jeder Aufruf, den LCM vor der Verbindung
// schickte und der dort fehlt - sofort. Bekannte, solche von nach dem
// Verbindungsaufbau und die eines Agents, der nichts meldet, bleiben.
func TestInventarLaesstUnbekannteAufrufeSofortScheitern(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, true)
	call := func(cmd string) (<-chan callOutcome, wire.Command) {
		conn, _ := hub.Conn(server)
		t.Cleanup(func() { _ = conn.Close() })
		a.mu.Lock()
		n := len(a.cmds)
		a.mu.Unlock()
		done := runAsync(conn, cmd)
		return done, a.command(t, n)
	}
	unknown, _ := call("kam nicht an")
	known, knownCmd := call("läuft dort")
	a.disconnect()

	a.connectWith(inventoryJSON(nil)) // älterer Stand: meldet nichts
	requirePending(t, unknown, 200*time.Millisecond)
	requirePending(t, known, 50*time.Millisecond)

	a.connectWith(inventoryJSON([]string{knownCmd.ID}))
	requireFails(t, unknown, ErrCommandUnknown)
	requirePending(t, known, 200*time.Millisecond)

	// Ein Aufruf von nach dem Verbindungsaufbau kann den Agent schon erreicht haben,
	// bevor dieser sein Inventar zusammenstellte.
	fresh, _ := call("frisch")
	a.hub.handleInventory(nil, packets.Subscription{},
		packets.Packet{TopicName: wire.TopicInv(a.id), Payload: inventoryJSON([]string{knownCmd.ID})})
	requirePending(t, fresh, 200*time.Millisecond)

	a.send(t, wire.Result{ID: knownCmd.ID, Output: "ok"})
	if r := <-known; r.err != nil || r.out != "ok" {
		t.Fatalf("ergebnis des bekannten aufrufs: %+v (%v)", r, r.err)
	}
}
