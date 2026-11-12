package remote

import (
	"encoding/json"
	"testing"

	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/remote/wire"
)

// TestInventarTraegtSchluesselPin: Der Hub hält fest, welchen Schlüssel- und
// CA-Pin der Agent führt - und dass ein älterer Agent keine meldet. Daran
// hängt, wie LCM sein Zertifikat erneuern darf, ohne Agenten auszusperren.
func TestInventarTraegtSchluesselPin(t *testing.T) {
	hub, server, _ := newTestHub(t)
	send := func(inv wire.Inventory) (string, string) {
		t.Helper()
		payload, _ := json.Marshal(inv)
		hub.handleInventory(nil, packets.Subscription{}, packets.Packet{
			TopicName: wire.TopicInv(server.AgentID), Payload: payload,
		})
		got, err := hub.servers.FindByAgentID(server.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		return got.AgentKeyPin, got.AgentCAPin
	}
	if key, ca := send(wire.Inventory{AgentVersion: "2.1.0", KeyPin: "abc123", CAPin: "ca0001"}); key != "abc123" || ca != "ca0001" {
		t.Errorf("Pins %q/%q, erwartet abc123/ca0001", key, ca)
	}
	if key, ca := send(wire.Inventory{AgentVersion: "2.0.0"}); key != "" || ca != "" {
		t.Errorf("alter Agent ohne Pins, gespeichert bleiben %q/%q", key, ca)
	}
}
