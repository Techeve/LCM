package remote

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"

	"LCM/internal/netfilter"
	"LCM/internal/remote/wire"
)

// Tests zu S-07: Grenzen für Verbindungen vor der Anmeldung, mit dem echten
// Broker. Die Fristen und Grenzen sind kurz gesetzt (setPreAuth), die Vorgaben
// stehen in preauth.go.

func setPreAuth(deadline time.Duration, maxPacket int, maxConns int64) func(*Hub) {
	return func(h *Hub) {
		h.preAuth.limits = preAuthLimits{deadline: deadline, maxPacket: maxPacket, maxConns: maxConns}
	}
}

// serve übergibt dem Hub das eine Ende einer Pipe; das Ergebnis von HandleConn
// kommt über den Kanal.
func serve(t *testing.T, hub *Hub) (peer net.Conn, ended chan error) {
	t.Helper()
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	ended = make(chan error, 1)
	go func() { ended <- hub.HandleConn(conn) }()
	return peer, ended
}

func expectEnd(t *testing.T, ended chan error, within time.Duration, what string) error {
	t.Helper()
	select {
	case err := <-ended:
		return err
	case <-time.After(within):
		t.Fatalf("%s: Verbindung blieb offen", what)
		return nil
	}
}

// agentConnect ist das CONNECT eines Agents, wie paho es schickt: Client-ID
// und Benutzer sind die AgentID, das Passwort das Secret, dazu das Testament.
func agentConnect(agentID, secret string) *packets.ConnectPacket {
	pk := packets.NewControlPacket(packets.Connect).(*packets.ConnectPacket)
	pk.ProtocolName, pk.ProtocolVersion = "MQTT", 4
	pk.CleanSession = true
	pk.ClientIdentifier = agentID
	pk.UsernameFlag, pk.Username = true, agentID
	pk.PasswordFlag, pk.Password = true, []byte(secret)
	pk.WillFlag, pk.WillQos, pk.WillRetain = true, 1, true
	pk.WillTopic, pk.WillMessage = wire.TopicStatus(agentID), []byte(wire.StatusOffline)
	pk.Keepalive = 30
	return pk
}

func encoded(t *testing.T, pk *packets.ConnectPacket) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pk.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestPreAuthStummeVerbindungWirdGeschlossen: Ohne CONNECT in der Frist schließt
// der Hub die Verbindung.
func TestPreAuthStummeVerbindungWirdGeschlossen(t *testing.T) {
	hub, _, _ := newTestHubWith(t, setPreAuth(300*time.Millisecond, preAuthMaxPacket, preAuthMaxConns))
	_, ended := serve(t, hub)
	start := time.Now()
	expectEnd(t, ended, 5*time.Second, "stumme Verbindung")
	if took := time.Since(start); took < 250*time.Millisecond {
		t.Errorf("nach %v geschlossen, vor Ablauf der Frist", took)
	}
	if n := hub.preAuth.open.Load(); n != 0 {
		t.Errorf("Platz nicht frei: %d", n)
	}
}

// TestPreAuthZuGrossesErstesPaket: Ein erstes Paket über der Grenze beendet die
// Verbindung sofort, schon bevor der Broker einen Puffer dafür anlegt; auch wenn
// das Längenfeld stückweise ankommt.
func TestPreAuthZuGrossesErstesPaket(t *testing.T) {
	hub, _, _ := newTestHubWith(t, setPreAuth(30*time.Second, 1024, preAuthMaxConns))
	cases := map[string][][]byte{
		"ganz":       {{0x10, 0xff, 0xff, 0x7f}},       // 2 MiB
		"stückweise": {{0x10}, {0x81}, {0x08}},         // 1025
		"acht MiB":   {{0x10, 0x80, 0x80, 0x80, 0x04}}, // 8 MiB
	}
	for name, chunks := range cases {
		peer, ended := serve(t, hub)
		go func() {
			for _, c := range chunks {
				if _, err := peer.Write(c); err != nil {
					return
				}
			}
		}()
		expectEnd(t, ended, 3*time.Second, name)
	}
}

// TestPreAuthPaketAnDerGrenzeGeht: Genau die Grenze ist erlaubt; ein CONNECT
// dieser Größe wird gelesen (und scheitert erst an der Anmeldung).
func TestPreAuthPaketAnDerGrenzeGeht(t *testing.T) {
	hub, server, secret := newTestHubWith(t, setPreAuth(30*time.Second, 0, preAuthMaxConns))
	pk := agentConnect(server.AgentID, secret)
	size := len(encoded(t, pk))
	hub.preAuth.limits.maxPacket = size - 2 // Länge ohne Kopf (Typ und ein Längenbyte)

	peer, _ := serve(t, hub)
	go func() { _, _ = peer.Write(encoded(t, pk)) }()
	if _, err := packets.ReadPacket(peer); err != nil {
		t.Fatalf("CONNACK nicht gelesen: %v", err)
	}
}

