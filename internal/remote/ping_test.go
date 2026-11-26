package remote

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/agentcaps"
	"LCM/internal/remote/wire"
)

// TestPingsKommenAnUndDasPongWirdVerbucht: Ein echter Agent meldet die
// Capability "ping", LCM pingt, der Agent antwortet, LCM merkt sich die Zeit.
func TestPingsKommenAnUndDasPongWirdVerbucht(t *testing.T) {
	if testing.Short() {
		t.Skip("echter broker, echte wartezeiten")
	}
	e := newChaosEnvPinging(t, 200*time.Millisecond)
	a := e.newAgent()
	a.startReady(true)

	var first time.Time
	waitUntil(t, 5*time.Second, "erstes pong", func() bool {
		at, ok := e.currentHub().LastPong(a.server.AgentID)
		first = at
		return ok
	})
	waitUntil(t, 5*time.Second, "weiteres pong", func() bool {
		at, _ := e.currentHub().LastPong(a.server.AgentID)
		return at.After(first)
	})
}

// TestPingsNurAnAgentenMitCapability: Ein Agent ohne "ping" (etwa 2.0.0)
// bekommt nie eine Nachricht, die er nicht kennt.
func TestPingsNurAnAgentenMitCapability(t *testing.T) {
	hub := New(nil, quiet).WithPingInterval(50 * time.Millisecond)
	if err := hub.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)

	var old, fresh atomic.Int32
	count := func(n *atomic.Int32) mqtt.InlineSubFn {
		return func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
			var cmd wire.Command
			if json.Unmarshal(pk.Payload, &cmd) == nil && cmd.Ping {
				n.Add(1)
			}
		}
	}
	if err := hub.broker.Subscribe(wire.TopicCmd("alt"), 1, count(&old)); err != nil {
		t.Fatal(err)
	}
	if err := hub.broker.Subscribe(wire.TopicCmd("neu"), 2, count(&fresh)); err != nil {
		t.Fatal(err)
	}
	// Beide gelten als verbunden; Inventar ohne bzw. mit "ping".
	hub.mu.Lock()
	hub.online["alt"] = &mqtt.Client{}
	hub.online["neu"] = &mqtt.Client{}
	hub.mu.Unlock()
	hub.startPinging("alt", agentcaps.Agent{Capabilities: []string{wire.CapabilityTTY}})
	hub.startPinging("neu", agentcaps.Agent{Capabilities: []string{wire.CapabilityTTY, wire.CapabilityPing}})

	waitUntil(t, 3*time.Second, "pings an den neuen agent", func() bool { return fresh.Load() >= 3 })
	if n := old.Load(); n != 0 {
		t.Fatalf("agent ohne capability hat %d pings bekommen", n)
	}
}

// TestPongOhneIDStoertNicht: Das Pong hat keine Request-ID und darf weder einen
// Warteplatz berühren noch als Ergebnis gelten.
func TestPongOhneIDStoertNicht(t *testing.T) {
	hub := New(nil, quiet)
	if err := hub.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)
	pong, _ := json.Marshal(wire.Result{Pong: true})
	if err := hub.broker.Publish(wire.TopicRes("x"), pong, false, 1); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, "pong verbucht", func() bool {
		_, ok := hub.LastPong("x")
		return ok
	})
}
