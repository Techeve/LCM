package middlewares

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/netfilter"
)

// testApp baut eine minimale Fiber-App mit der Allowlist-Middleware.
func testApp(t *testing.T, entries []string, trustProxy bool) *fiber.App {
	t.Helper()
	// Der Test-Harness meldet 0.0.0.0 als Gegenstelle - sie spielt den Proxy.
	proxies, err := netfilter.Parse([]string{"0.0.0.0/32"})
	if err != nil {
		t.Fatal(err)
	}
	return testAppWith(t, holderOf(t, entries), netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: trustProxy, Proxies: proxies}))
}

func holderOf(t *testing.T, entries []string) *netfilter.Holder {
	t.Helper()
	list, err := netfilter.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	holder := &netfilter.Holder{}
	holder.Store(list)
	return holder
}

func testAppWith(t *testing.T, holder *netfilter.Holder, trust *netfilter.ProxyTrustHolder) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: apiErrorHandler})
	app.Use(ResolveClientIP(trust))
	app.Use(IPAllowlist(holder, netfilter.EndpointUI, slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/api/v1/agent/download/:name", func(c fiber.Ctx) error { return c.SendString("binary") })
	return app
}

func statusOf(t *testing.T, app *fiber.App, path, xff string) int {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

// apiErrorHandler schreibt apierr-Fehler wie der zentrale Handler des Routers:
// Status des Fehlers, Code im Rumpf.
func apiErrorHandler(c fiber.Ctx, err error) error {
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	return c.Status(apiErr.Status).JSON(fiber.Map{"code": apiErr.Code})
}

// Mit trustProxyHeader entscheidet der X-Forwarded-For-Header - deterministisch
// unabhängig von der Peer-Adresse des Test-Harness.
func TestIPAllowlistMiddlewareForwardedFor(t *testing.T) {
	app := testApp(t, []string{"203.0.113.0/24"}, true)

	tests := []struct {
		xff    string
		status int
	}{
		{xff: "203.0.113.9", status: fiber.StatusOK},
		// Der rechte Eintrag kommt vom Proxy, der linke vom Client: Es zählt
		// der rechte.
		{xff: "203.0.113.9, 10.0.0.1", status: fiber.StatusForbidden},
		{xff: "8.8.8.8, 203.0.113.9", status: fiber.StatusOK},
		{xff: "8.8.8.8", status: fiber.StatusForbidden},
		{xff: "10.0.0.5", status: fiber.StatusForbidden},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("X-Forwarded-For", tt.xff)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tt.status {
			t.Errorf("xff=%q: status=%d, erwartet %d", tt.xff, resp.StatusCode, tt.status)
		}
	}
}

// Fehlt bei aktiviertem Trust der Header komplett und ist die Peer-Adresse des
// Test-Harness nicht in der Allowlist, wird abgewiesen (403).
func TestIPAllowlistMiddlewareDeniesUnknownWithoutHeader(t *testing.T) {
	app := testApp(t, []string{"203.0.113.9"}, true)
	req := httptest.NewRequest("GET", "/x", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("ohne passenden Header/Peer erwartet 403, bekam %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), errIPDenied.Code) {
		t.Errorf("Antwort %s enthält nicht den Code %s", body, errIPDenied.Code)
	}
}

// Hängt der Proxy (wie HAProxy) eine eigene X-Forwarded-For-Zeile an, zählt
// sie - nicht die erste, die der Client mitgeschickt hat.
func TestIPAllowlistMiddlewareReadsEveryForwardedForLine(t *testing.T) {
	app := testApp(t, []string{"203.0.113.0/24"}, true)
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Add("X-Forwarded-For", "203.0.113.9")
	req.Header.Add("X-Forwarded-For", "8.8.8.8")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("Zeile des Clients zählte: status=%d, erwartet 403", resp.StatusCode)
	}
}

// TestIgnoredForwardedForWarnsHourly: Kommt X-Forwarded-For von einer
// Gegenstelle außerhalb von trusted_proxies, warnt LCM - aber nur einmal je
// Stunde, nicht bei jeder Anfrage.
func TestIgnoredForwardedForWarnsHourly(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ignoredForwardedForWarning.Store(0)

	proxies, err := netfilter.Parse([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(ResolveClientIP(netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: proxies})))
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString(ClientIP(c)) })

	for range 3 {
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if body, _ := io.ReadAll(resp.Body); string(body) != "0.0.0.0" {
			t.Fatalf("Kopfzeile einer fremden Gegenstelle zählte: %s", body)
		}
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Errorf("%d Warnungen, erwartet genau eine:\n%s", n, logs.String())
	}
}

