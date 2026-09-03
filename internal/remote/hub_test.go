package remote

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/domain"
	"LCM/internal/remote/wire"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// newTestHub baut Hub + gestarteten Broker auf einer In-Memory-DB und legt
// einen Agent-Server mit bekanntem Secret an.
func newTestHub(t *testing.T) (*Hub, *domain.Server, string) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrieren: %v", err)
	}
	servers := repositories.NewServerRepository(db)
	const secret = "test-secret-123"
	server := &domain.Server{
		Name: "agent01", Transport: domain.TransportAgent, ServiceUser: "root",
		AgentID: "0e0a6cbb-aaaa-bbbb-cccc-1234567890ab", AgentTokenHash: wire.HashSecret(secret),
	}
	if err := servers.Create(server); err != nil {
		t.Fatalf("server anlegen: %v", err)
	}
	hub := New(servers, slog.Default())
	if err := hub.Start(); err != nil {
		t.Fatalf("hub starten: %v", err)
	}
	t.Cleanup(hub.Close)
	return hub, server, secret
}

// connectPacket baut ein CONNECT-Paket mit Username/Passwort.
func connectPacket(username, password string) packets.Packet {
	return packets.Packet{Connect: packets.ConnectParams{
		Username: []byte(username), Password: []byte(password),
	}}
}

func TestOnConnectAuthenticate(t *testing.T) {
	hub, server, secret := newTestHub(t)
	hook := &agentHook{hub: hub}

	tests := []struct {
		name     string
		clientID string
		username string
		password string
		want     bool
	}{
		{"gültig", server.AgentID, server.AgentID, secret, true},
		{"falsches secret", server.AgentID, server.AgentID, "falsch", false},
		{"unbekannte agent-id", "nope", "nope", secret, false},
		{"client-id != username", "anders", server.AgentID, secret, false},
		{"leerer username", "", "", secret, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := hub.broker.NewClient(nil, "test", tt.clientID, false)
			if got := hook.OnConnectAuthenticate(cl, connectPacket(tt.username, tt.password)); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnACLCheck(t *testing.T) {
	hub, server, _ := newTestHub(t)
	hook := &agentHook{hub: hub}
	id := server.AgentID
	cl := hub.broker.NewClient(nil, "test", id, false)

	tests := []struct {
		name  string
		topic string
		write bool
		want  bool
	}{
		{"eigenes cmd subscriben", wire.TopicCmd(id), false, true},
		{"eigenes res publizieren", wire.TopicRes(id), true, true},
		{"eigenes inv publizieren", wire.TopicInv(id), true, true},
		{"eigenes status publizieren", wire.TopicStatus(id), true, true},
		{"fremdes cmd subscriben", wire.TopicCmd("fremd"), false, false},
		{"fremdes res publizieren", wire.TopicRes("fremd"), true, false},
		{"eigenes cmd publizieren (nur server darf)", wire.TopicCmd(id), true, false},
		{"eigenes res subscriben (nur server liest)", wire.TopicRes(id), false, false},
		{"wildcard subscriben", "lcm/a/+/cmd", false, false},
		{"beliebiges topic", "$SYS/broker", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hook.OnACLCheck(cl, tt.topic, tt.write); got != tt.want {
				t.Fatalf("OnACLCheck(%q, write=%v) = %v, want %v", tt.topic, tt.write, got, tt.want)
			}
		})
	}

	t.Run("inline-client darf alles", func(t *testing.T) {
		inline := hub.broker.NewClient(nil, "test", "inline", true)
		if !hook.OnACLCheck(inline, wire.TopicCmd("egal"), true) {
			t.Fatal("inline-client muss durch die acl")
		}
	})
}

// TestRPCRoundtrip prüft die Kommando-Korrelation über den echten Broker:
// ein simulierter Agent (Inline-Subscription) beantwortet cmd-Nachrichten.
func TestRPCRoundtrip(t *testing.T) {
	hub, server, _ := newTestHub(t)
	// Agent online markieren (Session-Event simulieren).
	cl := hub.broker.NewClient(nil, "test", server.AgentID, false)
	hub.agentConnected(cl)

	// Simulierter Agent: beantwortet jedes Kommando mit Echo-Daten.
	err := hub.broker.Subscribe(wire.TopicCmd(server.AgentID), 99, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		var cmd wire.Command
		if json.Unmarshal(pk.Payload, &cmd) != nil || cmd.Cancel {
			return
		}
		res, _ := json.Marshal(wire.Result{ID: cmd.ID, Output: "echo:" + cmd.Cmd + ":" + cmd.Stdin, ExitCode: 7})
		_ = hub.broker.Publish(wire.TopicRes(server.AgentID), res, false, 1)
	})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	out, code, err := conn.RunStdin("ls", "eingabe")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "echo:ls:eingabe" || code != 7 {
		t.Fatalf("got (%q, %d)", out, code)
	}
}

// TestConnCloseUnblocks: Close() (Job-Abort/Watchdog) entsperrt wartende
// Kommandos mit Fehler und publiziert ein Cancel.
func TestConnCloseUnblocks(t *testing.T) {
	hub, server, _ := newTestHub(t)
	cl := hub.broker.NewClient(nil, "test", server.AgentID, false)
	hub.agentConnected(cl)

	cancelSeen := make(chan struct{}, 1)
	_ = hub.broker.Subscribe(wire.TopicCmd(server.AgentID), 99, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		var cmd wire.Command
		if json.Unmarshal(pk.Payload, &cmd) == nil && cmd.Cancel {
			select {
			case cancelSeen <- struct{}{}:
			default:
			}
		}
		// Kein Result - der Aufrufer soll hängen, bis Close() kommt.
	})

	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := conn.Run("sleep 100")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond) // Run() ins Warten laufen lassen
	_ = conn.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run muss nach Close mit Fehler enden")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close hat Run nicht entsperrt")
	}
	select {
	case <-cancelSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("kein Cancel-Publish nach Close")
	}
}

