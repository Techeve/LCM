package controllers

import (
	"encoding/json"
	"errors"
	"net/mail"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"LCM/internal/api/apierr"
	"LCM/internal/inputcheck"
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
	maxTokenLen       = 256  // Aktivierungslink-Token, Abo-Schlüssel (maxNameLen * 4)
	maxListLen        = 4096 // Listenfelder: Presets, Domains, Empfänger, Paketnamen
	maxItems          = 500  // Einträge in Listen eines Rumpfs (Regeln, Varianten, Sudo-Zeilen)
	maxRecipients     = 50   // E-Mail-Empfänger je Kanal oder Admin-Verteiler
	maxAPIKeyDays     = 3650 // längste Gültigkeit eines API-Schlüssels (zehn Jahre)
	maxTTLHours       = 8760 // Gültigkeit eines Aktivierungslinks (ein Jahr; der Service kürzt weiter)
	minSessionMinutes = 5
	maxSessionMinutes = 30 * 24 * 60
	maxPercent        = 100
	maxGraceDays      = 3650 // Kulanzfrist für absichtlich getrennte Server
)

// paramUUID liest den Pfadparameter "id" und verlangt eine UUID - Jobs,
// SSH-Sitzungen und Deep-Scan-Berichte tragen nichts anderes. Alles Übrige
// ist 400, bevor eine Datenbankabfrage läuft.
func paramUUID(c fiber.Ctx, name string) (string, error) {
	raw := c.Params(name)
	if _, err := uuid.Parse(raw); err != nil || len(raw) != 36 {
		return "", apierr.InvalidID
	}
	return raw, nil
}

// checkLine verlangt eine einzeilige Eingabe ohne Steuerzeichen, höchstens
// max Zeichen (Runen, nicht Bytes). Leer ist erlaubt - ob ein Feld Pflicht
// ist, entscheidet der Service.
func checkLine(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return errTooLong.With("field", field).With("limit", max)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return errControlChars.With("field", field)
		}
	}
	return nil
}

// checkText erlaubt Zeilenumbrüche und Tabulatoren, sonst wie checkLine.
func checkText(field, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return errTooLong.With("field", field).With("limit", max)
	}
	for _, r := range v {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return errControlChars.With("field", field)
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
		return errInvalidEmail.With("field", field)
	}
	return nil
}

// checkSender erlaubt leer, sonst eine Absenderadresse, mit oder ohne
// Anzeigenamen (`lcm@example.com`, `LCM <lcm@example.com>`). Der Name darf
// höchstens maxPersonNameLen Zeichen haben, die Adresse maxEmailLen.
func checkSender(field, v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if err := checkLine(field, v, maxEmailLen+2*maxPersonNameLen+8); err != nil {
		return err
	}
	addr, err := mail.ParseAddress(v)
	if err != nil {
		return errInvalidEmail.With("field", field)
	}
	if utf8.RuneCountInString(addr.Address) > maxEmailLen {
		return errTooLong.With("field", field).With("limit", maxEmailLen)
	}
	if utf8.RuneCountInString(addr.Name) > maxPersonNameLen {
		return errTooLong.With("field", field).With("limit", maxPersonNameLen)
	}
	return nil
}

// checkHost verlangt einen Hostnamen oder eine IP-Adresse. Was wie eine
// IP-Adresse aussieht, muss auch eine sein; nichts darf in einer Shell oder URL
// eine Bedeutung haben.
func checkHost(field, v string) error {
	if v == "" {
		return nil
	}
	if utf8.RuneCountInString(v) > maxHostLen {
		return errTooLong.With("field", field).With("limit", maxHostLen)
	}
	return addressError(field, v, inputcheck.HostProblem(v))
}

// checkIP verlangt eine einzelne IP-Adresse; leer ist erlaubt.
func checkIP(field, v string) error {
	if v == "" {
		return nil
	}
	return addressError(field, v, inputcheck.IPProblem(v))
}

// checkAddressList verlangt lauter IP-Adressen oder Netze in CIDR-Schreibweise.
func checkAddressList(field string, entries []string) error {
	if err := checkCount(field, len(entries), maxItems); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := addressError(field, entry, inputcheck.AddressOrNetProblem(entry)); err != nil {
			return err
		}
	}
	return nil
}

// checkDNSPresets prüft die Zeilen „Name = IP“ der Nameserver-Vorgaben.
func checkDNSPresets(field, v string) error {
	for _, line := range strings.Split(v, "\n") {
		_, address, named := strings.Cut(line, "=")
		if !named {
			address = line
		}
		if address = strings.TrimSpace(address); address != "" || named {
			if err := addressError(field, address, inputcheck.IPProblem(address)); err != nil {
				return err
			}
		}
	}
	return nil
}

// addressError macht aus dem Grund einer Adressprüfung den passenden Fehler.
// Der Wert steht im Fehler, damit in einer langen Liste klar ist, welcher Eintrag
// gemeint ist.
func addressError(field, value, reason string) error {
	if reason == "" {
		return nil
	}
	err, known := addressErrors[reason]
	if !known {
		err = errInvalidHost
	}
	return err.With("field", field).With("value", value)
}

var addressErrors = map[string]*apierr.Error{
	inputcheck.ReasonLetter:      errIPLetter,
	inputcheck.ReasonOctet:       errIPOctet,
	inputcheck.ReasonLeadingZero: errIPLeadingZero,
	inputcheck.ReasonFormat:      errIPFormat,
	inputcheck.ReasonIPv6:        errIPv6,
	inputcheck.ReasonPrefix:      errIPPrefix,
	inputcheck.ReasonHostBits:    errIPHostBits,
}

// checkCount begrenzt die Zahl der Einträge einer Liste im Rumpf.
func checkCount(field string, n, max int) error {
	if n > max {
		return errTooMany.With("field", field).With("limit", max)
	}
	return nil
}

// checkRange verlangt eine ganze Zahl im Bereich [min, max].
func checkRange(field string, n, min, max int) error {
	if n < min || n > max {
		return errOutOfRange.With("field", field).With("min", min).With("max", max)
	}
	return nil
}

// checkOptionalRange ist checkRange für PATCH-Rümpfe: Ein fehlendes Feld ist
// keine Eingabe.
func checkOptionalRange(field string, n *int, min, max int) error {
	if n == nil {
		return nil
	}
	return checkRange(field, *n, min, max)
}

// checkEmailList prüft eine Liste von Adressen (durch Komma, Semikolon oder
// Zeilen getrennt): höchstens maxRecipients, jede eine einzelne Adresse.
func checkEmailList(field, v string) error {
	if err := checkText(field, v, maxListLen); err != nil {
		return err
	}
	entries := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' })
	if err := checkCount(field, len(entries), maxRecipients); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := checkEmail(field, entry); err != nil {
			return err
		}
	}
	return nil
}

// bindBody liest den JSON-Rumpf. Ein Wert vom falschen Typ, etwa Buchstaben in
// einem Zahlenfeld, bekommt einen Fehler mit dem Feldnamen statt eines
// allgemeinen „ungültiger Rumpf“.
func bindBody(c fiber.Ctx, out any) error {
	err := c.Bind().Body(out)
	if err == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return apierr.InvalidBody
	}
	switch typeErr.Type.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return errNotANumber.With("field", typeErr.Field)
	}
	return errWrongType.With("field", typeErr.Field)
}

// checkPort erlaubt 0 (Vorgabe) und 1-65535.
func checkPort(field string, p int) error {
	if p < 0 || p > 65535 {
		return errInvalidPort.With("field", field)
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

// derefInt liefert den Wert einer optionalen Zahl, 0 für nil.
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
