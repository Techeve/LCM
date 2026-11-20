package remote_test

// In-Process-Integrationstest der gesamten LCM-Remote-Strecke:
// echter Fiber-Listener mit /mqtt-Route → WebSocket-Upgrade → eingebetteter
// mochi-Broker → echter lcm-agent-Client (paho über ws://) → Kommando-
// Roundtrip über hub.Conn (sshx.Conn). Ohne TLS (wie --dev) - die TLS-/
// Pinning-Logik ist in internal/agent separat gekapselt.

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/agent"
	"LCM/internal/core/domain"
	"LCM/internal/netfilter"
	"LCM/internal/remote"
	"LCM/internal/remote/wire"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

func TestAgentEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}

	hub, servers, server, app := connectedAgent(t)
	t.Cleanup(func() {
		_ = app.Shutdown()
		hub.Close()
	})

	// Kommando-Roundtrip über die sshx.Conn-Schnittstelle.
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	out, code, err := conn.Run("echo integration-ok")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 || out != "integration-ok\n" {
		t.Fatalf("got (%q, %d)", out, code)
	}

	// Exit-Code + stdin über RunStdin.
	out, code, err = conn.RunStdin("cat; exit 5", "durchgereicht")
	if err != nil {
		t.Fatalf("runstdin: %v", err)
	}
	if code != 5 || out != "durchgereicht" {
		t.Fatalf("got (%q, %d)", out, code)
	}

	// Inventar ist angekommen (agent_version gesetzt).
	waitFor(t, 5*time.Second, "inventar verbucht", func() bool {
		s, err := servers.FindByAgentID(server.AgentID)
		return err == nil && s.AgentVersion != ""
	})

	// Revocation: neues Secret in der DB (wie RegenerateToken) + Trennen -
	// der Agent kann sich mit dem alten Secret nicht mehr anmelden.
	if err := servers.UpdateFields(server.ID, map[string]any{
		"agent_token_hash": wire.HashSecret("neues-secret"),
	}); err != nil {
		t.Fatal(err)
	}
	hub.Disconnect(server.AgentID)
	waitFor(t, 10*time.Second, "agent bleibt offline", func() bool { return !hub.Online(server.AgentID) })
	// Reconnect-Versuche des Agents laufen ins Leere (Auth scheitert) -
	// kurz nachprüfen, dass er nicht wieder online kommt.
	time.Sleep(1500 * time.Millisecond)
	if hub.Online(server.AgentID) {
		t.Fatal("agent trotz revocation wieder online")
	}
	if _, err := hub.Conn(server); err == nil {
		t.Fatal("Conn muss nach revocation offline melden")
	}
}

// TestAbortStopsAgentCommand: Bricht LCM einen Job ab (Close der Conn), hält
// der Agent das Kommando samt Kindern an - die ganze Strecke über den Broker.
func TestAbortStopsAgentCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}
	hub, _, server, app := connectedAgent(t)
	t.Cleanup(func() {
		_ = app.Shutdown()
		hub.Close()
	})
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "durchgelaufen")
	done := make(chan error, 1)
	go func() {
		_, _, err := conn.Run("sleep 2; touch " + marker)
		done <- err
	}()
	time.Sleep(500 * time.Millisecond) // Kommando läuft im Agenten
	_ = conn.Close()
	if err := <-done; err == nil {
		t.Fatal("abgebrochenes kommando meldet erfolg")
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("kommando lief nach dem abbruch im agenten weiter")
	}
}

// TestCloseDisconnectsAgents: Agents hängen an keinem Listener des Brokers.
// Trennt Close sie nicht selbst, wartet der Broker ewig auf sie, und systemd
// beendet den Dienst nach 30 s mit SIGKILL.
func TestCloseDisconnectsAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}

	hub, _, server, app := connectedAgent(t)
	// Reihenfolge wie im Dienst: Erst nimmt der Agent-Port nichts mehr an, sonst
	// verbände sich der getrennte Agent sofort neu.
	if err := app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	closeInTime(t, hub)
	if hub.Online(server.AgentID) {
		t.Fatal("agent nach Close noch online")
	}
}

// TestCloseDropsSilentConn: Eine Verbindung ohne CONNECT führt der Broker noch
// nicht als Client, wartet aber genauso auf ihr Ende.
func TestCloseDropsSilentConn(t *testing.T) {
	hub := remote.New(nil, slog.Default())
	if err := hub.Start(); err != nil {
		t.Fatal(err)
	}
	conn, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	go func() { _ = hub.HandleConn(conn) }()
	// Das erste Byte eines CONNECT: Kommt Write zurück, liest der Broker schon.
	if _, err := peer.Write([]byte{0x10}); err != nil {
		t.Fatal(err)
	}
	closeInTime(t, hub)
}

func closeInTime(t *testing.T, hub *remote.Hub) {
	t.Helper()
	closed := make(chan struct{})
	go func() {
		hub.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hängt an einer offenen Verbindung")
	}
}

// connectedAgent startet die LCM-Seite (DB, Hub, Fiber-App mit /mqtt) und einen
// echten Agent und wartet, bis er online ist. Den Hub schließt der Aufrufer.
func connectedAgent(t *testing.T) (*remote.Hub, *repositories.ServerRepository, *domain.Server, *fiber.App) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	servers := repositories.NewServerRepository(db)
	const secret = "integration-secret"
	server := &domain.Server{
		Name: "roadwarrior", Transport: domain.TransportAgent, ServiceUser: "root",
		AgentID: "11111111-2222-3333-4444-555555555555", AgentTokenHash: wire.HashSecret(secret),
	}
	if err := servers.Create(server); err != nil {
		t.Fatal(err)
	}

	hub := remote.New(servers, slog.Default())
	if err := hub.Start(); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/mqtt", remote.WSHandler(hub, netfilter.ProxyTrust{}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	// Agent-Seite: echter Client (wie `lcm-agent run`), verbindet über ws://.
	cfg := &agent.Config{
		URL:     fmt.Sprintf("http://%s", ln.Addr().String()),
		AgentID: server.AgentID,
		Secret:  secret,
	}
	go func() { _ = agent.NewClient(cfg, slog.Default(), "test").Run() }()

	// Auf den Connect warten (Session-Event füllt die Online-Registry).
	waitFor(t, 10*time.Second, "agent online", func() bool { return hub.Online(server.AgentID) })
	return hub, servers, server, app
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}
