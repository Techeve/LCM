package remote

import (
	"net/netip"
	"sync"
	"testing"
	"time"
)

// fakeClock ist eine von Hand gestellte Uhr für die Limiter-Tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestLimiter() (*upgradeLimiter, *knownAgentIPs, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)}
	known := newKnownAgentIPs()
	known.now = clock.now
	return newUpgradeLimiter(known), known, clock
}

// countAllowed zählt, wie viele von n Upgrades im selben Fenster durchkommen.
func countAllowed(l *upgradeLimiter, ip string, n int) int {
	allowed := 0
	for range n {
		if l.allow(ip) {
			allowed++
		}
	}
	return allowed
}

func TestUpgradeLimitUnbekannteIPNachKarenzzeit(t *testing.T) {
	l, _, clock := newTestLimiter()
	clock.advance(upgradeGracePeriod)
	if got := countAllowed(l, "203.0.113.1", 50); got != upgradeLimitUnknown {
		t.Fatalf("durchgelassen = %d, want %d", got, upgradeLimitUnknown)
	}
	clock.advance(upgradeWindow)
	if !l.allow("203.0.113.1") {
		t.Fatal("im neuen Fenster muss wieder ein Upgrade durchgehen")
	}
}

func TestUpgradeLimitKarenzzeitNachStart(t *testing.T) {
	l, _, clock := newTestLimiter()
	if got := countAllowed(l, "203.0.113.1", 500); got != upgradeLimitGrace {
		t.Fatalf("in der Karenzzeit durchgelassen = %d, want %d", got, upgradeLimitGrace)
	}
	clock.advance(upgradeGracePeriod - time.Second - upgradeWindow)
	clock.advance(upgradeWindow) // neues Fenster, knapp vor Ende der Karenzzeit
	if got := countAllowed(l, "203.0.113.1", 500); got != upgradeLimitGrace {
		t.Fatalf("kurz vor Ende der Karenzzeit durchgelassen = %d, want %d", got, upgradeLimitGrace)
	}
	clock.advance(upgradeWindow) // Karenzzeit vorbei
	if got := countAllowed(l, "203.0.113.1", 500); got != upgradeLimitUnknown {
		t.Fatalf("nach der Karenzzeit durchgelassen = %d, want %d", got, upgradeLimitUnknown)
	}
}

func TestUpgradeLimitBekannterAgentIP(t *testing.T) {
	l, known, clock := newTestLimiter()
	clock.advance(upgradeGracePeriod) // Karenzzeit soll hier nicht helfen
	known.markRemote("203.0.113.7:4711")

	if got := countAllowed(l, "203.0.113.7", 1000); got != upgradeLimitKnown {
		t.Fatalf("bekannte IP durchgelassen = %d, want %d", got, upgradeLimitKnown)
	}
	if got := countAllowed(l, "203.0.113.8", 1000); got != upgradeLimitUnknown {
		t.Fatalf("andere IP durchgelassen = %d, want %d", got, upgradeLimitUnknown)
	}
}

func TestUpgradeLimitBekannteIPVierInSechs(t *testing.T) {
	l, known, clock := newTestLimiter()
	clock.advance(upgradeGracePeriod)
	known.markRemote("[::ffff:203.0.113.7]:4711")
	if got := countAllowed(l, "203.0.113.7", 1000); got != upgradeLimitKnown {
		t.Fatalf("durchgelassen = %d, want %d", got, upgradeLimitKnown)
	}
}

func TestUpgradeLimitMarkeLaeuftNachVierundzwanzigStundenAb(t *testing.T) {
	l, known, clock := newTestLimiter()
	clock.advance(upgradeGracePeriod)
	known.markRemote("203.0.113.7:4711")

	clock.advance(knownAgentIPLifetime - time.Second)
	if !known.has(netip.MustParseAddr("203.0.113.7")) {
		t.Fatal("IP muss kurz vor Ablauf noch bekannt sein")
	}
	clock.advance(time.Second)
	if known.has(netip.MustParseAddr("203.0.113.7")) {
		t.Fatal("IP muss nach 24 Stunden vergessen sein")
	}
	if got := countAllowed(l, "203.0.113.7", 1000); got != upgradeLimitUnknown {
		t.Fatalf("durchgelassen = %d, want %d", got, upgradeLimitUnknown)
	}
}

func TestUpgradeLimitUnlesbareGegenadresseMarkiertNichts(t *testing.T) {
	_, known, _ := newTestLimiter()
	known.markRemote("")
	known.markRemote("kein-host")
	if len(known.expires) != 0 {
		t.Fatalf("bekannte IPs = %d, want 0", len(known.expires))
	}
}

func TestUpgradeLimitBekannteIPsSindBegrenzt(t *testing.T) {
	_, known, clock := newTestLimiter()
	for i := range knownAgentIPMax {
		known.mark(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}))
	}
	extra := netip.MustParseAddr("192.0.2.1")
	known.mark(extra)
	if known.has(extra) || len(known.expires) != knownAgentIPMax {
		t.Fatalf("volle Menge darf nicht wachsen (len=%d)", len(known.expires))
	}
	clock.advance(knownAgentIPLifetime)
	known.mark(extra)
	if !known.has(extra) || len(known.expires) != 1 {
		t.Fatalf("abgelaufene Einträge müssen Platz machen (len=%d)", len(known.expires))
	}
}

func TestUpgradeLimitErfolgreicheAnmeldungMarkiertIP(t *testing.T) {
	hub, server, secret := newTestHub(t)
	hook := &agentHook{hub: hub}
	good := netip.MustParseAddr("203.0.113.7")
	bad := netip.MustParseAddr("203.0.113.9")

	fail := hub.broker.NewClient(nil, "test", server.AgentID, false)
	fail.Net.Remote = "203.0.113.9:5000"
	if hook.OnConnectAuthenticate(fail, connectPacket(server.AgentID, "falsch")) {
		t.Fatal("falsches Secret darf nicht durch")
	}
	wrongID := hub.broker.NewClient(nil, "test", "anders", false)
	wrongID.Net.Remote = "203.0.113.9:5001"
	hook.OnConnectAuthenticate(wrongID, connectPacket(server.AgentID, secret))
	if hub.knownIPs.has(bad) {
		t.Fatal("fehlgeschlagene Anmeldung darf die IP nicht markieren")
	}

	ok := hub.broker.NewClient(nil, "test", server.AgentID, false)
	ok.Net.Remote = "203.0.113.7:5002"
	if !hook.OnConnectAuthenticate(ok, connectPacket(server.AgentID, secret)) {
		t.Fatal("gültige Anmeldung muss durch")
	}
	if !hub.knownIPs.has(good) {
		t.Fatal("erfolgreiche Anmeldung muss die IP markieren")
	}
}
