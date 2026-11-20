package services_test

// Tests für das Agent-Enrollment (LCM Remote): Token-Erzeugung, Hash at
// rest, Regenerate mit Session-Trennung und das SSH-Feature-Gating.

import (
	"errors"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
	"LCM/internal/storage/repositories"
)

// fakeHub erfüllt services.AgentConnector für die Service-Tests.
type fakeHub struct {
	online        map[string]bool
	disconnected  []string
	connRequested []string
}

func (f *fakeHub) Conn(server *domain.Server) (sshx.Conn, error) {
	f.connRequested = append(f.connRequested, server.AgentID)
	return nil, services.ErrAgentOffline
}
func (f *fakeHub) Online(agentID string) bool { return f.online[agentID] }
func (f *fakeHub) Disconnect(agentID string)  { f.disconnected = append(f.disconnected, agentID) }

// TestAgentDownloadPin: Ohne Pin (nicht verdrahtet oder Zertifikat nicht
// lesbar) bleibt er leer - der Join-Befehl prüft dann normal, statt mit -k
// jedes Zertifikat anzunehmen.
func TestAgentDownloadPin(t *testing.T) {
	env := newTestEnv(t)
	if pin := env.Servers.AgentDownloadPin(); pin != "" {
		t.Errorf("unverdrahtet: Pin %q, erwartet leer", pin)
	}
	env.Servers.WithAgentDownloadPin(func() (string, error) { return "", errors.New("kein zertifikat") })
	if pin := env.Servers.AgentDownloadPin(); pin != "" {
		t.Errorf("bei Fehler: Pin %q, erwartet leer", pin)
	}
	env.Servers.WithAgentDownloadPin(func() (string, error) { return "c2hhMjU2LXBpbg==", nil })
	if pin := env.Servers.AgentDownloadPin(); pin != "c2hhMjU2LXBpbg==" {
		t.Errorf("Pin %q nicht durchgereicht", pin)
	}
}