// TestPreAuthNormalerAgentVerbindet: Ein CONNECT wie von einem echten Agent
// passt weit unter die Grenze, wird beantwortet, und danach zählt die
// Verbindung nicht mehr als unangemeldet.
func TestPreAuthNormalerAgentVerbindet(t *testing.T) {
	hub, server, secret := newTestHub(t)
	pk := agentConnect(server.AgentID, secret)
	if size := len(encoded(t, pk)); size > preAuthMaxPacket/10 {
		t.Fatalf("CONNECT hat %d Byte, die Grenze %d lässt zu wenig Luft", size, preAuthMaxPacket)
	}
	peer, ended := serve(t, hub)
	go func() { _, _ = peer.Write(encoded(t, pk)) }()
	ack, err := packets.ReadPacket(peer)
	if err != nil {
		t.Fatal(err)
	}
	if ca, ok := ack.(*packets.ConnackPacket); !ok || ca.ReturnCode != packets.Accepted {
		t.Fatalf("Antwort %v, erwartet CONNACK Accepted", ack)
	}
	waitFor(t, func() bool { return hub.Online(server.AgentID) })
	if n := hub.preAuth.open.Load(); n != 0 {
		t.Errorf("angemeldete Verbindung zählt noch als unangemeldet: %d", n)
	}
	select {
	case err := <-ended:
		t.Fatalf("Verbindung beendet: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestPreAuthFalschesPasswortGibtPlatzFrei: Eine abgewiesene Anmeldung gibt den
// Platz wieder frei.
func TestPreAuthFalschesPasswortGibtPlatzFrei(t *testing.T) {
	hub, server, _ := newTestHub(t)
	peer, ended := serve(t, hub)
	go func() { _, _ = peer.Write(encoded(t, agentConnect(server.AgentID, "falsch"))) }()
	_, _ = packets.ReadPacket(peer)
	expectEnd(t, ended, 3*time.Second, "falsches Passwort")
	if n := hub.preAuth.open.Load(); n != 0 {
		t.Errorf("Platz nicht frei: %d", n)
	}
}

// TestPreAuthObergrenzeUnangemeldeterVerbindungen: Über der Grenze weist
// HandleConn ab; angemeldete Verbindungen zählen nicht mit; ein freier Platz
// wird wieder vergeben.
func TestPreAuthObergrenzeUnangemeldeterVerbindungen(t *testing.T) {
	hub, server, secret := newTestHubWith(t, setPreAuth(30*time.Second, preAuthMaxPacket, 3))

	agentPeer, _ := serve(t, hub)
	go func() { _, _ = agentPeer.Write(encoded(t, agentConnect(server.AgentID, secret))) }()
	if _, err := packets.ReadPacket(agentPeer); err != nil { // Pipes sind synchron: Der Hub schreibt erst, wenn gelesen wird.
		t.Fatal(err)
	}
	waitFor(t, func() bool { return hub.Online(server.AgentID) })

	var silent []net.Conn
	for range 3 {
		peer, _ := serve(t, hub)
		silent = append(silent, peer)
	}
	waitFor(t, hub.PreAuthFull)

	_, ended := serve(t, hub)
	if err := expectEnd(t, ended, 3*time.Second, "vierte Verbindung"); !errors.Is(err, errPreAuthFull) {
		t.Errorf("Fehler %v, erwartet errPreAuthFull", err)
	}

	_ = silent[0].Close() // Platz wird frei
	waitFor(t, func() bool { return !hub.PreAuthFull() })
	again, againEnded := serve(t, hub)
	select {
	case err := <-againEnded:
		t.Fatalf("Verbindung auf freiem Platz abgewiesen: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	_ = again.Close()
}

// TestPreAuthUpgradeUeberGrenzeBekommt503: Der WebSocket-Handler weist vor dem
// Upgrade mit 503 ab, sobald die Grenze erreicht ist.
func TestPreAuthUpgradeUeberGrenzeBekommt503(t *testing.T) {
	hub, _, _ := newTestHubWith(t, setPreAuth(30*time.Second, preAuthMaxPacket, 1))
	app := fiber.New()
	app.Get(wire.WSPath, WSHandler(hub, netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{})))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	url := "ws://" + ln.Addr().String() + wire.WSPath

	first, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	waitFor(t, hub.PreAuthFull)

	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if conn != nil {
		_ = conn.Close()
	}
	if !errors.Is(err, websocket.ErrBadHandshake) || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("err = %v, resp = %v, erwartet 503", err, resp)
	}
	if !strings.Contains(err.Error(), "bad handshake") {
		t.Errorf("unerwarteter Fehler %v", err)
	}
}

// TestPreAuthFristEndetMitDerAnmeldung: Nach dem CONNECT gilt die Frist des
// Keepalive, nicht mehr die kurze vor der Anmeldung.
func TestPreAuthFristEndetMitDerAnmeldung(t *testing.T) {
	hub, server, secret := newTestHubWith(t, setPreAuth(300*time.Millisecond, preAuthMaxPacket, preAuthMaxConns))
	peer, ended := serve(t, hub)
	go func() { _, _ = peer.Write(encoded(t, agentConnect(server.AgentID, secret))) }()
	if _, err := packets.ReadPacket(peer); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		t.Fatalf("angemeldete Verbindung nach der Frist beendet: %v", err)
	case <-time.After(900 * time.Millisecond):
	}
}
