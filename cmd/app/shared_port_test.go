package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/infrastructure/tlsx/tlsxtest"
	"LCM/internal/mcp"
	"LCM/internal/sniroute"
)

var testAddresses = publicAddresses{
	UI:           "https://ui.example.test",
	Agent:        "https://agent.example.test:9320",
	MCP:          "https://MCP.example.test",
	AgentEnabled: true,
	MCPEnabled:   true,
}

func TestRouteTarget(t *testing.T) {
	tests := []struct {
		name       string
		addr       publicAddresses
		serverName string
		want       string
	}{
		{"UI", testAddresses, "ui.example.test", targetUI},
		{"Agent, Port der Adresse zählt nicht", testAddresses, "agent.example.test", targetAgent},
		{"MCP, Groß-/Kleinschreibung egal", testAddresses, "mcp.example.test", targetMCP},
		{"SNI in Großbuchstaben mit Punkt", testAddresses, "Agent.Example.Test.", targetAgent},
		{"ohne Namen", testAddresses, "", targetUI},
		{"unbekannter Name", testAddresses, "anders.example.test", targetUI},
		{"keine Adressen eingetragen", publicAddresses{}, "agent.example.test", targetUI},
		{"gleicher Name: UI vor Agent", publicAddresses{UI: "https://lcm.example.test", Agent: "https://lcm.example.test:9320", AgentEnabled: true}, "lcm.example.test", targetUI},
		{"gleicher Name: Agent vor MCP", publicAddresses{Agent: "https://x.example.test", MCP: "https://x.example.test", AgentEnabled: true}, "x.example.test", targetAgent},
		{"Agent aus: Name wird abgewiesen", withoutAgent(testAddresses), "agent.example.test", sniroute.Refuse},
		{"MCP aus: Name wird abgewiesen", withoutMCP(testAddresses), "mcp.example.test", sniroute.Refuse},
		{"Agent aus: die Oberfläche bleibt", withoutAgent(testAddresses), "ui.example.test", targetUI},
		{"Agent aus: ein anderer Name geht an die Oberfläche", withoutAgent(testAddresses), "anders.example.test", targetUI},
		{"gleicher Name wie die Oberfläche: die Oberfläche geht vor", publicAddresses{UI: "https://x.example.test", MCP: "https://x.example.test"}, "x.example.test", targetUI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeTarget(tt.addr, tt.serverName); got != tt.want {
				t.Errorf("routeTarget = %q, erwartet %q", got, tt.want)
			}
		})
	}
}

func withoutAgent(a publicAddresses) publicAddresses { a.AgentEnabled = false; return a }
func withoutMCP(a publicAddresses) publicAddresses   { a.MCPEnabled = false; return a }

func TestSharedRoutes(t *testing.T) {
	routes := sharedRoutes(publicAddresses{
		UI: "https://lcm.example.test", Agent: "https://lcm.example.test:9320", MCP: "https://mcp.example.test",
		AgentEnabled: true,
	})
	want := []sharedRoute{
		{Service: targetUI, Host: "lcm.example.test"},
		{Service: targetAgent, Host: "lcm.example.test", ShadowedBy: targetUI},
		{Service: targetMCP, Host: "mcp.example.test", Inactive: true}, // MCP ist aus
	}
	if fmt.Sprint(routes) != fmt.Sprint(want) {
		t.Errorf("Zuordnung = %+v, erwartet %+v", routes, want)
	}
	if got := sharedRoutes(publicAddresses{})[0]; got.Host != "" {
		t.Errorf("ohne Adresse: %+v", got)
	}
}

func TestBindHint(t *testing.T) {
	denied := fmt.Errorf("listen tcp :443: %w", fs.ErrPermission)
	if hint := bindHint(":443", denied); !strings.Contains(hint, "CAP_NET_BIND_SERVICE") || !strings.Contains(hint, "systemctl edit lcm") {
		t.Errorf("Hinweis für Port 443 = %q", hint)
	}
	if hint := bindHint(":8443", denied); hint != "" {
		t.Errorf("Port über 1023 braucht keine Capability: %q", hint)
	}
	if hint := bindHint(":443", errors.New("address already in use")); hint != "" {
		t.Errorf("anderer Fehler: %q", hint)
	}
}