func TestCreateAgentServer(t *testing.T) {
	env := newTestEnv(t)
	env.Servers.WithCertFingerprint(func() (string, error) { return "cafe01", nil }).
		WithCAFingerprint(func() (string, error) { return "ca0001", nil })

	server, token, err := env.Servers.CreateAgentServer("notebook01", "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !server.IsAgent() || server.ServiceUser != "root" || server.Reachable {
		t.Fatalf("server falsch angelegt: %+v", server)
	}
	if server.AgentID == "" {
		t.Fatal("agent-id fehlt")
	}

	// Token trägt AgentID, Secret und den Zertifikats-Fingerprint.
	parsed, err := services.ParseAgentToken(token)
	if err != nil {
		t.Fatalf("token parsen: %v", err)
	}
	secret := parsed.Secret
	if parsed.AgentID != server.AgentID || parsed.CertFingerprint != "cafe01" || parsed.CAFingerprint != "ca0001" {
		t.Fatalf("token-inhalt falsch: %+v", parsed)
	}
	// At rest liegt NUR der Hash; er passt zum Secret aus dem Token.
	stored, err := repositories.NewServerRepository(env.DB()).FindByAgentID(server.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentTokenHash != wire.HashSecret(secret) {
		t.Fatal("hash at rest passt nicht zum token-secret")
	}
	if stored.AgentTokenHash == secret || stored.AgentTokenPrefix != secret[:12] {
		t.Fatal("secret-ablage falsch (klartext oder prefix kaputt)")
	}

	// Doppelter Name wird abgewiesen.
	if _, _, err := env.Servers.CreateAgentServer("notebook01", "tester"); !errors.Is(err, services.ErrServerNameTaken) {
		t.Fatalf("erwartete ErrServerNameTaken, got %v", err)
	}
}

func TestRegenerateAgentToken(t *testing.T) {
	env := newTestEnv(t)
	hub := &fakeHub{online: map[string]bool{}}
	env.Servers.WithAgentHub(hub)

	server, oldToken, err := env.Servers.CreateAgentServer("agent02", "tester")
	if err != nil {
		t.Fatal(err)
	}
	newToken, err := env.Servers.RegenerateAgentToken(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("token unverändert")
	}
	// Aktive Session wird getrennt (Revocation).
	if len(hub.disconnected) != 1 || hub.disconnected[0] != server.AgentID {
		t.Fatalf("session nicht getrennt: %v", hub.disconnected)
	}
	// Der alte Hash ist ersetzt.
	parsedNew, _ := services.ParseAgentToken(newToken)
	newSecret := parsedNew.Secret
	stored, _ := repositories.NewServerRepository(env.DB()).FindByAgentID(server.AgentID)
	if stored.AgentTokenHash != wire.HashSecret(newSecret) {
		t.Fatal("neuer hash nicht gespeichert")
	}

	// Nur für Agent-Server erlaubt.
	sshServer := seedSSHServer(t, env, "ssh01")
	if _, err := env.Servers.RegenerateAgentToken(repositories.ScopeAll(), sshServer.ID, "tester"); !errors.Is(err, services.ErrNotAgentServer) {
		t.Fatalf("erwartete ErrNotAgentServer, got %v", err)
	}
}

// seedSSHServer legt einen klassischen SSH-Server direkt in der DB an.
func seedSSHServer(t *testing.T, env *testEnv, name string) *domain.Server {
	t.Helper()
	server := &domain.Server{
		Name: name, Host: "10.0.0.9", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
		HostKeyFingerprint: "SHA256:test", PrivateKeyEnc: "enc", Reachable: true,
	}
	if err := repositories.NewServerRepository(env.DB()).Create(server); err != nil {
		t.Fatal(err)
	}
	return server
}

func TestAgentTransportGating(t *testing.T) {
	env := newTestEnv(t)
	server, _, err := env.Servers.CreateAgentServer("gated", "tester")
	if err != nil {
		t.Fatal(err)
	}
	scope := repositories.ScopeAll()

	if _, err := env.Servers.HardenSSH(scope, server.ID, "tester"); !errors.Is(err, services.ErrAgentTransport) {
		t.Fatalf("HardenSSH: erwartete ErrAgentTransport, got %v", err)
	}
	if _, err := env.Servers.UnhardenSSH(scope, server.ID, "tester"); !errors.Is(err, services.ErrAgentTransport) {
		t.Fatalf("UnhardenSSH: erwartete ErrAgentTransport, got %v", err)
	}
	if _, err := env.Servers.Reconnect(scope, services.ReconnectRequest{ID: server.ID, Actor: "tester"}); !errors.Is(err, services.ErrAgentTransport) {
		t.Fatalf("Reconnect: erwartete ErrAgentTransport, got %v", err)
	}
	// Der eingeschränkte Modus zielt auf den SSH-Service-User - auf dem
	// Agent-Kanal bliebe root root, die Umschaltung wäre nur Anschein.
	if _, err := env.Servers.RestrictSudo(scope, server.ID, "tester"); !errors.Is(err, services.ErrAgentTransport) {
		t.Fatalf("RestrictSudo: erwartete ErrAgentTransport, got %v", err)
	}
	// Die Zeit-Aktionen sind dagegen reine Shell-Aktionen und dürfen NICHT
	// am Transport scheitern: ohne verbundenen Hub kommt der Fehler erst aus
	// dem Verbindungsaufbau (R3: Zeit/NTP war fälschlich SSH-gesperrt).
	if _, err := env.Servers.SetTimezone(scope, server.ID, "Europe/Berlin", "tester"); errors.Is(err, services.ErrAgentTransport) {
		t.Fatal("SetTimezone: darf für Agent-Server nicht transport-gesperrt sein")
	}
	if _, err := env.Servers.ConfigureNTP(scope, server.ID, []string{"0.pool.ntp.org"}, "tester"); errors.Is(err, services.ErrAgentTransport) {
		t.Fatal("ConfigureNTP: darf für Agent-Server nicht transport-gesperrt sein")
	}
	// Decommission mit Ziel-Bereinigung ist gesperrt …
	if _, err := env.Servers.Decommission(scope, server.ID, "tester", services.DecommissionOptions{PurgeTarget: true}); !errors.Is(err, services.ErrAgentTransport) {
		t.Fatalf("Decommission(purge): erwartete ErrAgentTransport, got %v", err)
	}
	// … ohne Bereinigung erlaubt und trennt eine aktive Session.
	hub := &fakeHub{online: map[string]bool{server.AgentID: true}}
	env.Servers.WithAgentHub(hub)
	if _, err := env.Servers.Decommission(scope, server.ID, "tester", services.DecommissionOptions{}); err != nil {
		t.Fatalf("Decommission: %v", err)
	}
	if len(hub.disconnected) != 1 {
		t.Fatal("agent-session beim löschen nicht getrennt")
	}
}
