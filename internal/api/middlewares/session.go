package middlewares

import (
	"net/netip"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
)

// Namen von Cookies und Header der Browser-Sitzung. Die Cookie-Namen tragen das
// Präfix __Host-: Der Browser nimmt sie nur an, wenn sie Secure sind, Path=/
// haben und kein Domain-Attribut tragen - sie lassen sich weder von einer
// Subdomain noch über reines HTTP setzen oder überschreiben.
const (
	// SessionCookieName trägt den Sitzungswert (HttpOnly - JavaScript sieht ihn nie).
	SessionCookieName = "__Host-lcm_session"
	// CSRFCookieName trägt das CSRF-Token (lesbar, damit die Oberfläche es
	// als Header zurückschicken kann).
	CSRFCookieName = "__Host-lcm_csrf"
	// CSRFHeader ist der Header, in dem ändernde Cookie-Anfragen das Token mitschicken.
	CSRFHeader = "X-CSRF-Token"
	// SessionHeader trägt den Sitzungswert der Arbeitsspeicher-Variante.
	SessionHeader = "X-LCM-Session"
)

const (
	sessionIDContextKey        = "auth.session"
	sessionTransportContextKey = "auth.session.transport"
	secureContextKey           = "request.secure"
)

// ErrCSRFInvalid: Eine ändernde Anfrage mit Cookie-Sitzung ohne passendes Token.
var ErrCSRFInvalid = apierr.Define(fiber.StatusForbidden, "auth.csrf_invalid", "CSRF token missing or invalid")

// CurrentSessionID liefert die (öffentliche) ID der Browser-Sitzung dieses
// Requests, "" bei API-Key.
func CurrentSessionID(c fiber.Ctx) string {
	id, _ := c.Locals(sessionIDContextKey).(string)
	return id
}

// CurrentSessionTransport liefert den Weg, auf dem die Browser-Sitzung dieses
// Requests ankam, "" ohne Sitzung. Nur beim Cookie schickt der Browser die
// Anmeldung von selbst mit und braucht CSRF-Schutz.
func CurrentSessionTransport(c fiber.Ctx) services.Transport {
	transport, _ := c.Locals(sessionTransportContextKey).(services.Transport)
	return transport
}

// RequestIsHTTPS meldet, ob diese Anfrage über HTTPS kam (siehe requestIsHTTPS).
// Setzt ResolveClientIP voraus.
func RequestIsHTTPS(c fiber.Ctx) bool {
	https, _ := c.Locals(secureContextKey).(bool)
	return https
}

// requestIsHTTPS meldet, ob die Anfrage des Browsers über HTTPS kam: direkte
// TLS-Verbindung, oder X-Forwarded-Proto: https von einem vertrauten Proxy
// (dieselbe Vertrauensregel wie für X-Forwarded-For). Von jedem anderen Peer
// ist die Kopfzeile frei erfunden und zählt nicht.
func requestIsHTTPS(c fiber.Ctx, trust netfilter.ProxyTrust) bool {
	if c.RequestCtx().IsTLS() {
		return true
	}
	peer, err := netip.ParseAddr(strings.TrimSpace(c.IP()))
	if err != nil || !trust.Trusts(peer.Unmap()) {
		return false
	}
	proto, _, _ := strings.Cut(c.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// SetSessionCookies setzt Sitzungs- und CSRF-Cookie. Immer Secure: Die Cookie-
// Variante gibt es nur über HTTPS (der Aufrufer prüft RequestIsHTTPS), und das
// Präfix __Host- verlangt es. Lebensdauer = absolute Obergrenze; das Leerlauf-
// Ende prüft der Server.
func SetSessionCookies(c fiber.Ctx, token, csrf string, lifetime time.Duration) {
	maxAge := int(lifetime / time.Second)
	c.Cookie(&fiber.Cookie{Name: SessionCookieName, Value: token, Path: "/", MaxAge: maxAge,
		HTTPOnly: true, SameSite: fiber.CookieSameSiteStrictMode, Secure: true})
	c.Cookie(&fiber.Cookie{Name: CSRFCookieName, Value: csrf, Path: "/", MaxAge: maxAge,
		HTTPOnly: false, SameSite: fiber.CookieSameSiteStrictMode, Secure: true})
}

// ClearSessionCookies weist den Browser an, beide Cookies zu löschen.
func ClearSessionCookies(c fiber.Ctx) {
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		c.Cookie(&fiber.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1,
			HTTPOnly: name == SessionCookieName, SameSite: fiber.CookieSameSiteStrictMode, Secure: true})
	}
}

// HasSessionCookies meldet, ob der Browser Reste einer Cookie-Sitzung mitschickt.
func HasSessionCookies(c fiber.Ctx) bool {
	return c.Cookies(SessionCookieName) != "" || c.Cookies(CSRFCookieName) != ""
}

// csrfExemptPaths sind die Anmelde-Schritte: Sie laufen vor der Sitzung. Ein
// Rest-Cookie einer früheren Sitzung darf die Anmeldung nicht blockieren.
var csrfExemptPaths = map[string]bool{
	"/api/v1/auth/login":     true,
	"/api/v1/auth/login/2fa": true,
}

// CSRFProtect verlangt bei jeder ändernden Anfrage, die per Cookie angemeldet
// ist, den Header X-CSRF-Token mit dem zur Sitzung gehörenden Wert (Double-
// Submit). Kopfzeilen-Sitzung (X-LCM-Session) und API-Key sind ausgenommen:
// Browser hängen sie nicht von selbst an, sie sind nicht fälschbar. Läuft nach
// Authenticate.
func CSRFProtect(auth *services.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
		sessionID := CurrentSessionID(c)
		if sessionID == "" || CurrentSessionTransport(c) != services.TransportCookie || !isMutating(c.Method()) || csrfExemptPaths[c.Path()] {
			return c.Next()
		}
		if !auth.CSRFValid(sessionID, c.Get(CSRFHeader)) {
			return ErrCSRFInvalid
		}
		return c.Next()
	}
}