// TestDisconnectFailsPending: trennt sich der Agent während eines laufenden
// Kommandos, scheitern wartende Aufrufe sofort.
func TestDisconnectFailsPending(t *testing.T) {
	hub, server, _ := newTestHub(t)
	cl := hub.broker.NewClient(nil, "test", server.AgentID, false)
	hub.agentConnected(cl)

	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	done := make(chan error, 1)
	go func() {
		_, _, err := conn.Run("hängt")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	hub.agentDisconnected(cl)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run muss nach Agent-Disconnect mit Fehler enden")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Disconnect hat Run nicht entsperrt")
	}
	if hub.Online(server.AgentID) {
		t.Fatal("agent noch online")
	}
	if _, err := hub.Conn(server); err == nil {
		t.Fatal("Conn muss offline melden")
	}
}

// TestDisconnectFailsSubsequentCommandFast: trennt sich der Agent, muss ein
// FOLGE-Kommando auf derselben Conn sofort scheitern (nicht aufs volle
// Kommando-Timeout warten) - das hielt sonst Mehr-Schritt-Abläufe (Scan)
// bis zum Watchdog fest.
func TestDisconnectFailsSubsequentCommandFast(t *testing.T) {
	hub, server, _ := newTestHub(t)
	// Sehr großzügige Stille-Frist: Ein hängendes Folge-Kommando würde so
	// lange warten - der Test muss trotzdem in Millisekunden zurückkehren.
	hub.WithIdleTimeout(func() time.Duration { return time.Hour })
	cl := hub.broker.NewClient(nil, "test", server.AgentID, false)
	hub.agentConnected(cl)

	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Agent trennt sich, während die Conn offen ist (kein Kommando läuft).
	hub.agentDisconnected(cl)

	done := make(chan error, 1)
	go func() {
		_, _, err := conn.Run("nächster scan-schritt")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("folge-kommando nach disconnect muss scheitern")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("folge-kommando wartete aufs timeout statt sofort zu scheitern")
	}
}

// TestTakeoverKeepsOnline: der Disconnect eines ALTEN Clients (Session-
// Takeover) darf den neu verbundenen nicht austragen.
func TestTakeoverKeepsOnline(t *testing.T) {
	hub, server, _ := newTestHub(t)
	oldCl := hub.broker.NewClient(nil, "test", server.AgentID, false)
	newCl := hub.broker.NewClient(nil, "test", server.AgentID, false)
	hub.agentConnected(oldCl)
	hub.agentConnected(newCl)
	hub.agentDisconnected(oldCl) // Takeover: alter Client geht nachträglich
	if !hub.Online(server.AgentID) {
		t.Fatal("takeover hat den neuen client ausgetragen")
	}
	hub.agentDisconnected(newCl)
	if hub.Online(server.AgentID) {
		t.Fatal("echter disconnect nicht verbucht")
	}
}
