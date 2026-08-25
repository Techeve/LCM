package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// contentSecurityPolicy für die eingebettete SPA: alles kommt von der
// eigenen Origin (Bootstrap/Svelte sind lokal gebündelt, keine CDNs).
//   - script-src 'self': KEIN 'unsafe-inline' - das gebaute index.html lädt
//     nur ein verlinktes Modul, keine Inline-Skripte (der wichtigste Schutz
//     gegen XSS-basierten Token-Diebstahl).
//   - style-src erlaubt 'unsafe-inline', weil Svelte 5 seine Styles als
//     <style>-Tags injiziert.
//   - img-src erlaubt data: für den 2FA-QR-Code (Inline-Data-URI).
//   - frame-ancestors 'none' ergänzt X-Frame-Options gegen Clickjacking.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// SecurityHeaders setzt sichere HTTP-Header für die SPA-Auslieferung.
// Für API-only-Deployments hinter einem Reverse-Proxy können diese dort
// gesetzt werden - doppelt schadet aber nicht.
func SecurityHeaders() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("Permissions-Policy", permissionsPolicy)
		c.Set("Content-Security-Policy", contentSecurityPolicy)
		// HSTS: Browser wenden den Header nur über HTTPS an (über Klartext-HTTP
		// wird er laut Spec ignoriert), daher unbedenklich immer zu senden.
		c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// Cross-Origin-Isolation: schützt gegen XS-Leaks, bei denen eine fremde
		// Seite über Seiteneffekte (Ladezeiten, Fehlerereignisse) Rückschlüsse
		// auf Antworten dieser Origin zieht.
		c.Set("Cross-Origin-Opener-Policy", "same-origin")
		c.Set("Cross-Origin-Resource-Policy", "same-origin")
		// Altlast-Schutz (Flash/Acrobat-Cross-Domain-Policies).
		c.Set("X-Permitted-Cross-Domain-Policies", "none")
		// API-Antworten enthalten Server-Inventar, Nutzerdaten und Job-Ausgaben.
		// Ohne explizite Vorgabe dürfen Browser und zwischengeschaltete Caches
		// sie heuristisch zwischenspeichern - auf gemeinsam genutzten Geräten
		// bliebe der Inhalt so nach dem Abmelden lesbar.
		if strings.HasPrefix(c.Path(), "/api/") {
			c.Set("Cache-Control", "no-store")
		}
		return c.Next()
	}
}

// permissionsPolicy schaltet Browser-Fähigkeiten ab, die diese Anwendung
// niemals braucht - greift auch dann, wenn über eine andere Lücke fremdes
// Markup eingeschleust würde.
const permissionsPolicy = "accelerometer=(), autoplay=(), camera=(), " +
	"display-capture=(), encrypted-media=(), fullscreen=(), geolocation=(), " +
	"gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), " +
	"picture-in-picture=(), serial=(), usb=(), xr-spatial-tracking=()"
