package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
)

// MaintenanceGuard sperrt jede schreibende Anfrage, solange LCM sich selbst
// aktualisiert. Die Sperre im Job-Dienst hält neue Jobs schon auf; diese hier
// gilt darüber hinaus für alles, was ein zweiter Tab oder ein anderer
// Benutzer anstoßen könnte - auch Änderungen ohne Job, die der Neustart
// mittendrin abschnitte.
//
// Lesen bleibt frei, ebenso das, was zum Zuschauen gehört: Anmelden und
// Abmelden, der Ereigniskanal, das Selbst-Update selbst (Abbrechen, solange es
// noch wartet) und die eigene Darstellung.
func MaintenanceGuard(active func() bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		if maintenanceBlocked(c.Method(), c.Path()) && active() {
			return apierr.Maintenance
		}
		return c.Next()
	}
}

func maintenanceBlocked(method, path string) bool {
	if method == fiber.MethodGet || method == fiber.MethodHead || method == fiber.MethodOptions {
		return false
	}
	// Fiber vergleicht Routen ohne Rücksicht auf Groß- und Kleinschreibung,
	// c.Path() behält sie aber: /API/V1/... käme sonst an der Sperre vorbei.
	rest, ok := strings.CutPrefix(strings.ToLower(path), "/api/v1")
	if !ok {
		return false
	}
	switch {
	case strings.HasPrefix(rest, "/auth/"):
		return false // Anmelden, Abmelden, eigene Einstellungen der Oberfläche
	case rest == "/events/ticket":
		return false
	case rest == "/system/self-update":
		return false
	case rest == "/system/release-notes/seen":
		return false
	}
	return true
}
