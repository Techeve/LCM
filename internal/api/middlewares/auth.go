// Package middlewares enthält Fiber-Middlewares für Authentifizierung
// (JWT + API-Key) und Autorisierung (RBAC via RequirePermission).
package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// userContextKey ist der Locals-Schlüssel für den authentifizierten User.
const userContextKey = "auth.user"

// apiKeyContextKey hält das Prefix des verwendeten API-Keys (nie den Key
// selbst) - für das Access-Log, damit Key-Anfragen von Sitzungen
// unterscheidbar sind (R2-049).
const apiKeyContextKey = "auth.apikey"

// CurrentAPIKeyPrefix liefert das Prefix des für diesen Request verwendeten
// API-Keys, oder "" bei einer Sitzungs-Authentisierung.
func CurrentAPIKeyPrefix(c fiber.Ctx) string {
	p, _ := c.Locals(apiKeyContextKey).(string)
	return p
}

// rejectedAuthContextKey hält den ABGEWIESENEN Anmeldeweg ("apikey" oder
// "session") samt Key-Prefix. Ohne diese Markierung war ein ungültiger
// API-Key im Log von einem abgelaufenen Sitzungs-Token ununterscheidbar -
// beides nur ein 401 ohne user (R2-049).
const (
	rejectedAuthContextKey   = "auth.rejected"
	rejectedPrefixContextKey = "auth.rejected.prefix"
)

// RejectedAuth liefert den abgewiesenen Anmeldeweg dieses Requests
// ("apikey"/"session", "" = keiner) und bei Keys das versuchte Prefix.
func RejectedAuth(c fiber.Ctx) (method, prefix string) {
	m, _ := c.Locals(rejectedAuthContextKey).(string)
	p, _ := c.Locals(rejectedPrefixContextKey).(string)
	return m, p
}

// safeKeyPrefix kürzt einen (potenziell angreifergewählten) Key-Wert auf die
// Prefix-Länge und behält nur log-sichere Zeichen - der volle Key darf nie
// ins Log, und Steuerzeichen dürfen keine Zeilen fälschen.
func safeKeyPrefix(key string) string {
	const maxLen = 12
	out := make([]rune, 0, maxLen)
	for _, r := range key {
		if len(out) == maxLen {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' {
			out = append(out, r)
		}
	}
	return string(out)
}

// CurrentUser liefert den authentifizierten User des Requests oder nil.
func CurrentUser(c fiber.Ctx) *domain.User {
	user, _ := c.Locals(userContextKey).(*domain.User)
	return user
}

// Authenticate prüft den Authorization-Header und hängt bei Erfolg den
// User an den Request-Kontext. Unterstützt zwei Schemata:
//
//	Authorization: Bearer <jwt>     - Browser-/User-Sessions
//	X-API-Key: <key>                - Service-zu-Service-Kommunikation
//
// Ohne gültige Credentials wird der Request NICHT abgebrochen - das
// entscheidet RequireAuth bzw. RequirePermission. So können Routen
// optional-authentifiziert sein (öffentlich mit optionalem User-Kontext).
func Authenticate(auth *services.AuthService, apiKeys *services.APIKeyService) fiber.Handler {
	return func(c fiber.Ctx) error {
		if apiKey := c.Get("X-API-Key"); apiKey != "" {
			if user, key, err := apiKeys.Validate(apiKey); err == nil {
				// MCP-Keys sind ausschließlich für den separaten MCP-Listener
				// gedacht und dürfen die REST-API/UI NICHT nutzen - sonst
				// könnte ein für read-only-Serverdaten gedachter Key hier
				// schreibend wirken. Kein User-Kontext → RequireAuth greift.
				if key.IsMCP() {
					return fiber.NewError(fiber.StatusForbidden,
						"MCP-API-Key ist nur für die MCP-Schnittstelle gültig, nicht für die REST-API")
				}
				// Scope-Prüfung: read-only-Keys dürfen keine
				// schreibenden HTTP-Methoden verwenden.
				if key.IsReadOnly() && isMutating(c.Method()) {
					return fiber.NewError(fiber.StatusForbidden,
						"API-Key hat nur Lese-Rechte (scope: read)")
				}
				c.Locals(userContextKey, user)
				c.Locals(apiKeyContextKey, key.Prefix)
			} else {
				// Abgewiesener Key: fürs Access-Log markieren (R2-049) -
				// sonst sieht der 401 aus wie eine abgelaufene Sitzung.
				c.Locals(rejectedAuthContextKey, "apikey")
				c.Locals(rejectedPrefixContextKey, safeKeyPrefix(apiKey))
			}
			return c.Next()
		}

		header := c.Get("Authorization")
		if token, ok := strings.CutPrefix(header, "Bearer "); ok {
			if user, err := auth.ValidateToken(token); err == nil {
				c.Locals(userContextKey, user)
			} else {
				c.Locals(rejectedAuthContextKey, "session")
			}
		}
		return c.Next()
	}
}

// isMutating meldet, ob eine HTTP-Methode Daten verändert.
func isMutating(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return false
	default:
		return true
	}
}

// RequireAuth bricht mit 401 ab, wenn kein authentifizierter User vorliegt.
func RequireAuth() fiber.Handler {
	return func(c fiber.Ctx) error {
		if CurrentUser(c) == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "Anmeldung erforderlich")
		}
		return c.Next()
	}
}

// RequirePermission prüft, ob der authentifizierte User die angegebene
// Permission über eine seiner Rollen besitzt. Die Rollenauflösung ist
// bereits beim Authenticate-Schritt passiert (User kommt mit vorgeladenen
// Roles.Permissions aus der DB).
//
// Verwendung: servers.Post("/join", middlewares.RequirePermission(domain.PermServersWrite), ctrl.Join)
func RequirePermission(code string) fiber.Handler {
	return func(c fiber.Ctx) error {
		user := CurrentUser(c)
		if user == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "Anmeldung erforderlich")
		}
		if !user.HasPermission(code) {
			return fiber.NewError(fiber.StatusForbidden, "fehlende Berechtigung: "+code)
		}
		return c.Next()
	}
}
