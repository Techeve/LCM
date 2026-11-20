package remote_test

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/agent"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/netfilter"
	"LCM/internal/remote"
	"LCM/internal/remote/wire"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// TestAgentFolgtDerCA spielt den Umstieg über die ganze Strecke durch, mit
// TLS wie im Betrieb: Ein Agent kennt nur das alte selbstsignierte
// Zertifikat (Fingerprint aus dem Token) und lernt dessen Schlüssel. LCM
// stellt auf die CA um (gleicher Schlüssel), der Agent verbindet sich neu und
// lernt die CA. Danach bekommt das Zertifikat einen neuen Schlüssel, und der
// Agent kommt trotzdem wieder.
func TestAgentFolgtDerCA(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket + tls)")
	}
	renewBefore := services.CertRenewBefore
	services.CertRenewBefore = 100 * 24 * time.Hour // jedes Zertifikat ist fällig
	t.Cleanup(func() { services.CertRenewBefore = renewBefore })

	dataDir := t.TempDir()
	tlsxtest.LegacySelfSigned(t, dataDir, time.Now().Add(-time.Hour))
	ca, err := tlsx.EnsureCA(dataDir, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	store, err := tlsx.NewCertStore(filepath.Join(dataDir, tlsx.CertFileName), filepath.Join(dataDir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	hub, servers, server, addr := tlsBroker(t, store)
	renewal := services.NewCertRenewal(dataDir, servers, store.Reload).WithOnline(hub.Online)
	renewal.SetCA(ca)

	legacyFP, _ := tlsx.Fingerprint(filepath.Join(dataDir, tlsx.CertFileName))
	pins := func() *domain.Server {
		s, err := servers.FindByAgentID(server.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	startAgent(t, hub, server, addr, legacyFP)
	waitFor(t, 10*time.Second, "schlüssel-pin gemeldet", func() bool { return pins().AgentKeyPin != "" })

	// Schritt 1: Zertifikat der CA mit demselben Schlüssel.
	renewal.Run()
	if info, _ := tlsx.Inspect(dataDir, ca); !info.FromCA {
		t.Fatal("nicht auf die CA umgestellt")
	}
	reconnect(t, hub, server)
	waitFor(t, 10*time.Second, "ca-pin gemeldet", func() bool { return pins().AgentCAPin == ca.Fingerprint() })

	// Schritt 2: neuer Schlüssel, der Agent folgt über die CA.
	keyBefore := pins().AgentKeyPin
	renewal.Run()
	info, _ := tlsx.Inspect(dataDir, ca)
	if info.KeyPin == keyBefore {
		t.Fatal("kein neuer Schlüssel")
	}
	reconnect(t, hub, server)
	waitFor(t, 10*time.Second, "neuer schlüssel-pin gemeldet", func() bool { return pins().AgentKeyPin == info.KeyPin })
	runEcho(t, hub, server)
}

// tlsBroker startet Hub und Agent-Listener mit TLS aus dem CertStore und
// liefert dessen Adresse.
func tlsBroker(t *testing.T, store *tlsx.CertStore) (*remote.Hub, *repositories.ServerRepository, *domain.Server, string) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	servers := repositories.NewServerRepository(db)
	server := &domain.Server{
		Name: "roadwarrior", Transport: domain.TransportAgent, ServiceUser: "root",
		AgentID: "22222222-3333-4444-5555-666666666666", AgentTokenHash: wire.HashSecret("tls-secret"),
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
	go func() { _ = app.Listener(tls.NewListener(ln, store.TLSConfig())) }()
	t.Cleanup(func() {
		_ = app.Shutdown()
		hub.Close()
	})
	return hub, servers, server, ln.Addr().String()
}

// startAgent startet einen echten Agent (wie `lcm-agent run`) mit dem
// Zertifikats-Pin aus dem Token und wartet, bis er online ist.
func startAgent(t *testing.T, hub *remote.Hub, server *domain.Server, addr, certFP string) {
	t.Helper()
	cfg := &agent.Config{
		URL:     fmt.Sprintf("https://%s", addr),
		AgentID: server.AgentID, Secret: "tls-secret", CertFingerprint: certFP,
	}
	path := filepath.Join(t.TempDir(), "agent.json")
	go func() { _ = agent.NewClient(cfg, slog.Default(), "test").WithConfigPath(path).Run() }()
	waitFor(t, 10*time.Second, "agent online", func() bool { return hub.Online(server.AgentID) })
}

// reconnect trennt den Agent; er verbindet sich von selbst neu und sieht
// dabei das aktuelle Zertifikat. Ob das geklappt hat, zeigen die gemeldeten
// Pins - die Trennung selbst ist zu kurz, um sie sicher abzuwarten.
func reconnect(t *testing.T, hub *remote.Hub, server *domain.Server) {
	t.Helper()
	hub.Disconnect(server.AgentID)
	time.Sleep(100 * time.Millisecond)
	waitFor(t, 15*time.Second, "agent wieder online", func() bool { return hub.Online(server.AgentID) })
}

func runEcho(t *testing.T, hub *remote.Hub, server *domain.Server) {
	t.Helper()
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	out, code, err := conn.Run("echo ca-ok")
	if err != nil || code != 0 || out != "ca-ok\n" {
		t.Fatalf("kommando nach dem schlüsselwechsel: (%q, %d, %v)", out, code, err)
	}
}
