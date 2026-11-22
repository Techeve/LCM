package remote_test

// In-Process-Integrationstest der gesamten LCM-Remote-Strecke:
// echter Fiber-Listener mit /mqtt-Route → WebSocket-Upgrade → eingebetteter
// mochi-Broker → echter lcm-agent-Client (paho über ws://) → Kommando-
// Roundtrip über hub.Conn (sshx.Conn). Ohne TLS (wie --dev) - die TLS-/
// Pinning-Logik ist in internal/agent separat gekapselt.

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/agent"
	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
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
	return connectedAgentVia(t, nil, "")
}

// connectedAgentVia ist connectedAgent hinter einem Proxy: forwardedFor wird
// dem Upgrade-Request als X-Forwarded-For vorgesetzt (leer = keine Kopfzeile),
// trust sagt, ob LCM sie glaubt.
func connectedAgentVia(t *testing.T, trust *netfilter.ProxyTrustHolder, forwardedFor string) (*remote.Hub, *repositories.ServerRepository, *domain.Server, *fiber.App) {
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
	if forwardedFor != "" {
		app.Use(func(c fiber.Ctx) error {
			c.Request().Header.Set(fiber.HeaderXForwardedFor, forwardedFor)
			return c.Next()
		})
	}
	app.Get("/mqtt", remote.WSHandler(hub, trust))
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

// TestAgentTerminalEndToEnd: Web-Konsole über die ganze Strecke - LCM öffnet
// über hub.Conn ein Terminal, der echte Agent startet eine Shell im PTY, die
// Ausgabe kommt zurück, und Close beendet die Shell. Nur unter Linux als root
// (PTY, Arbeitsverzeichnis /root wie im Dienst).
func TestAgentTerminalEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("braucht linux und root")
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
	defer conn.Close()
	term, err := conn.(sshx.TerminalConn).Terminal("xterm", 90, 20)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if _, err := term.Write([]byte("stty size; echo tty-$((40+2))\n")); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "tty-42") && time.Now().Before(deadline) {
		n, err := term.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "20 90") || !strings.Contains(out.String(), "tty-42") {
		t.Fatalf("ausgabe ohne fenstergröße oder rechnung: %q", out.String())
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Read(buf); err != io.EOF {
		t.Fatalf("nach close erwartet EOF, bekam %v", err)
	}
}

// TestHubKnowsPeerAddress: Der Hub kennt die Gegenadresse eines verbundenen
// Agents - hinter einem vertrauten Proxy die weitergeleitete, sonst die der
// Verbindung.
func TestHubKnowsPeerAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}
	loopback, err := netfilter.Parse([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		trust *netfilter.ProxyTrustHolder
		want  string
	}{
		"vertrauter Proxy":     {netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: loopback}), "203.0.113.9"},
		"ohne Vertrauen":       {nil, "127.0.0.1"},
		"Proxy nicht vertraut": {netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: mustParse(t, "192.0.2.1/32")}), "127.0.0.1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hub, _, server, app := connectedAgentVia(t, tc.trust, "203.0.113.9")
			t.Cleanup(func() {
				_ = app.Shutdown()
				hub.Close()
			})
			peers := hub.ConnectedPeers()
			if len(peers) != 1 || peers[0].AgentID != server.AgentID || peers[0].Name != "roadwarrior" {
				t.Fatalf("Agenten: %+v", peers)
			}
			if got := peers[0].Addr.String(); got != tc.want {
				t.Errorf("Adresse = %s, erwartet %s", got, tc.want)
			}
		})
	}
}

func mustParse(t *testing.T, entries ...string) netfilter.Allowlist {
	t.Helper()
	list, err := netfilter.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	return list
}
