package controllers

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Eingabegrenzen der API. Der Server ist die Autorität: Was hier nicht
// durchkommt, erreicht keinen Service - unabhängig davon, was die Oberfläche
// zulässt. Die Werte sind großzügig für echte Eingaben und eng für Missbrauch.
const (
	maxNameLen        = 64    // Namen von Servern, Gruppen, Regeln, Schlüsseln …
	maxDescriptionLen = 1000  // Beschreibungen
	maxHostLen        = 253   // Hostname/IP (RFC 1035)
	maxEmailLen       = 254   // RFC 5321
	maxPersonNameLen  = 64    // Vor-/Nachname
	maxLoginUserLen   = 64    // Anmeldename auf Zielsystemen (RouterOS frei)
	maxPasswordLen    = 1024  // Passwörter an Zielsysteme (nie gehasht, nur weitergereicht)
	maxScriptLen      = 32768 // Skript-Regeln und Custom-Aktionen
	maxQueryLen       = 100   // Suchfelder
	maxCronLen        = 100
	maxCommandLen     = 512  // eine sudoers-Kommandozeile
	maxPathLen        = 4096 // PATH_MAX
)

// paramUUID liest den Pfadparameter "id" und verlangt eine UUID - Jobs,
// SSH-Sitzungen und Deep-Scan-Berichte tragen nichts anderes. Alles Übrige
// ist 400, bevor eine Datenbankabfrage läuft.
func paramUUID(c fiber.Ctx, name string) (string, error) {
	raw := c.Params(name)
	if _, err := uuid.Parse(raw); err != nil || len(raw) != 36 {
		return "", fiber.NewError(fiber.StatusBadRequest, "ungültige ID (UUID erwartet)")
	}
	return raw, nil
}

// invalid ist die einheitliche Antwort auf eine abgewiesene Eingabe.
func invalid(field, why string) error {
	return fiber.NewError(fiber.StatusUnprocessableEntity, field+": "+why)
}

// checkLine verlangt eine einzeilige Eingabe ohne Steuerzeichen, höchstens
// max Zeichen (Runen, nicht Bytes). Leer ist erlaubt - ob ein Feld Pflicht
// ist, entscheidet der Service.
func checkLine(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return invalid(field, fmt.Sprintf("länger als %d Zeichen", max))
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return invalid(field, "enthält Steuerzeichen")
		}
	}
	return nil
}

// checkText erlaubt Zeilenumbrüche und Tabulatoren, sonst wie checkLine.
func checkText(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return invalid(field, fmt.Sprintf("länger als %d Zeichen", max))
	}
	for _, r := range v {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return invalid(field, "enthält Steuerzeichen")
		}
	}
	return nil
}

// checkLines prüft mehrere einzeilige Felder auf einmal: Name, Wert, Grenze.
func checkLines(fields ...lineField) error {
	for _, f := range fields {
		if err := checkLine(f.name, f.value, f.max); err != nil {
			return err
		}
	}
	return nil
}

type lineField struct {
	name  string
	value string
	max   int
}

// checkEmail erlaubt leer, sonst eine einzelne Adresse ohne Anzeigename.
func checkEmail(field, v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if err := checkLine(field, v, maxEmailLen); err != nil {
		return err
	}
	addr, err := mail.ParseAddress(v)
	if err != nil || addr.Address != v {
		return invalid(field, "keine gültige E-Mail-Adresse")
	}
	return nil
}

// checkHost verlangt einen Hostnamen oder eine IP-Adresse: nur die Zeichen,
// die darin vorkommen dürfen. Kein Leerzeichen, kein Schrägstrich, nichts,
// was in einer Shell oder URL eine Bedeutung hätte.
func checkHost(field, v string) error {
	if v == "" {
		return nil
	}
	if utf8.RuneCountInString(v) > maxHostLen {
		return invalid(field, fmt.Sprintf("länger als %d Zeichen", maxHostLen))
	}
	for _, r := range v {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == ':' || r == '[' || r == ']' || r == '_' || r == '%'
		if !ok {
			return invalid(field, "unzulässiges Zeichen in Host/IP")
		}
	}
	return nil
}

// checkPort erlaubt 0 (Vorgabe) und 1-65535.
func checkPort(field string, p int) error {
	if p < 0 || p > 65535 {
		return invalid(field, "außerhalb von 1-65535")
	}
	return nil
}

// checkOptionalLines ist checkLines für PATCH-Rümpfe mit Zeigern - fehlende
// Felder (nil → "") sind keine Eingabe.
func checkOptionalLines(fields ...lineField) error { return checkLines(fields...) }

// deref liefert den Wert eines optionalen Strings, "" für nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
