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
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/agent"
	"LCM/internal/core/domain"
	"LCM/internal/remote"
	"LCM/internal/remote/wire"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

func TestAgentEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}

	// LCM-Seite: DB, Server-Datensatz, Hub, Fiber-App mit /mqtt.
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
	t.Cleanup(hub.Close)

	app := fiber.New()
	app.Get("/mqtt", remote.WSHandler(hub))
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
