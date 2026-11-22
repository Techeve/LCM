package main

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/mcp"
)

// nameApp antwortet jeder Anfrage mit seinem Namen.
func nameApp(name string) *fiber.App {
	app := fiber.New()
	app.Get("/*", func(c fiber.Ctx) error { return c.SendString(name) })
	return app
}

// httpGet holt http://addr/ und liefert den Rumpf; leer, wenn niemand antwortet.
func httpGet(addr string) string {
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func listenTaken(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func mustBind(t *testing.T, addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("%s ist nicht frei: %v", addr, err)
	}
	_ = ln.Close()
}

func openListener(t *testing.T, grace time.Duration) (*portListener, string) {
	t.Helper()
	p := newPortListener("Test", nameApp("dienst"), nil, grace)
	t.Cleanup(p.Close)
	addr := freeAddr(t)
	if err := p.Open(addr); err != nil {
		t.Fatal(err)
	}
	waitForAddr(t, addr)
	return p, addr
}

func TestPortListenerWechselBedientNeuenPortUndSchliesstDenAltenNachDerFrist(t *testing.T) {
	p, oldAddr := openListener(t, 100*time.Millisecond)

	newAddr := freeAddr(t)
	commit, _, err := p.Prepare(newAddr)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	waitForAddr(t, newAddr)
	if got := httpGet(newAddr); got != "dienst" {
		t.Errorf("neuer Port antwortet %q", got)
	}
	if p.current() != newAddr {
		t.Errorf("current = %q", p.current())
	}
	// Nach der Frist ist der alte Port zu.
	waitUntil(t, func() bool {
		conn, err := net.Dial("tcp", oldAddr)
		if err == nil {
			_ = conn.Close()
		}
		return err != nil
	})
}

// Während der Frist antworten beide Ports: Der Browser, der den Wechsel
// ausgelöst hat, bekommt seine Antwort.
func TestPortListenerAlterPortBleibtWaehrendDerFristOffen(t *testing.T) {
	p, oldAddr := openListener(t, time.Hour)

	newAddr := freeAddr(t)
	commit, _, err := p.Prepare(newAddr)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	waitForAddr(t, newAddr)
	if got := httpGet(oldAddr); got != "dienst" {
		t.Errorf("alter Port antwortet %q", got)
	}
}

func TestPortListenerBindFehlerBehaeltDenAltenPort(t *testing.T) {
	p, oldAddr := openListener(t, 0)

	_, _, err := p.Prepare(listenTaken(t))
	if err == nil {
		t.Fatal("kein Fehler für einen belegten Port")
	}
	if !strings.Contains(err.Error(), "Test") {
		t.Errorf("der Fehler nennt den Dienst nicht: %v", err)
	}
	if p.current() != oldAddr || httpGet(oldAddr) != "dienst" {
		t.Error("der alte Port läuft nicht mehr")
	}
}

func TestPortListenerAbortGibtDenPortFrei(t *testing.T) {
	p, _ := openListener(t, 0)
	newAddr := freeAddr(t)
	_, abort, err := p.Prepare(newAddr)
	if err != nil {
		t.Fatal(err)
	}
	abort()
	mustBind(t, newAddr)
	if p.current() == newAddr {
		t.Error("abgebrochener Wechsel wurde übernommen")
	}
}

func TestPortListenerLeereAdresseSchaltetAus(t *testing.T) {
	p, addr := openListener(t, 0)
	commit, _, err := p.Prepare("")
	if err != nil {
		t.Fatal(err)
	}
	commit()
	waitUntil(t, func() bool { _, err := net.Dial("tcp", addr); return err != nil })
	if p.current() != "" {
		t.Errorf("current = %q", p.current())
	}
}

func TestPortListenerNilIstHarmlos(t *testing.T) {
	var p *portListener
	if err := p.Open(":1"); err != nil {
		t.Error(err)
	}
	commit, abort, err := p.Prepare(":1")
	if err != nil {
		t.Error(err)
	}
	commit()
	abort()
	p.Close()
}

// runtimeFixture: UI, Agent und MCP auf freien Ports, ohne gemeinsamen Port.
type runtimeFixture struct {
	r                        *runtimeListeners
	uiAddr, agentAddr, mcpPt string
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
	ui, uiAddr := openListener(t, 0)
	agent, agentAddr := openListener(t, 0)
	server := mcp.NewServer(mcp.NewHandler(noServers{}, func(string) bool { return true }, "LCM", "test"),
		slog.New(slog.DiscardHandler))
	t.Cleanup(server.Shutdown)
	mcpAddr := freeAddr(t)
	_ = server.Apply(true, "127.0.0.1", mustPort(t, mcpAddr), false)
	waitForAddr(t, mcpAddr)
	return &runtimeFixture{
		r:      &runtimeListeners{ui: ui, agent: agent, mcp: server, uiHost: "127.0.0.1", agentHost: "127.0.0.1"},
		uiAddr: uiAddr, agentAddr: agentAddr, mcpPt: mcpAddr,
	}
}

func (f *runtimeFixture) plan(ui, agent, mcpPort string) services.AddressPlan {
	return services.AddressPlan{
		UIPort: listenPort(ui), AgentPort: listenPort(agent), MCPPort: listenPort(mcpPort),
		MCPEnabled: true, AgentEnabled: true,
	}
}

// Ein Wechsel aller drei Ports ohne Neustart: Danach antworten die neuen Ports.
func TestRuntimeListenersWechselAllerPorts(t *testing.T) {
	f := newRuntimeFixture(t)
	ui, agent, mcpAddr := freeAddr(t), freeAddr(t), freeAddr(t)

	change, err := f.r.Prepare(f.plan(ui, agent, mcpAddr))
	if err != nil {
		t.Fatal(err)
	}
	change.Commit()

	for _, addr := range []string{ui, agent} {
		waitForAddr(t, addr)
		if got := httpGet(addr); got != "dienst" {
			t.Errorf("%s antwortet %q", addr, got)
		}
	}
	waitForAddr(t, mcpAddr)
	if httpGet(mcpAddr) == "" {
		t.Error("MCP antwortet am neuen Port nicht")
	}
	// Der alte MCP-Port ist frei.
	waitUntil(t, func() bool { _, err := net.Dial("tcp", f.mcpPt); return err != nil })
}

// Scheitert die Oberfläche, gibt der Rest seine vorab gebundenen Ports wieder frei
// und alles bleibt, wie es war.
func TestRuntimeListenersFehlerRolltAllesZurueck(t *testing.T) {
	f := newRuntimeFixture(t)
	agent, mcpAddr := freeAddr(t), freeAddr(t)

	_, err := f.r.Prepare(f.plan(listenTaken(t), agent, mcpAddr))
	if err == nil {
		t.Fatal("kein Fehler für den belegten UI-Port")
	}
	if !strings.Contains(err.Error(), "Oberfläche") && !strings.Contains(err.Error(), "Test") {
		t.Errorf("der Fehler nennt den Dienst nicht: %v", err)
	}
	mustBind(t, agent)
	mustBind(t, mcpAddr)
	for _, addr := range []string{f.uiAddr, f.agentAddr} {
		if httpGet(addr) != "dienst" {
			t.Errorf("%s läuft nicht mehr", addr)
		}
	}
	if httpGet(f.mcpPt) == "" {
		t.Error("MCP läuft nicht mehr am alten Port")
	}
}

// Dasselbe, wenn schon der MCP-Port belegt ist.
func TestRuntimeListenersBelegterMCPPortBehaeltAlles(t *testing.T) {
	f := newRuntimeFixture(t)
	ui, agent := freeAddr(t), freeAddr(t)

	if _, err := f.r.Prepare(f.plan(ui, agent, listenTaken(t))); err == nil {
		t.Fatal("kein Fehler für den belegten MCP-Port")
	}
	mustBind(t, ui)
	mustBind(t, agent)
	if httpGet(f.mcpPt) == "" || httpGet(f.uiAddr) != "dienst" {
		t.Error("etwas läuft nicht mehr, obwohl der Wechsel scheiterte")
	}
}

// Ein Plan, der nichts ändert, bindet nichts.
func TestRuntimeListenersUnveraenderterPlanIstLeer(t *testing.T) {
	f := newRuntimeFixture(t)
	change, err := f.r.Prepare(f.plan(f.uiAddr, f.agentAddr, f.mcpPt))
	if err != nil {
		t.Fatal(err)
	}
	change.Commit()
	if httpGet(f.uiAddr) != "dienst" || httpGet(f.agentAddr) != "dienst" {
		t.Error("ein unveränderter Plan hat etwas gestört")
	}
}

// Der Port der Oberfläche wird bei jedem Wechsel für Healthcheck und Probe vermerkt.
func TestPortListenerMeldetNeuenPortAnOnOpen(t *testing.T) {
	var noted []string
	p, _ := openListener(t, 0)
	p.onOpen = func(addr string) { noted = append(noted, addr) }

	newAddr := freeAddr(t)
	commit, _, err := p.Prepare(newAddr)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if len(noted) != 1 || noted[0] != newAddr {
		t.Errorf("onOpen = %v", noted)
	}
}

// Der Schalter des Agenten schließt seinen Port und trennt die Agenten, aber erst
// mit dem Commit: Scheitert ein Bind, bleibt alles verbunden.
func TestRuntimeListenersAgentAusSchliesstPortUndSchaltetDenZugang(t *testing.T) {
	f := newRuntimeFixture(t)
	var gate []bool
	f.r.agentGate = func(on bool) { gate = append(gate, on) }

	off := f.plan(f.uiAddr, f.agentAddr, f.mcpPt)
	off.AgentPort, off.AgentEnabled = 0, false

	rolledBack, err := f.r.Prepare(off)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack.Rollback()
	if len(gate) != 0 || httpGet(f.agentAddr) != "dienst" {
		t.Fatalf("ein zurückgerollter Wechsel schaltete: Tor %v", gate)
	}

	change, err := f.r.Prepare(off)
	if err != nil {
		t.Fatal(err)
	}
	change.Commit()
	waitUntil(t, func() bool { _, err := net.Dial("tcp", f.agentAddr); return err != nil })
	if len(gate) != 1 || gate[0] {
		t.Errorf("Tor = %v, erwartet [false]", gate)
	}
	if httpGet(f.uiAddr) != "dienst" {
		t.Error("die Oberfläche läuft nicht mehr")
	}

	on := f.plan(f.uiAddr, f.agentAddr, f.mcpPt)
	on.AgentEnabled = true
	change, err = f.r.Prepare(on)
	if err != nil {
		t.Fatal(err)
	}
	change.Commit()
	waitForAddr(t, f.agentAddr)
	if len(gate) != 2 || !gate[1] || httpGet(f.agentAddr) != "dienst" {
		t.Errorf("Agent wieder an: Tor %v", gate)
	}
}

// MCP bindet an den Host der Oberfläche; ohne Angabe wie diese an alle Interfaces.
func TestMCPBindetAnDenHostDerOberflaeche(t *testing.T) {
	if got := mcpBindHost("192.0.2.10"); got != "192.0.2.10" {
		t.Errorf("mit Host = %q", got)
	}
	if got := mcpBindHost(""); got != "0.0.0.0" {
		t.Errorf("ohne Host = %q", got)
	}
	r := &runtimeListeners{uiHost: "127.0.0.1"}
	if got := r.mcpHost(); got != "127.0.0.1" {
		t.Errorf("mcpHost = %q", got)
	}
}
