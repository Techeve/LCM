package middlewares

import (
	"io"

	"github.com/gofiber/fiber/v3"
)

// BodyBudget begrenzt Request-Rümpfe je Route, BEVOR sie gelesen werden.
//
// Fiber kennt nur EIN Body-Limit für die ganze App, und das muss groß genug
// für den größten legitimen Upload sein (das Backup-Archiv beim Restore,
// 64 MiB). Ohne diese Middleware gälte dieselbe Grenze auch für die
// Anmeldung - ein unangemeldeter Client könnte den Dienst mit 64-MiB-Rümpfen
// auf /auth/login den Arbeitsspeicher füllen. Zusammen mit
// fiber.Config.StreamRequestBody greift die Prüfung hier auf die Kopfzeilen,
// bevor der Rumpf im Speicher liegt.
//
// exempt nennt die Pfade, die das große Limit wirklich brauchen. Rümpfe ohne
// Längenangabe (Transfer-Encoding: chunked) werden auf allen anderen Pfaden
// abgewiesen: Browser und übliche Clients schicken die Länge immer mit, und
// ohne sie ließe sich die Grenze nicht vorab prüfen.
func BodyBudget(limit int, exempt func(path string) bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		if exempt(c.Path()) {
			err := c.Next()
			if req := c.Request(); req.IsBodyStream() && req.Header.ContentLength() > 0 && !c.RequestCtx().Hijacked() {
				// Ein ungelesener Upload ist zu groß zum Leerlesen - dann
				// lieber die Verbindung beenden als Müll als nächste Anfrage.
				c.RequestCtx().SetConnectionClose()
			}
			return err
		}
		// fasthttp meldet -1 für chunked; -2 steht für „keine Angabe" und
		// bedeutet bei Anfragen einen leeren Rumpf (HTTP/1.1 §6).
		length := c.Request().Header.ContentLength()
		if length == -1 {
			return reject(c, fiber.StatusLengthRequired,
				"Content-Length erforderlich - Rümpfe ohne Längenangabe sind nur beim Backup-Upload zulässig")
		}
		if length > limit {
			return reject(c, fiber.StatusRequestEntityTooLarge, "Request-Body zu groß")
		}
		err := c.Next()
		drainUnread(c, limit)
		return err
	}
}

// drainUnread liest einen Rumpf zu Ende, den der Handler nicht angefasst hat
// (etwa weil RequirePermission vorher mit 403 abgebrochen hat). Mit Streaming
// bleibt er sonst auf der Leitung liegen, und fasthttp läse ihn als nächste
// Anfrage derselben Keep-Alive-Verbindung - die dann mit 400 scheiterte.
// Gelesen wird höchstens das Budget: mehr hat die Kopfzeile nicht zugelassen.
func drainUnread(c fiber.Ctx, limit int) {
	req := c.Request()
	if !req.IsBodyStream() || req.Header.ContentLength() <= 0 || c.RequestCtx().Hijacked() {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(req.BodyStream(), int64(limit)))
	_ = req.CloseBodyStream()
}

// reject beantwortet einen abgewiesenen Rumpf und schließt die Verbindung
// danach: Der ungelesene Rumpf liegt noch auf der Leitung, und fasthttp würde
// ihn sonst als nächste Anfrage zu lesen versuchen.
func reject(c fiber.Ctx, status int, message string) error {
	c.RequestCtx().SetConnectionClose()
	return fiber.NewError(status, message)
}
