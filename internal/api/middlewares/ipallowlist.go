package middlewares

import (
	"log/slog"
	"net/netip"
	"strings"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/netfilter"
)

// clientIPContextKey hält die je Request einmal bestimmte Client-Adresse.
const clientIPContextKey = "client.ip"

// ResolveClientIP bestimmt EINMAL je Request die maßgebliche Client-Adresse
// (Peer-Adresse, hinter einem vertrauten Proxy die aus X-Forwarded-For) und
// hinterlegt sie im Kontext. Alle Stellen, die eine Adresse brauchen - die
// IP-Allowlist, die Anmeldesperre, das Zugriffs- und das Sicherheitsprotokoll -
// lesen sie über ClientIP und sehen damit garantiert DIESELBE Adresse.
//
// Läuft so früh wie möglich (direkt nach recover): nichts davor darf eine
// Adresse brauchen.
func ResolveClientIP(trust netfilter.ProxyTrust) fiber.Handler {
	return func(c fiber.Ctx) error {
		if addr, ok := netfilter.ClientIP(c.IP(), c.Get("X-Forwarded-For"), trust); ok {
			c.Locals(clientIPContextKey, addr.String())
		}
		return c.Next()
	}
}

// ClientIP liefert die für Sperren, Filter und Protokolle maßgebliche
// Client-Adresse. Ohne ResolveClientIP davor (Tests, schlanke Apps) die rohe
// Peer-Adresse - nie eine ungeprüfte Kopfzeile.
func ClientIP(c fiber.Ctx) string {
	if ip, _ := c.Locals(clientIPContextKey).(string); ip != "" {
		return ip
	}
	return strings.TrimSpace(c.IP())
}

// IPAllowlist beschränkt den Zugriff auf zugelassene Client-Adressen. Nicht
// zugelassene Anfragen werden früh mit 403 abgewiesen (bevor Authentifizierung
// oder Controller-Code laufen) und als Warnung protokolliert.
//
// Die Adresse kommt aus ResolveClientIP - hinter einem Reverse-Proxy also nur
// dann aus X-Forwarded-For, wenn der Proxy als vertrauenswürdig konfiguriert
// ist (trust_proxy_header + trusted_proxies).
//
// Der Aufrufer registriert diese Middleware nur, wenn die Allowlist nicht leer
// ist - ohne Konfiguration entsteht so gar kein Overhead.
func IPAllowlist(list netfilter.Allowlist, logger *slog.Logger) fiber.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c fiber.Ctx) error {
		// LCM Remote: Agent-Endpunkte sind vom IP-Filter ausgenommen - der
		// ganze Zweck des Agents sind Server OHNE feste IP (Roaming, NAT),
		// eine Allowlist würde genau sie aussperren. Die Absicherung liegt
		// dort in der Token-Authentifizierung des Brokers (/mqtt) bzw. im
		// öffentlichen Charakter des Binary-Downloads; die Allowlist schützt
		// weiterhin Admin-UI und API.
		if isAgentPath(c.Path()) {
			return c.Next()
		}
		addr, err := netip.ParseAddr(ClientIP(c))
		if err != nil {
			logger.Warn("access denied: client ip not determinable",
				"path", c.Path(), "remote", c.IP())
			return fiber.NewError(fiber.StatusForbidden, "Zugriff nicht erlaubt")
		}
		if !list.Allows(addr) {
			logger.Warn("access denied: ip not in allowlist",
				"ip", addr.String(), "method", c.Method(), "path", c.Path())
			return fiber.NewError(fiber.StatusForbidden, "Zugriff von dieser Adresse nicht erlaubt")
		}
		return c.Next()
	}
}

// isAgentPath meldet die vom IP-Filter ausgenommenen LCM-Remote-Pfade.
//
// AUSSCHLIESSLICH der öffentliche Binary-Download. Der MQTT-WebSocket (/mqtt)
// gehört bewusst NICHT hierher: er existiert nur auf dem dedizierten
// Agent-Gateway, und das registriert diese Middleware gar nicht erst. Auf dem
// UI/REST-Port wäre /mqtt keine Route, sondern liefe in den SPA-Catch-All -
// eine Ausnahme hier hätte also nur bewirkt, dass nicht zugelassene Clients
// die Admin-Oberfläche ausgeliefert bekommen.
func isAgentPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/agent/download/")
}