// Eine leere Liste lässt alles zu, ein nil-Halter ebenso (Apps ohne Liste).
func TestIPAllowlistEmptyListAllowsEveryone(t *testing.T) {
	for name, holder := range map[string]*netfilter.Holder{"leer": {}, "nil": nil} {
		app := testAppWith(t, holder, nil)
		if got := statusOf(t, app, "/x", ""); got != fiber.StatusOK {
			t.Errorf("%s: status=%d, erwartet 200", name, got)
		}
	}
}

// Die Liste gilt ab dem Austausch, ohne neue App und ohne Neustart.
func TestIPAllowlistChangeTakesEffectAtRuntime(t *testing.T) {
	holder := &netfilter.Holder{}
	app := testAppWith(t, holder, nil)
	if got := statusOf(t, app, "/x", ""); got != fiber.StatusOK {
		t.Fatalf("ohne Liste: status=%d", got)
	}
	holder.Store(holderOf(t, []string{"203.0.113.0/24"}).Load())
	if got := statusOf(t, app, "/x", ""); got != fiber.StatusForbidden {
		t.Errorf("mit fremdem Netz: status=%d, erwartet 403", got)
	}
	holder.Store(netfilter.Allowlist{})
	if got := statusOf(t, app, "/x", ""); got != fiber.StatusOK {
		t.Errorf("nach dem Leeren: status=%d, erwartet 200", got)
	}
}

// Von einer NICHT vertrauten Gegenstelle zählt X-Forwarded-For nicht: Wer die
// Kopfzeile selbst setzt, kommt damit nicht in die Liste.
func TestIPAllowlistIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	elsewhere, err := netfilter.Parse([]string{"192.0.2.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	app := testAppWith(t, holderOf(t, []string{"203.0.113.0/24"}), netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: elsewhere}))
	if got := statusOf(t, app, "/x", "203.0.113.9"); got != fiber.StatusForbidden {
		t.Errorf("gefälschte Kopfzeile: status=%d, erwartet 403", got)
	}
}

// Der öffentliche Binary-Download bleibt für jede Adresse erreichbar.
func TestIPAllowlistKeepsAgentDownloadOpen(t *testing.T) {
	app := testApp(t, []string{"203.0.113.0/24"}, false)
	if got := statusOf(t, app, "/api/v1/agent/download/lcm-agent", ""); got != fiber.StatusOK {
		t.Errorf("Download: status=%d, erwartet 200", got)
	}
	if got := statusOf(t, app, "/x", ""); got != fiber.StatusForbidden {
		t.Errorf("andere Route: status=%d, erwartet 403", got)
	}
}

// X-Forwarded-For zählt nur von einer Gegenstelle in trusted_proxies, und ein
// Austausch der Regel wirkt ab der nächsten Anfrage: Eine fremde Gegenstelle
// kommt mit einer gefälschten Kopfzeile nicht in die Liste, ein vertrauter Proxy
// reicht den Client durch, und nach dem Ausschalten gilt wieder die Verbindung.
func TestResolveClientIPFollowsTrustSwap(t *testing.T) {
	// Der Test-Harness meldet 0.0.0.0 als Gegenstelle.
	peerList, err := netfilter.Parse([]string{"0.0.0.0/32"})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := netfilter.Parse([]string{"192.0.2.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	holder := netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{})
	app := fiber.New()
	app.Use(ResolveClientIP(holder))
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString(ClientIP(c)) })
	seen := func() string {
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	steps := []struct {
		name  string
		trust netfilter.ProxyTrust
		want  string
	}{
		{"aus", netfilter.ProxyTrust{}, "0.0.0.0"},
		{"ohne Liste", netfilter.ProxyTrust{Enabled: true}, "0.0.0.0"},
		{"anderer Proxy", netfilter.ProxyTrust{Enabled: true, Proxies: elsewhere}, "0.0.0.0"},
		{"diese Gegenstelle vertraut", netfilter.ProxyTrust{Enabled: true, Proxies: peerList}, "203.0.113.9"},
		{"Schalter aus", netfilter.ProxyTrust{Enabled: false, Proxies: peerList}, "0.0.0.0"},
	}
	for _, step := range steps {
		holder.Store(step.trust)
		if got := seen(); got != step.want {
			t.Errorf("%s: Client = %s, erwartet %s", step.name, got, step.want)
		}
	}
}
