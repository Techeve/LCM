package middlewares

import (
	"fmt"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
)

// remediationAllowedExact sind die Endpunkte, die ein Nutzer im
// Remediation-Zustand (Passwort ändern / 2FA einrichten) noch erreichen darf.
var remediationAllowedExact = map[string]bool{
	"/api/v1/auth/logout":     true,
	"/api/v1/auth/me":         true,
	"/api/v1/auth/2fa/setup":  true,
	"/api/v1/auth/2fa/enable": true,
	"/api/v1/system/info":     true,
	"/api/v1/health":          true,
}

// AccountRemediation erzwingt serverseitig, dass ein Nutzer, dessen Konto eine
// Aktion NACHHOLEN muss, nur noch die dafür nötigen Endpunkte erreicht - alles
// andere: 403. Zwei Fälle:
//   - MustChangePassword: das initiale/zurückgesetzte Passwort muss geändert
//     werden, bevor irgendetwas anderes geht.
//   - 2FA-Pflicht laut Rollen-Policy, aber nicht eingerichtet: erst einrichten.
//
// Ohne diese Middleware wären beide Policies nur Frontend-Hinweise (ein
// direkter API-Client mit gültigem Token könnte sie umgehen). Läuft nach
// Authenticate; ohne authentifizierten User ein No-op.
func AccountRemediation(settings *services.SettingsService) fiber.Handler {
	return func(c fiber.Ctx) error {
		user := CurrentUser(c)
		if user == nil {
			return c.Next()
		}

		needPassword := user.MustChangePassword
		need2FA := false
		if !user.TOTPEnabled {
			// settings.Get() nur nötig, wenn 2FA überhaupt fehlen könnte.
			if s, err := settings.Get(); err == nil && s.Requires2FA(user.RoleNames()) {
				need2FA = true
			}
		}
		if !needPassword && !need2FA {
			return c.Next()
		}

		// Erlaubte Endpunkte während der Remediation (Passwort-Reset, 2FA-Setup,
		// me/logout) durchlassen - der Rest wird geblockt. Der Passwort-Reset
		// ist dabei strikt auf das EIGENE Konto begrenzt: ein Admin im
		// Remediation-Zustand darf nicht fremde Passwörter zurücksetzen,
		// bevor er sein eigenes geändert hat.
		path := c.Path()
		ownReset := fmt.Sprintf("/api/v1/users/%d/reset-password", user.ID)
		if remediationAllowedExact[path] || path == ownReset {
			return c.Next()
		}
		if needPassword {
			return fiber.NewError(fiber.StatusForbidden,
				"Passwort muss zuerst geändert werden - bis dahin ist das Konto gesperrt")
		}
		return fiber.NewError(fiber.StatusForbidden,
			"Zwei-Faktor-Authentifizierung ist für diese Rolle Pflicht und muss zuerst eingerichtet werden")
	}
}
