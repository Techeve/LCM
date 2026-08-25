package middlewares

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
)

// AccessLog protokolliert jeden Request über den Log-Service (slog):
// Methode, Pfad, Status, Dauer, Client-IP und - falls authentifiziert -
// den Usernamen. Im Debug-Level kommen User-Agent und Query dazu.
//
// Die Middleware steht VOR Authenticate in der Kette; da der User erst
// nach c.Next() feststeht, wird er nach Abschluss des Requests gelesen.
func AccessLog(logger *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()
		if err != nil {
			var fe *fiber.Error
			if errors.As(err, &fe) {
				status = fe.Code
			} else {
				status = fiber.StatusInternalServerError
			}
		}

		attrs := []any{
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.IP(),
		}
		if user := CurrentUser(c); user != nil {
			attrs = append(attrs, "user", user.Username)
		}
		// R2-049: Key-Anfragen kenntlich machen - sonst ununterscheidbar von
		// interaktiven Sitzungen. Nur das Prefix, nie der Key. Auch die
		// ABWEISUNG wird benannt: ein ungültiger Key sah vorher genauso aus
		// wie ein abgelaufenes Sitzungs-Token (beides 401 ohne user).
		if prefix := CurrentAPIKeyPrefix(c); prefix != "" {
			attrs = append(attrs, "auth", "apikey", "api_key_prefix", prefix)
		} else if CurrentUser(c) != nil {
			attrs = append(attrs, "auth", "session")
		} else if method, prefix := RejectedAuth(c); method != "" {
			attrs = append(attrs, "auth_rejected", method)
			if prefix != "" {
				attrs = append(attrs, "api_key_prefix", prefix)
			}
		}
		if logger.Enabled(c.Context(), slog.LevelDebug) {
			attrs = append(attrs,
				"query", string(c.RequestCtx().QueryArgs().QueryString()),
				"user_agent", c.Get("User-Agent"),
			)
		}

		logger.LogAttrs(c.Context(), levelFor(status), "request", toAttrs(attrs)...)
		return err
	}
}

func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func toAttrs(kv []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		attrs = append(attrs, slog.Any(kv[i].(string), kv[i+1]))
	}
	return attrs
}
