package middlewares

import (
	"log/slog"
	"net/netip"
	"strings"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/netfilter"
)

// IPAllowlist beschränkt den Zugriff auf zugelassene Client-Adressen. Nicht
// zugelassene Anfragen werden früh mit 403 abgewiesen (bevor Authentifizierung
// oder Controller-Code laufen) und als Warnung protokolliert.
//
// Die Client-IP stammt standardmäßig aus der direkten TCP-Verbindung
// (fälschungssicher). Läuft LCM hinter einem vertrauenswürdigen Reverse-Proxy,
// aktiviert trustProxyHeader die Auswertung von X-Forwarded-For.
//
// Der Aufrufer registriert diese Middleware nur, wenn die Allowlist nicht leer
// ist - ohne Konfiguration entsteht so gar kein Overhead.
func IPAllowlist(list netfilter.Allowlist, trustProxyHeader bool, logger *slog.Logger) fiber.Handler {
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
		addr, ok := clientIP(c.IP(), c.Get("X-Forwarded-For"), trustProxyHeader)
		if !ok {
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

// ClientIP liefert die für Sperren und Filter maßgebliche Client-Adresse als
// String. Bewusst dieselbe Grundlage wie die IP-Allowlist: Brute-Force-Sperren
// MÜSSEN dieselbe Adresse sehen. Nutzte der Login stattdessen die rohe
// Peer-Adresse, wäre das hinter einem Reverse-Proxy für alle Clients dieselbe
// IP - fünf Fehlversuche eines Angreifers sperrten dann die Anmeldung für die
// gesamte Installation (globaler Login-DoS).
func ClientIP(c fiber.Ctx, trustProxyHeader bool) string {
	if addr, ok := clientIP(c.IP(), c.Get("X-Forwarded-For"), trustProxyHeader); ok {
		return addr.String()
	}
	return strings.TrimSpace(c.IP())
}

// clientIP ermittelt die für den Filter maßgebliche Client-Adresse. remoteIP
// ist die direkte TCP-Peer-Adresse (Fiber c.IP()); xff der Inhalt des
// X-Forwarded-For-Headers. Bei trustProxyHeader wird die ERSTE (linkeste)
// Adresse aus dem Header bevorzugt - konventionell der ursprüngliche Client -
// und nur bei fehlendem/ungültigem Header auf die Peer-Adresse zurückgefallen.
//
// Als reine Funktion (ohne fiber.Ctx) direkt testbar.
func clientIP(remoteIP, xff string, trustProxyHeader bool) (netip.Addr, bool) {
	if trustProxyHeader && xff != "" {
		first := xff
		if i := strings.IndexByte(xff, ','); i >= 0 {
			first = xff[:i]
		}
		if addr, err := netip.ParseAddr(strings.TrimSpace(first)); err == nil {
			return addr.Unmap(), true
		}
	}
	if addr, err := netip.ParseAddr(strings.TrimSpace(remoteIP)); err == nil {
		return addr.Unmap(), true
	}
	return netip.Addr{}, false
}
