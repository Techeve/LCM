package remote_test

import (
	"log/slog"
	"testing"
	"time"

	"LCM/internal/agent"
	"LCM/internal/core/domain"
	"LCM/internal/remote"
)

// joinAgent verbindet einen echten Agent mit dem Hub und liefert ihn zurück.
func joinAgent(t *testing.T, l hubListener) *agent.Client {
	t.Helper()
	cfg := &agent.Config{URL: l.url, AgentID: l.server.AgentID, Secret: l.secret}
	client := agent.NewClient(cfg, slog.Default(), "test").WithJobsDir(t.TempDir())
	go func() { _ = client.Run() }()
	t.Cleanup(client.Stop)
	waitFor(t, 10*time.Second, "agent online", func() bool { return l.hub.Online(l.server.AgentID) })
	return client
}

func waitDrops(t *testing.T, hub *remote.Hub, agentID string, n int) domain.AgentLink {
	t.Helper()
	var link domain.AgentLink
	waitFor(t, 10*time.Second, "Trennung verbucht", func() bool {
		link = hub.LinkStats(agentID)
		return link.Disconnects >= n
	})
	return link
}

// Verbucht der Hub die Gründe, aus denen ein echter Agent getrennt wird? Der
// Abschied des Agents und das Trennen durch LCM sind gewollt und färben die
// Ampel nicht.
func TestEchterAgentTrennungenMitUrsache(t *testing.T) {
	if testing.Short() {
		t.Skip("integrationstest (echter broker + websocket)")
	}
	l := listeningHub(t, nil, "")
	t.Cleanup(l.hub.Close)
	id := l.server.AgentID

	client := joinAgent(t, l)
	if link := l.hub.LinkStats(id); link.Connects != 1 || link.Disconnects != 0 {
		t.Fatalf("nach dem Start: %+v", link)
	}

	// Der Agent meldet sich ab (systemctl stop).
	client.Stop()
	link := waitDrops(t, l.hub, id, 1)
	if link.UnintendedLastHour != 0 {
		t.Fatalf("Abschied des Agents zählt als ungewollt: %+v", link)
	}

	// Agent-Zugang aus: LCM trennt.
	joinAgent(t, l)
	l.hub.SetAccepting(false)
	link = waitDrops(t, l.hub, id, 2)
	l.hub.SetAccepting(true)
	if link.UnintendedLastHour != 0 {
		t.Fatalf("Zugang aus zählt als ungewollt: %+v", link)
	}

	// Token widerrufen: Der Agent kommt nicht wieder.
	waitFor(t, 15*time.Second, "agent wieder online", func() bool { return l.hub.Online(id) })
	l.hub.Disconnect(id)
	link = waitDrops(t, l.hub, id, 3)
	if link.UnintendedLastHour != 0 || link.Connects < 3 {
		t.Fatalf("nach Widerruf: %+v", link)
	}
}
