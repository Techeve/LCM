package dbmetrics

import (
	"regexp"
	"strings"
)

// maxSQLLen begrenzt den Anweisungstext in der Liste der langsamsten.
const maxSQLLen = 300

var (
	reStringLiteral = regexp.MustCompile(`'(?:[^']|'')*'`)
	reNumber        = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	reSavepoint     = regexp.MustCompile(`\bsp0x[0-9a-f]+\b`) // GORM: "sp%p"
	rePlaceholders  = regexp.MustCompile(`\?(?:\s*,\s*\?)+`)
	reSpace         = regexp.MustCompile(`\s+`)
	reTable         = regexp.MustCompile("(?i)\\b(?:from|into|update|table)\\s+[\"`]?(\\w+)")
)

// normalizeSQL entfernt Werte aus einer Anweisung: GORM setzt Platzhalter,
// handgeschriebene Anweisungen können aber Zahlen und Zeichenketten direkt
// enthalten. Lange IN-Listen schrumpfen auf einen Platzhalter.
func normalizeSQL(sql string) string {
	s := reStringLiteral.ReplaceAllString(sql, "?")
	s = reSavepoint.ReplaceAllString(s, "sp?")
	s = reNumber.ReplaceAllString(s, "?")
	s = rePlaceholders.ReplaceAllString(s, "?…")
	s = strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
	if len(s) > maxSQLLen {
		s = s[:maxSQLLen] + "…"
	}
	return s
}

// statementKind liefert die Art einer Anweisung aus ihrem ersten Wort.
func statementKind(sql string) string {
	word, _, _ := strings.Cut(strings.TrimSpace(sql), " ")
	switch kind := strings.ToUpper(word); kind {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "PRAGMA":
		return kind
	case "WITH":
		return "SELECT"
	}
	return "OTHER"
}

// tableOf liefert die Tabelle einer handgeschriebenen Anweisung (bei GORM
// steht sie im Statement). Leer, wenn keine zu erkennen ist.
func tableOf(sql string) string {
	if m := reTable.FindStringSubmatch(sql); m != nil {
		return m[1]
	}
	return ""
}

// writes sagt, ob eine Art Zeilen schreibt.
func writes(kind string) bool {
	return kind == "INSERT" || kind == "UPDATE" || kind == "DELETE"
}
