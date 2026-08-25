package middlewares

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/netfilter"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		xff        string
		trustProxy bool
		want       string
		wantOK     bool
	}{
		{name: "peer ohne proxy", remote: "203.0.113.9", trustProxy: false, want: "203.0.113.9", wantOK: true},
		{name: "xff ignoriert ohne trust", remote: "203.0.113.9", xff: "10.0.0.1", trustProxy: false, want: "203.0.113.9", wantOK: true},
		{name: "xff genutzt mit trust", remote: "10.0.0.1", xff: "203.0.113.9", trustProxy: true, want: "203.0.113.9", wantOK: true},
		{name: "xff erste adresse (client)", remote: "10.0.0.1", xff: "203.0.113.9, 10.0.0.2, 10.0.0.3", trustProxy: true, want: "203.0.113.9", wantOK: true},
		{name: "trust aber leerer xff faellt auf peer", remote: "198.51.100.7", xff: "", trustProxy: true, want: "198.51.100.7", wantOK: true},
		{name: "trust aber ungueltiger xff faellt auf peer", remote: "198.51.100.7", xff: "kaputt", trustProxy: true, want: "198.51.100.7", wantOK: true},
		{name: "v4-in-v6 wird entpackt", remote: "::ffff:203.0.113.9", trustProxy: false, want: "203.0.113.9", wantOK: true},
		{name: "ungueltige peer-ip", remote: "nicht-ip", trustProxy: false, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, ok := clientIP(tt.remote, tt.xff, tt.trustProxy)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v, erwartet %v", ok, tt.wantOK)
			}
			if ok && addr.String() != tt.want {
				t.Errorf("ip=%s, erwartet %s", addr.String(), tt.want)
			}
		})
	}
}

// testApp baut eine minimale Fiber-App mit der Allowlist-Middleware.
func testApp(t *testing.T, entries []string, trustProxy bool) *fiber.App {
	t.Helper()
	list, err := netfilter.Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(IPAllowlist(list, trustProxy, slog.New(slog.NewTextHandler(io.Discard, nil))))
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
