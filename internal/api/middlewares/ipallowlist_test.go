package middlewares

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/netfilter"
)

// testApp baut eine minimale Fiber-App mit der Allowlist-Middleware.
func testApp(t *testing.T, entries []string, trustProxy bool) *fiber.App {
	t.Helper()
	list, err := netfilter.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(ResolveClientIP(netfilter.ProxyTrust{Enabled: trustProxy}))
	app.Use(IPAllowlist(list, slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString("ok") })
	return app
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
		{xff: "203.0.113.9, 10.0.0.1", status: fiber.StatusOK},
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
}
