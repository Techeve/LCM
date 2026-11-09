package middlewares

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// ETag versieht erfolgreiche JSON-Antworten auf GET-Anfragen mit einem starken
// ETag (SHA-256 über den Rumpf, auf 16 Byte gekürzt) und beantwortet eine
// Anfrage mit passendem If-None-Match mit 304 ohne Rumpf: Eine unveränderte
// Liste kostet den Client so nur noch die Prüfung.
//
// Bewusst NICHT betroffen: Antworten, die nicht 200 oder nicht JSON sind (Fehler,
// Downloads, Ereignisstrom), Streams, WebSocket-Verbindungen und der Ereigniskanal.
// Cache-Control: no-store bleibt gesetzt (siehe SecurityHeaders) - die Oberfläche
// schickt If-None-Match selbst.
func ETag() fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() != fiber.MethodGet || c.Get(fiber.HeaderUpgrade) != "" || c.Path() == "/api/v1/events" {
			return c.Next()
		}
		if err := c.Next(); err != nil {
			return err
		}
		resp := c.Response()
		if resp.StatusCode() != fiber.StatusOK || resp.IsBodyStream() || !isJSONResponse(c) {
			return nil
		}
		sum := sha256.Sum256(resp.Body())
		tag := `"` + hex.EncodeToString(sum[:16]) + `"`
		c.Set(fiber.HeaderETag, tag)
		if matchesETag(c.Get(fiber.HeaderIfNoneMatch), tag) {
			resp.ResetBody()
			c.Status(fiber.StatusNotModified)
		}
		return nil
	}
}

func isJSONResponse(c fiber.Ctx) bool {
	return strings.HasPrefix(string(c.Response().Header.ContentType()), fiber.MIMEApplicationJSON)
}

// matchesETag prüft If-None-Match gegen unser Etikett: "*" oder eine Liste von
// Etiketten, schwach verglichen (das Präfix W/ zählt nicht, RFC 9110 13.1.2).
func matchesETag(header, tag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == tag {
			return true
		}
	}
	return false
}
