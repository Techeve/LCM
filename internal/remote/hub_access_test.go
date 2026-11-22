package remote_test

import (
	"log/slog"
	"net"
	"testing"
	"time"

	"LCM/internal/remote"
)

// Der Agent-Zugang aus (agent_enabled): SetAccepting(false) trennt die offenen
// Verbindungen und weist neue ab, auf welchem Weg sie auch kommen; wieder an,
// nimmt der Hub sie wieder an.
func TestSetAcceptingTrenntUndWeistAb(t *testing.T) {
	hub := remote.New(nil, slog.Default())
	if err := hub.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)

	open, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	ended := make(chan struct{})
	go func() { _ = hub.HandleConn(open); close(ended) }()
	if _, err := peer.Write([]byte{0x10}); err != nil { // Der Broker liest schon.
		t.Fatal(err)
	}

	hub.SetAccepting(false)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("die offene Verbindung wurde nicht getrennt")
	}

	refused, refusedPeer := net.Pipe()
	t.Cleanup(func() { _ = refusedPeer.Close() })
	if err := hub.HandleConn(refused); err == nil {
		t.Error("eine neue Verbindung wurde bei ausgeschaltetem Zugang angenommen")
	}
	if _, err := refusedPeer.Read(make([]byte, 1)); err == nil {
		t.Error("die abgewiesene Verbindung blieb offen")
	}

	hub.SetAccepting(true)
	again, againPeer := net.Pipe()
	t.Cleanup(func() { _ = againPeer.Close() })
	taken := make(chan error, 1)
	go func() { taken <- hub.HandleConn(again) }()
	select {
	case err := <-taken:
		t.Errorf("bei eingeschaltetem Zugang sofort beendet: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
}

// Für nil ist es ein No-op (kein Hub ohne LCM Remote).
func TestSetAcceptingNilIstHarmlos(t *testing.T) {
	var hub *remote.Hub
	hub.SetAccepting(false)
}
