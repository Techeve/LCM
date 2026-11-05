package middlewares

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestAccessLogSchwaerztDieFahrkarte: Die Einmal-Fahrkarte eines WebSockets reist
// in der URL, steht aber nie im Protokoll - auch nicht im Debug-Level, in dem
// die Abfrage mitgeschrieben wird.
func TestAccessLogSchwaerztDieFahrkarte(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	app := fiber.New()
	app.Use(AccessLog(logger))
	app.Get("/api/v1/events", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusUnauthorized) })

	req := httptest.NewRequest("GET", "/api/v1/events?ticket=geheime-fahrkarte&cols=80", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	line := out.String()
	if strings.Contains(line, "geheime-fahrkarte") {
		t.Errorf("die Fahrkarte steht im Zugriffsprotokoll: %s", line)
	}
	if !strings.Contains(line, "cols=80") || !strings.Contains(line, "REDACTED") {
		t.Errorf("Abfrage ohne Schwärzung oder ohne die übrigen Parameter: %s", line)
	}
}

func TestRedactQuery(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"a=1":                     "a=1",
		"ticket=x":                "ticket=REDACTED",
		"term=xterm&ticket=x&b=2": "b=2&term=xterm&ticket=REDACTED",
		"ticket=a&ticket=b":       "ticket=REDACTED",
		"%zz":                     "",
	} {
		if got := redactQuery(in); got != want {
			t.Errorf("redactQuery(%q) = %q, erwartet %q", in, got, want)
		}
	}
}
