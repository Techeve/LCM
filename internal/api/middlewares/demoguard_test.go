package middlewares

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestDemoBlocked prüft die Sperrliste der öffentlichen Demo: Zugangs-
// Manipulation, Daten-Abfluss und ausgehende Verbindungen sind gesperrt,
// der normale Demo-Betrieb (Server-Aktionen, Lesen) bleibt frei.
func TestDemoBlocked(t *testing.T) {
	tests := []struct {
		method, path string
		blocked      bool
	}{
		// Zugangs-Manipulation.
		{"POST", "/api/v1/users/", true},
		{"POST", "/api/v1/users/2/reset-password", true},
		{"DELETE", "/api/v1/users/2", true},
		{"GET", "/api/v1/users/", false},
		{"POST", "/api/v1/auth/2fa/setup", true},
		{"POST", "/api/v1/auth/password-reset", true},
		{"POST", "/api/v1/auth/login", false},
		{"POST", "/api/v1/apikeys/", true},
		{"GET", "/api/v1/apikeys/", false},

		// Daten-Abfluss.
		{"GET", "/api/v1/system/backups", false},
		{"GET", "/api/v1/system/backups/x.lcmbak/download", true},
		{"POST", "/api/v1/system/backups/trigger-now", true},
		{"POST", "/api/v1/system/self-update", true},

		// Ausgehende Verbindungen zu Besucher-Zielen.
		{"POST", "/api/v1/servers/probe", true},
		{"POST", "/api/v1/servers/join", true},
		{"POST", "/api/v1/servers/7/reconnect", true},
		{"POST", "/api/v1/servers/7/dns-test", true},
		{"POST", "/api/v1/servers/7/port-scan", true},
		{"GET", "/api/v1/servers/7/port-scan", false},
		{"POST", "/api/v1/notification-channels/1/test", true},
		{"PATCH", "/api/v1/settings/global", true},
		{"GET", "/api/v1/settings/global", false},

		// Demo-Betrieb bleibt erlaubt.
		{"POST", "/api/v1/servers/7/update", false},
		{"POST", "/api/v1/servers/7/reboot", false},
		{"POST", "/api/v1/groups/", false},

		// Nicht-API-Pfade (SPA-Auslieferung) bleiben unberührt.
		{"GET", "/index.html", false},
	}
	for _, tt := range tests {
		if got := demoBlocked(tt.method, tt.path); got != tt.blocked {
			t.Errorf("demoBlocked(%s %s) = %v, erwartet %v", tt.method, tt.path, got, tt.blocked)
		}
	}
}

// TestDemoGuardFollowsFiberRouting schickt Schreibweisen, die Fiber auf
// dieselbe Route legen könnte, durch eine App wie im Router (Sperre an der
// Gruppe /api/v1): Groß-/Kleinschreibung, abschließende und doppelte
// Schrägstriche, Punkt-Segmente. Erreicht eine davon den Handler, ist die
// Sperre umgangen. Doppelte Schrägstriche routet Fiber gar nicht (404).
func TestDemoGuardFollowsFiberRouting(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: apiErrorHandler})
	api := app.Group("/api/v1")
	api.Use(DemoGuard())
	api.Post("/system/self-update", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	api.Get("/system/backups/:name/download", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	api.Post("/servers/:id/port-scan", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	requests := []struct{ method, path string }{
		{"POST", "/api/v1/system/self-update"},
		{"POST", "/API/v1/system/self-update"},
		{"POST", "/api/V1/System/Self-Update"},
		{"POST", "/api/v1/system/self-update/"},
		{"POST", "/api/v1/system/self-update//"},
		{"POST", "//api//v1/system//self-update"},
		{"POST", "/api/v1/system/./self-update"},
		{"POST", "/api/v1/x/../system/self-update"},
		{"GET", "/api/v1/system/backups/x.lcmbak/DOWNLOAD"},
		{"GET", "/api/v1/system/backups/x.lcmbak/download/"},
		{"POST", "/api/v1/servers/7/Port-Scan/"},
	}
	for _, rq := range requests {
		resp, err := app.Test(httptest.NewRequest(rq.method, "http://lcm.test"+rq.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == fiber.StatusNoContent {
			t.Errorf("%s %s: Handler erreicht, die Demo-Sperre ist umgangen", rq.method, rq.path)
		}
	}
}
