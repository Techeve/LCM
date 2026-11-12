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
	list, err := netfilter.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	// Der Test-Harness meldet 0.0.0.0 als Gegenstelle - sie spielt den Proxy.
	proxies, err := netfilter.Parse([]string{"0.0.0.0/32"})
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{ErrorHandler: apiErrorHandler})
	app.Use(ResolveClientIP(netfilter.ProxyTrust{Enabled: trustProxy, Proxies: proxies}))
	app.Use(IPAllowlist(list, slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString("ok") })
	return app
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
	app.Use(ResolveClientIP(netfilter.ProxyTrust{Enabled: true, Proxies: proxies}))
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