// whoAmI antwortet mit dem Dienst und dem, was die Anfrage über TLS verrät.
func whoAmI(service string) *fiber.App {
	app := fiber.New()
	app.Use(middlewares.ResolveClientIP(nil))
	app.Get("/*", func(c fiber.Ctx) error {
		middlewares.SetSessionCookies(c, "token", "csrf", time.Hour)
		return c.SendString(fmt.Sprintf("%s https=%v scheme=%s", service, middlewares.RequestIsHTTPS(c), c.Scheme()))
	})
	return app
}

type noServers struct{}

func (noServers) Servers() ([]mcp.ServerView, error)           { return nil, nil }
func (noServers) Server(string) (*mcp.ServerView, bool, error) { return nil, false, nil }

// sharedFixture ist ein laufender gemeinsamer Port mit allen drei Diensten.
type sharedFixture struct {
	port      *sharedPort
	addr      string // Adresse des gemeinsamen Ports
	pool      *tlsx.Pool
	mu        sync.Mutex
	addresses publicAddresses
	ui, agent *fiber.App
}

func (f *sharedFixture) currentAddresses() publicAddresses {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addresses
}

func (f *sharedFixture) setAddresses(a publicAddresses) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addresses = a
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

func newTestPool(t *testing.T) *tlsx.Pool {
	t.Helper()
	dir := t.TempDir()
	tlsxtest.ServerCert(t, dir, time.Now().Add(-time.Hour))
	store, err := tlsx.NewCertStore(filepath.Join(dir, tlsx.CertFileName), filepath.Join(dir, tlsx.KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	return tlsx.NewPool(store)
}

func newSharedFixture(t *testing.T) *sharedFixture {
	t.Helper()
	f := &sharedFixture{addr: freeAddr(t), pool: newTestPool(t), addresses: testAddresses}
	f.port = newSharedPort(f.pool, f.currentAddresses)
	t.Cleanup(f.port.Close)

	mcpServer := mcp.NewServer(mcp.NewHandler(noServers{}, func(string) bool { return true }, "LCM", "test"),
		slog.New(slog.DiscardHandler))
	f.port.AttachMCP(mcpServer)
	_ = mcpServer.Apply(true, "127.0.0.1", mustPort(t, freeAddr(t)), false)
	t.Cleanup(mcpServer.Shutdown)

	f.ui, f.agent = whoAmI(targetUI), whoAmI(targetAgent)
	t.Cleanup(func() { _ = f.ui.Shutdown(); _ = f.agent.Shutdown() })
	f.port.SetApps(f.ui, f.agent)
	if err := f.port.Open(f.addr); err != nil {
		t.Fatal(err)
	}
	waitForAddr(t, f.addr)
	return f
}

func mustPort(t *testing.T, addr string) int {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if _, err := fmt.Sscan(port, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func waitForAddr(t *testing.T, addr string) {
	t.Helper()
	for range 100 {
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s nimmt keine Verbindungen an", addr)
}

// request schickt einen HTTPS-Request an addr. Mit serverName sendet der
// Client diesen Namen als SNI, ohne verbindet er sich per IP und sendet keinen.
func request(t *testing.T, addr, serverName, method, path string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true, ServerName: serverName},
		DisableKeepAlives: true,
	}}
	req, err := http.NewRequest(method, "https://"+addr+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer x")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Anfrage mit Name %q: %v", serverName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestSharedPortVerteiltNachName(t *testing.T) {
	f := newSharedFixture(t)

	for _, tt := range []struct{ serverName, wantPrefix string }{
		{"ui.example.test", "ui "},
		{"agent.example.test", "agent "},
		{"", "ui "},                       // ohne SNI
		{"unbekannt.example.test", "ui "}, // unbekannter Name
	} {
		resp, body := request(t, f.addr, tt.serverName, "GET", "/")
		if resp.StatusCode != 200 || !strings.HasPrefix(body, tt.wantPrefix) {
			t.Errorf("Name %q: %d %q, erwartet Dienst %q", tt.serverName, resp.StatusCode, body, tt.wantPrefix)
		}
	}

	resp, body := request(t, f.addr, "mcp.example.test", "POST", "/mcp")
	if resp.StatusCode != 200 || !strings.Contains(body, `"jsonrpc"`) {
		t.Errorf("MCP-Name: %d %q, erwartet eine JSON-RPC-Antwort", resp.StatusCode, body)
	}
}

// Ein ausgeschalteter Dienst bekommt am gemeinsamen Port keine Antwort: Die
// Verbindung wird nach dem Handshake geschlossen und fällt nicht auf die
// Oberfläche zurück. Die übrigen Namen laufen weiter.
func TestSharedPortAusgeschalteterDienstAntwortetNicht(t *testing.T) {
	f := newSharedFixture(t)
	f.setAddresses(withoutAgent(withoutMCP(testAddresses)))

	for _, name := range []string{"agent.example.test", "mcp.example.test"} {
		if err := requestFails(f.addr, name); err == nil {
			t.Errorf("Name %q: eine Antwort, erwartet eine geschlossene Verbindung", name)
		}
	}
	if _, body := request(t, f.addr, "ui.example.test", "GET", "/"); !strings.HasPrefix(body, "ui ") {
		t.Errorf("Oberfläche: %q", body)
	}
	if _, body := request(t, f.addr, "unbekannt.example.test", "GET", "/"); !strings.HasPrefix(body, "ui ") {
		t.Errorf("unbekannter Name: %q, erwartet die Oberfläche", body)
	}

	f.setAddresses(testAddresses) // wieder an
	if _, body := request(t, f.addr, "agent.example.test", "GET", "/"); !strings.HasPrefix(body, "agent ") {
		t.Errorf("Agent wieder an: %q", body)
	}
}

// requestFails schickt einen Request mit diesem Namen und liefert den Fehler (nil: Antwort bekommen).
func requestFails(addr, serverName string) error {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true, ServerName: serverName},
		DisableKeepAlives: true,
	}}
	resp, err := client.Get("https://" + addr + "/")
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

// fasthttp erkennt die *tls.Conn der Weiche als TLS: Die App sieht https, und
// die Sitzungs-Cookies bekommen Secure.
func TestSharedPortApplikationSiehtHTTPS(t *testing.T) {
	f := newSharedFixture(t)

	resp, body := request(t, f.addr, "ui.example.test", "GET", "/")
	if want := "ui https=true scheme=https"; body != want {
		t.Errorf("Antwort = %q, erwartet %q", body, want)
	}
	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) == 0 {
		t.Fatal("die App setzte keine Cookies")
	}
	for _, c := range cookies {
		if !strings.Contains(strings.ToLower(c), "; secure") {
			t.Errorf("Cookie ohne Secure: %s", c)
		}
	}
}

func TestSharedPortZuordnungFolgtGeaendertenAdressenOhneNeustart(t *testing.T) {
	f := newSharedFixture(t)

	if _, body := request(t, f.addr, "neu.example.test", "GET", "/"); !strings.HasPrefix(body, "ui ") {
		t.Fatalf("vor der Änderung: %q", body)
	}
	changed := testAddresses
	changed.Agent = "https://neu.example.test"
	f.setAddresses(changed)
	if _, body := request(t, f.addr, "neu.example.test", "GET", "/"); !strings.HasPrefix(body, "agent ") {
		t.Errorf("nach der Änderung: %q, erwartet den Agent", body)
	}
	if _, body := request(t, f.addr, "agent.example.test", "GET", "/"); !strings.HasPrefix(body, "ui ") {
		t.Errorf("alter Name: %q, erwartet die Weboberfläche", body)
	}
}

// Dieselbe App bedient ihren eigenen Port und den gemeinsamen zugleich.
func TestSharedPortNebenDemEigenenPortDerApp(t *testing.T) {
	f := &sharedFixture{addr: freeAddr(t), pool: newTestPool(t), addresses: testAddresses}
	f.port = newSharedPort(f.pool, f.currentAddresses)
	t.Cleanup(f.port.Close)
	ui := whoAmI(targetUI)
	t.Cleanup(func() { _ = ui.Shutdown() })

	ownAddr := freeAddr(t)
	go func() {
		_ = ui.Listen(ownAddr, fiber.ListenConfig{DisableStartupMessage: true, TLSConfig: f.pool.TLSConfig()})
	}()
	waitForAddr(t, ownAddr)
	f.port.SetApps(ui, nil)
	if err := f.port.Open(f.addr); err != nil {
		t.Fatal(err)
	}
	waitForAddr(t, f.addr)

	for _, addr := range []string{ownAddr, f.addr} {
		if _, body := request(t, addr, "ui.example.test", "GET", "/"); body != "ui https=true scheme=https" {
			t.Errorf("%s: %q", addr, body)
		}
	}
}

func TestSharedPortStatus(t *testing.T) {
	f := newSharedFixture(t)
	status := f.port.Status()
	if status.Address != f.addr || !status.Listening || len(status.Routes) != 3 {
		t.Errorf("Status = %+v", status)
	}
	f.port.Close()
	if f.port.Status() != nil {
		t.Error("ein geschlossener Port hat einen Status")
	}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	for range 100 {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Bedingung trat nicht ein")
}

func TestSharedPortCloseGibtDenPortFrei(t *testing.T) {
	f := newSharedFixture(t)
	f.port.Close()
	waitUntil(t, func() bool {
		conn, err := net.Dial("tcp", f.addr)
		if err == nil {
			_ = conn.Close()
		}
		return err != nil
	})
}

// Ist der Port belegt, läuft der Rest weiter: Open meldet nur, dass er nicht aufging.
func TestSharedPortBelegterPortStoertNicht(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	p := newSharedPort(newTestPool(t), func() publicAddresses { return testAddresses })
	defer p.Close()

	p.SetApps(whoAmI(targetUI), nil)
	if err := p.Open(taken.Addr().String()); err == nil {
		t.Fatal("Open meldet keinen Fehler für einen belegten Port")
	}
	if status := p.Status(); status == nil || status.Listening {
		t.Errorf("Status = %+v, erwartet: gewünscht, aber nicht geöffnet", status)
	}
}

func TestSharedPortOhneEinstellungIstNil(t *testing.T) {
	var p *sharedPort
	p.SetApps(nil, nil) // darf nicht abstürzen
	if err := p.Open(":443"); err != nil {
		t.Error(err)
	}
	if commit, abort, err := p.Prepare(":443"); err != nil || commit == nil || abort == nil {
		t.Error("Prepare für nil")
	}
	if p.Status() != nil {
		t.Error("Status für nil")
	}
	p.Close()
}

// Der gemeinsame Port schließt und öffnet sich zur Laufzeit, auch auf einem
// anderen Port, und MCP hängt danach wieder dran.
func TestSharedPortOeffnetUndSchliesstZurLaufzeit(t *testing.T) {
	f := newSharedFixture(t)

	commit, _, err := f.port.Prepare("")
	if err != nil {
		t.Fatal(err)
	}
	commit()
	waitUntil(t, func() bool { _, err := net.Dial("tcp", f.addr); return err != nil })
	if f.port.Status() != nil {
		t.Error("ein geschlossener Port hat einen Status")
	}

	newAddr := freeAddr(t)
	commit, _, err = f.port.Prepare(newAddr)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	waitForAddr(t, newAddr)
	for _, tt := range []struct{ serverName, wantPrefix string }{
		{"ui.example.test", "ui "}, {"agent.example.test", "agent "},
	} {
		if _, body := request(t, newAddr, tt.serverName, "GET", "/"); !strings.HasPrefix(body, tt.wantPrefix) {
			t.Errorf("Name %q: %q", tt.serverName, body)
		}
	}
	if resp, body := request(t, newAddr, "mcp.example.test", "POST", "/mcp"); resp.StatusCode != 200 || !strings.Contains(body, `"jsonrpc"`) {
		t.Errorf("MCP nach dem Wiederöffnen: %d %q", resp.StatusCode, body)
	}
	if status := f.port.Status(); status == nil || status.Address != newAddr || !status.Listening {
		t.Errorf("Status = %+v", status)
	}
}

// Ein belegter neuer Port lässt den laufenden gemeinsamen Port in Ruhe.
func TestSharedPortWechselMitBelegtemPortBehaeltDenAlten(t *testing.T) {
	f := newSharedFixture(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()

	if _, _, err := f.port.Prepare(taken.Addr().String()); err == nil {
		t.Fatal("kein Fehler für den belegten Port")
	}
	if _, body := request(t, f.addr, "ui.example.test", "GET", "/"); !strings.HasPrefix(body, "ui ") {
		t.Errorf("der alte Port antwortet %q", body)
	}
}

func TestSharedPortGleicheAdresseIstLeer(t *testing.T) {
	f := newSharedFixture(t)
	commit, abort, err := f.port.Prepare(f.addr)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	abort()
	if _, body := request(t, f.addr, "ui.example.test", "GET", "/"); !strings.HasPrefix(body, "ui ") {
		t.Errorf("Antwort = %q", body)
	}
}
