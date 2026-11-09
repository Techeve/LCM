// Package middlewares enthält Fiber-Middlewares für Authentifizierung
// (Browser-Sitzung + API-Key) und Autorisierung (RBAC via RequirePermission).
package middlewares

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
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

// Authenticate prüft die Anmeldung des Requests und hängt bei Erfolg den
// User an den Request-Kontext. Unterstützt drei Wege, in dieser Rangfolge:
//
//	X-API-Key: <key>                - Skripte und Service-zu-Service-Kommunikation
//	X-LCM-Session: <wert>           - Browser-Sitzung, Arbeitsspeicher-Variante
//	Cookie __Host-lcm_session       - Browser-Sitzung, Cookie-Variante (ändernde Anfragen: CSRFProtect)
//
// Eine Sitzung gilt nur über den Weg, für den sie ausgestellt wurde. Ein
// Authorization: Bearer an der REST-API ist keine Anmeldung.
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
					return errMCPKeyOnREST
				}
				// Scope-Prüfung: read-only-Keys dürfen keine
				// schreibenden HTTP-Methoden verwenden.
				if key.IsReadOnly() && isMutating(c.Method()) {
					return errKeyReadOnly
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

		if token := c.Get(SessionHeader); token != "" {
			authenticateSession(c, auth, token, services.TransportHeader)
			return c.Next()
		}
		if token := c.Cookies(SessionCookieName); token != "" {
			authenticateSession(c, auth, token, services.TransportCookie)
		}
		return c.Next()
	}
}

// authenticateSession prüft den Sitzungswert der Browser-Anmeldung. Merkt sich
// die Sitzungs-ID für CSRFProtect und CurrentSessionID.
func authenticateSession(c fiber.Ctx, auth *services.AuthService, token string, transport services.Transport) {
	user, sessionID, err := auth.ValidateSession(token, transport)
	if err != nil {
		c.Locals(rejectedAuthContextKey, "session")
		return
	}
	c.Locals(userContextKey, user)
	c.Locals(sessionIDContextKey, sessionID)
	c.Locals(sessionTransportContextKey, transport)
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
			return apierr.Unauthorized
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
			return apierr.Unauthorized
		}
		if !user.HasPermission(code) {
			return apierr.Forbidden.With("permission", code)
		}
		return c.Next()
	}
}
