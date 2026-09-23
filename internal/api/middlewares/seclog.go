package middlewares

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// SecurityEvent schreibt ein sicherheitsrelevantes Ereignis als EINE
// Protokollzeile mit festem Text „security" - Anmeldungen, Sperren,
// Passwort- und Zweitfaktor-Änderungen, API-Schlüssel, Konsolen-Sitzungen.
//
// Das Audit-Log in der Datenbank hält dieselben Vorgänge, aber es liegt auf
// demselben Knoten wie alles andere: Wer den Host übernimmt, kann es ändern.
// Das Journal lässt sich dagegen weiterleiten (journald → Syslog/SIEM) und ist
// für fail2ban greifbar: `event=login.failed ip=<HOST>`. Erfolge (Suffix
// „.ok") sind INFO, alles andere WARN - `journalctl -p warning | grep
// security` zeigt damit nur, was Aufmerksamkeit verdient.
//
// Die Adresse ist dieselbe wie für Allowlist und Anmeldesperre (ClientIP);
// Nutzernamen sind angreifergewählt und werden auf eine Zeile gekürzt.
func SecurityEvent(c fiber.Ctx, event string, attrs ...any) {
	level := slog.LevelWarn
	if strings.HasSuffix(event, ".ok") {
		level = slog.LevelInfo
	}
	base := []any{"event", event, "ip", ClientIP(c), "path", strings.Clone(c.Path())}
	for i := 1; i < len(attrs); i += 2 {
		if s, ok := attrs[i].(string); ok {
			attrs[i] = logSafe(s)
		}
	}
	slog.Log(context.Background(), level, "security", append(base, attrs...)...)
}

// logSafe kürzt einen fremdbestimmten Wert und entfernt Zeilenumbrüche, damit
// eine Eingabe keine zweite Protokollzeile fälschen kann.
func logSafe(s string) string {
	const maxLen = 64
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return strings.Clone(s)
}
