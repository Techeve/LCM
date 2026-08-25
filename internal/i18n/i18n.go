// Package i18n wählt die Sprache der Konsolen- und Installationsausgaben und
// stellt sicher, dass sie überall lesbar ankommen.
//
// Zwei Regeln:
//
//  1. ENGLISCH ist der Standard. Deutsch nur, wenn das System tatsächlich auf
//     Deutsch eingestellt ist (LC_ALL/LC_MESSAGES/LANG). So versteht jeder
//     Administrator die Ausgabe, unabhängig vom Herkunftsland des Systems.
//
//  2. AUSGABE IST IMMER ASCII. Konsolen-Ausgaben landen in Terminals, Logs,
//     Journal-Exporten und CI-Protokollen, deren Zeichenkodierung wir nicht
//     kontrollieren - Umlaute werden dort regelmäßig zu unleserlichem Zeichen-
//     salat („Weboberflächeâ€œ). Deshalb übersetzt T deutsche Texte automatisch
//     nach ue/ae/oe/ss.
//
// Der zweite Punkt ist bewusst in T eingebaut statt an jeder Aufrufstelle:
// Im Quelltext dürfen die deutschen Texte normal geschrieben werden (lesbar,
// pflegeleicht) - die Umwandlung passiert zuverlässig an einer Stelle.
//
// Gilt NICHT für die Weboberfläche: Die liefert UTF-8 über HTTP aus und hat
// ihre eigene Übersetzung (frontend/src/locales). Und nicht für die
// Journal-Logmeldungen (slog) - die sind einheitlich englisch, damit der
// Support unabhängig vom Kundensystem dieselbe Sprache sieht.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang liefert "de" oder "en". Ausgewertet wird bei jedem Aufruf, damit Tests
// die Umgebung setzen können; die Auswertung ist billig (drei Env-Lookups).
func Lang() string {
	// POSIX-Reihenfolge: LC_ALL schlägt LC_MESSAGES schlägt LANG.
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return langOf(v)
		}
	}
	return "en"
}

// langOf bildet einen Locale-Namen auf unsere zwei Sprachen ab.
// "C" und "POSIX" sind ausdrücklich Englisch.
func langOf(locale string) string {
	l := strings.ToLower(locale)
	if l == "de" || strings.HasPrefix(l, "de_") || strings.HasPrefix(l, "de-") {
		return "de"
	}
	return "en"
}

// T wählt zwischen englischer und deutscher Fassung und liefert reines ASCII.
func T(en, de string) string {
	if Lang() == "de" {
		return ASCII(de)
	}
	return ASCII(en)
}

// Tf ist T mit anschließender Formatierung (fmt.Sprintf-Semantik).
// Die eingesetzten Werte werden NICHT umgewandelt - sie sind Daten
// (Pfade, Namen, Fehlertexte), keine Übersetzungen.
func Tf(en, de string, args ...any) string {
	return fmt.Sprintf(T(en, de), args...)
}

// replacer wandelt die Zeichen um, die außerhalb von UTF-8-Umgebungen
// zerbrechen. Reihenfolge egal - strings.NewReplacer arbeitet auf dem
// längsten Treffer.
var replacer = strings.NewReplacer(
	// Deutsche Umlaute und Eszett.
	"ä", "ae", "ö", "oe", "ü", "ue",
	"Ä", "Ae", "Ö", "Oe", "Ü", "Ue",
	"ß", "ss",
	// Typografie, die in der Codebasis vorkommt.
	"-", "-", "-", "-", "…", "...",
	"„", "\"", "“", "\"", "”", "\"",
	"‚", "'", "‘", "'", "’", "'",
	"×", "x", "→", "->", "·", "-",
	" ", " ", // geschütztes Leerzeichen
)

// ASCII wandelt einen Text in darstellbares ASCII um. Zeichen ohne bekannte
// Entsprechung werden entfernt, statt als Fragezeichen oder Zeichensalat
// stehen zu bleiben.
func ASCII(s string) string {
	s = replacer.Replace(s)
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}
