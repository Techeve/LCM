package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// DemoGuard sperrt im öffentlichen Demo-Modus (--demo-public) die Endpunkte,
// die dort nichts verloren haben. Drei Gefahrenklassen:
//
//  1. Zugangs-Manipulation: Besucher dürfen weder Passwörter ändern noch
//     Konten anlegen/löschen - sonst sperrt der erste Scherzkeks alle
//     Nachfolgenden aus (bis zum nächsten Auto-Reset).
//  2. Daten-Abfluss: das System-Backup enthält den Master-Key.
//  3. Ausgehende Verbindungen zu Besucher-Zielen (Join/Probe/Reconnect,
//     Mail-Test): die Demo-Instanz wäre sonst ein anonymer Portscanner
//     bzw. Spam-Relay (SSRF).
//
// Alles andere - insbesondere Aktionen auf den Demo-Servern - bleibt
// erlaubt: Es läuft ohnehin nur gegen die Simulation (demo_sim.go).
func DemoGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		if demoBlocked(c.Method(), c.Path()) {
			return fiber.NewError(fiber.StatusForbidden,
				"In der öffentlichen Demo deaktiviert / disabled in this public demo")
		}
		return c.Next()
	}
}

func demoBlocked(method, path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/v1")
	if !ok {
		return false
	}
	mutating := method != fiber.MethodGet && method != fiber.MethodHead

	switch {
	// Zugangs-Manipulation.
	case mutating && strings.HasPrefix(rest, "/users"):
		return true
	case strings.HasPrefix(rest, "/auth/2fa/"):
		return true
	case strings.HasPrefix(rest, "/auth/password-reset"):
		return true
	case mutating && strings.HasPrefix(rest, "/apikeys"):
		return true

	// Daten-Abfluss: Backups (enthalten den Master-Key) komplett nur lesend,
	// der Download auch lesend nicht.
	case strings.HasPrefix(rest, "/system/backups") && (mutating || strings.HasSuffix(rest, "/download")):
		return true
	case rest == "/system/self-update" && mutating:
		return true

	// Ausgehende Verbindungen zu Besucher-Zielen (SSRF/Spam).
	case rest == "/servers/probe" || rest == "/servers/join" || rest == "/servers/dsm/probe":
		return true
	case strings.HasPrefix(rest, "/servers/") && (strings.HasSuffix(rest, "/reconnect") || strings.HasSuffix(rest, "/dns-test")):
		return true
	case mutating && strings.HasPrefix(rest, "/notification-channels"):
		return true
	case mutating && strings.HasPrefix(rest, "/settings"):
		return true
	case mutating && strings.HasPrefix(rest, "/subscription"):
		return true
	}
	return false
}
