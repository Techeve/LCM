package middlewares

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/inputcheck"
)

// maxSafeNumber ist die größte ganze Zahl, die JSON-Clients exakt darstellen.
// Keine Eingabe der API braucht mehr, und größere IDs enden sonst als 500.
const maxSafeNumber = 1<<53 - 1

var errSQLSuspicious = apierr.Define(422, "validation.sql_suspicious", "value looks like an SQL statement")

// sqlExemptKeys sind Felder, deren Inhalt frei sein muss und nie in eine Abfrage
// wandert: Passwörter und Schlüssel sind beliebige Zeichen, Befehle und Skripte
// laufen auf den Zielsystemen und dürfen dort auch SQL enthalten. Alles andere
// wird geprüft, auch Felder, die erst künftig dazukommen.
var sqlExemptKeys = map[string]bool{
	"password": true, "current_password": true, "new_password": true, "login_password": true,
	"default_ssh_password": true, "mail_password": true, "repo_password": true,
	"secret": true, "token": true, "ticket": true, "key": true, "access_key": true, "code": true,
	"crowdsec_console_key": true, "subscription_key": true, "private_key_pem": true, "public_key": true,
	"command": true, "commands": true, "version_command": true, "sudo_commands": true,
	"sudo_rules": true, "edit_rules": true, "path_rules": true, "next": true,
}

// numberRange ist der erlaubte Bereich eines Abfrageparameters.
type numberRange struct{ min, max int64 }

// queryNumbers sind die Zahlen in Abfragen. Unsinn wie `limit=abc` oder
// `page=-1` bekommt einen Fehler, statt still auf eine Vorgabe zu fallen.
var queryNumbers = map[string]numberRange{
	"limit":     {0, 5000},
	"offset":    {0, 1_000_000_000},
	"page":      {1, 1_000_000},
	"page_size": {1, 1000},
	"cols":      {1, 1000},
	"rows":      {1, 500},
}

// InputGuard prüft jede API-Anfrage, bevor ein Handler sie sieht: Pfad,
// Abfrageparameter und JSON-Rumpf auf SQL in Strings (Suchmetrik in
// inputcheck), Zahlen auf sinnvolle Grenzen. Die Prüfung der einzelnen Felder
// bleibt Sache der Controller; hier steht, was für alle Felder gilt.
func InputGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := checkRequest(c); err != nil {
			return err
		}
		return c.Next()
	}
}

func checkRequest(c fiber.Ctx) error {
	if inputcheck.LooksLikeSQL(c.Path()) {
		return refuseSQL(c, "path")
	}
	for key, value := range c.Queries() {
		if bound, ok := queryNumbers[key]; ok {
			if err := checkQueryNumber(key, value, bound); err != nil {
				return err
			}
		}
		if !sqlExemptKeys[key] && inputcheck.LooksLikeSQL(value) {
			return refuseSQL(c, key)
		}
	}
	if !strings.HasPrefix(c.Get(fiber.HeaderContentType), fiber.MIMEApplicationJSON) {
		return nil
	}
	return checkJSONBody(c)
}

func checkQueryNumber(key, value string, bound numberRange) error {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < bound.min || n > bound.max {
		return apierr.OutOfRange.With("field", key).With("min", bound.min).With("max", bound.max)
	}
	return nil
}

// checkJSONBody geht alle Werte des Rumpfs durch. Kaputtes JSON lässt es
// passieren: Das meldet der Handler beim Einlesen.
func checkJSONBody(c fiber.Ctx) error {
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.UseNumber()
	var body any
	if decoder.Decode(&body) != nil {
		return nil
	}
	return walkJSON(c, "", "", body)
}

// walkJSON prüft einen Wert. path ist der Feldname für die Fehlermeldung, key
// der letzte Schlüssel davor: Listen erben ihn.
func walkJSON(c fiber.Ctx, path, key string, value any) error {
	switch v := value.(type) {
	case map[string]any:
		for name, item := range v {
			if err := walkJSON(c, joinPath(path, name), name, item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := walkJSON(c, path, key, item); err != nil {
				return err
			}
		}
	case string:
		if !sqlExemptKeys[key] && inputcheck.LooksLikeSQL(v) {
			return refuseSQL(c, path)
		}
	case json.Number:
		return checkJSONNumber(path, v)
	}
	return nil
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func checkJSONNumber(path string, n json.Number) error {
	value, err := n.Float64()
	if err != nil || value > maxSafeNumber || value < -maxSafeNumber {
		return apierr.OutOfRange.With("field", path).With("min", -maxSafeNumber).With("max", maxSafeNumber)
	}
	return nil
}

// refuseSQL lehnt ab und protokolliert: Eine Sondierung soll im Log sichtbar
// sein. Der Wert selbst bleibt draußen, er könnte Daten enthalten.
func refuseSQL(c fiber.Ctx, field string) error {
	slog.Warn("sql-artige eingabe abgelehnt", "field", field, "path", c.Path(), "ip", c.IP())
	return errSQLSuspicious.With("field", field)
}
