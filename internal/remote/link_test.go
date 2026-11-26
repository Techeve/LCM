package remote

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/domain"
)

func testBook() (*linkBook, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}
	book := newLinkBook()
	book.now = clock.now
	return book, clock
}

func TestClassifyDrop(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"sauber ohne Fehler", nil, domain.DropAgent},
		{"DISCONNECT des Agents", packets.CodeDisconnect, domain.DropAgent},
		{"LCM wird beendet", packets.ErrServerShuttingDown, domain.DropLCM},
		{"Keepalive abgelaufen", fmt.Errorf("read: %w", os.ErrDeadlineExceeded), domain.DropPing},
		{"Netz-Timeout", &net.OpError{Op: "read", Err: timeoutErr{}}, domain.DropPing},
		{"Verbindung abgerissen", io.EOF, domain.DropNetwork},
		{"Verbindung zurückgesetzt", errors.New("read: connection reset by peer"), domain.DropNetwork},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyDrop(tt.err); got != tt.want {
				t.Fatalf("classifyDrop(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }

func TestBilanzZaehltVerbindungenUndTrennungen(t *testing.T) {
	book, _ := testBook()
	book.connected("a", false)
	book.dropped("a", packets.CodeDisconnect)
	book.connected("a", false)
	book.dropped("a", io.EOF)

	got := book.snapshot("a")
	if got.Connects != 2 || got.Disconnects != 2 {
		t.Fatalf("Zähler = %+v", got)
	}
	// Nur der Netzfehler war ungewollt, der Abschied des Agents nicht.
	if got.UnintendedLastHour != 1 || got.LastCause != domain.DropNetwork || got.LastCauseAt == nil {
		t.Fatalf("ungewollt = %+v", got)
	}
	if other := book.snapshot("b"); other != (domain.AgentLink{}) {
		t.Fatalf("fremder Agent hat Zähler: %+v", other)
	}
}

// Mehr als fünf ungewollte Trennungen in einer Stunde machen die Verbindung
// instabil; mit der Zeit fallen die alten aus dem Fenster.
func TestSchwelleFuenfProStunde(t *testing.T) {
	book, clock := testBook()
	for i := range 5 {
		book.connected("a", false)
		book.dropped("a", io.EOF)
		clock.advance(time.Minute)
		if link := book.snapshot("a"); link.Unstable() {
			t.Fatalf("nach %d Abbrüchen schon instabil", i+1)
		}
	}
	book.connected("a", false)
	book.dropped("a", fmt.Errorf("x: %w", os.ErrDeadlineExceeded))
	link := book.snapshot("a")
	if !link.Unstable() || link.UnintendedLastHour != 6 || link.LastCause != domain.DropPing {
		t.Fatalf("sechster Abbruch: %+v", link)
	}

	// Eine Stunde nach den ersten fünf bleibt nur der sechste im Fenster.
	clock.advance(domain.AgentLinkWindow - 30*time.Second)
	link = book.snapshot("a")
	if link.Unstable() || link.UnintendedLastHour != 1 {
		t.Fatalf("nach einer Stunde: %+v", link)
	}
	// Die Summe seit dem Start bleibt stehen.
	if link.Disconnects != 6 {
		t.Fatalf("Gesamtzahl = %d, want 6", link.Disconnects)
	}
	clock.advance(time.Hour)
	if link = book.snapshot("a"); link.UnintendedLastHour != 0 || link.LastCause != "" || link.LastCauseAt != nil {
		t.Fatalf("nach zwei Stunden: %+v", link)
	}
}

func TestGewollteTrennungenZaehlenNichtAlsUngewollt(t *testing.T) {
	book, _ := testBook()
	for _, cause := range []string{domain.DropRevoked, domain.DropAccessOff, domain.DropLCM} {
		book.connected("a", false)
		book.expect("a", cause)
		// Der Broker meldet das eigene Trennen als Netzfehler; die vermerkte
		// Ursache gilt.
		if got := book.dropped("a", io.EOF); got != cause {
			t.Fatalf("Ursache = %q, want %q", got, cause)
		}
	}
	link := book.snapshot("a")
	if link.Disconnects != 3 || link.UnintendedLastHour != 0 {
		t.Fatalf("Bilanz = %+v", link)
	}
}

// Ein vermerkter Grund gilt nur für die Verbindung, die LCM trennte: Verbindet
// sich der Agent neu, bevor der Hook kam, verfällt er.
func TestVermerkteUrsacheVerfaelltBeimNeuenConnect(t *testing.T) {
	book, _ := testBook()
	book.expect("a", domain.DropRevoked)
	book.connected("a", false)
	if got := book.dropped("a", io.EOF); got != domain.DropNetwork {
		t.Fatalf("Ursache = %q", got)
	}
}

func TestUebernahmeZaehltAlsUngewollt(t *testing.T) {
	book, _ := testBook()
	book.connected("a", false)
	book.connected("a", true) // neue Verbindung, bevor die alte als tot galt
	link := book.snapshot("a")
	if link.Connects != 2 || link.UnintendedLastHour != 1 || link.LastCause != domain.DropTakeover {
		t.Fatalf("Bilanz = %+v", link)
	}
}

func TestBilanzBegrenztSpeicher(t *testing.T) {
	book, _ := testBook()
	for range 3 * maxKeptDrops {
		book.dropped("a", io.EOF)
	}
	if n := len(book.agents["a"].unintended); n != maxKeptDrops {
		t.Fatalf("gemerkt = %d, want %d", n, maxKeptDrops)
	}
}

// Der Hub verbucht Verbindung, Übernahme und Trennung über seine Hooks.
func TestHubFuehrtBilanz(t *testing.T) {
	hub, server, _ := newTestHub(t)
	first := hub.broker.NewClient(nil, "test", server.AgentID, false)
	second := hub.broker.NewClient(nil, "test", server.AgentID, false)

	hub.agentConnected(first)
	hub.agentConnected(second) // Übernahme
	hub.agentDisconnected(first, nil)
	if link := hub.LinkStats(server.AgentID); link.Connects != 2 || link.Disconnects != 1 || link.LastCause != domain.DropTakeover {
		t.Fatalf("nach Übernahme: %+v (der alte Client darf nicht noch einmal zählen)", link)
	}
	hub.agentDisconnected(second, io.EOF)
	link := hub.LinkStats(server.AgentID)
	if link.Disconnects != 2 || link.UnintendedLastHour != 2 || link.LastCause != domain.DropNetwork {
		t.Fatalf("nach Netzfehler: %+v", link)
	}
}

// rawConnect öffnet über HandleConn eine MQTT-Verbindung ohne paho: So lässt sich
// der Abbruch ohne Abschied und das Verstummen nachstellen. keepalive in
// Sekunden; zurück kommt das Ende der Leitung.
func rawConnect(t *testing.T, hub *Hub, agentID, secret string, keepalive uint16) net.Conn {
	t.Helper()
	pk := packets.Packet{
		FixedHeader:     packets.FixedHeader{Type: packets.Connect},
		ProtocolVersion: 4,
		Connect: packets.ConnectParams{
			ProtocolName: []byte("MQTT"), Clean: true, Keepalive: keepalive, ClientIdentifier: agentID,
			UsernameFlag: true, Username: []byte(agentID), PasswordFlag: true, Password: []byte(secret),
		},
	}
	var buf bytes.Buffer
	if err := pk.ConnectEncode(&buf); err != nil {
		t.Fatal(err)
	}
	server, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	go func() { _ = hub.HandleConn(server) }()
	go func() { _, _ = io.Copy(io.Discard, peer) }() // CONNACK und alles Weitere lesen
	if _, err := peer.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	return peer
}

func waitLink(t *testing.T, hub *Hub, agentID string, cond func(domain.AgentLink) bool) domain.AgentLink {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if link := hub.LinkStats(agentID); cond(link) {
			return link
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Bilanz erreichte den Zustand nicht: %+v", hub.LinkStats(agentID))
	return domain.AgentLink{}
}

// Über den echten Broker: Eine Leitung, die ohne Abschied abreißt, ist ein
// Netzfehler; eine, die verstummt, ein Ausfall der Lebenszeichen.
func TestEchterBrokerUnterscheidetNetzfehlerUndVerstummen(t *testing.T) {
	if testing.Short() {
		t.Skip("wartet auf den Keepalive des Brokers")
	}
	hub, server, secret := newTestHub(t)

	peer := rawConnect(t, hub, server.AgentID, secret, 30)
	waitLink(t, hub, server.AgentID, func(l domain.AgentLink) bool { return l.Connects == 1 })
	_ = peer.Close()
	link := waitLink(t, hub, server.AgentID, func(l domain.AgentLink) bool { return l.Disconnects == 1 })
	if link.LastCause != domain.DropNetwork {
		t.Fatalf("abgerissene Leitung: Ursache = %q, want network", link.LastCause)
	}

	rawConnect(t, hub, server.AgentID, secret, 1) // Keepalive 1 s, danach Stille
	link = waitLink(t, hub, server.AgentID, func(l domain.AgentLink) bool { return l.Disconnects == 2 })
	if link.LastCause != domain.DropPing || link.UnintendedLastHour != 2 {
		t.Fatalf("verstummte Leitung: %+v, want Ursache ping", link)
	}
}
