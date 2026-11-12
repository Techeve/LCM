package inputcheck

import (
	"net/url"
	"regexp"
	"strings"
)

// Die Suchmetrik für SQL in Strings. Die Datenbankschicht bindet Werte als
// Parameter, das ist der eigentliche Schutz. Diese Prüfung ist eine zweite
// Linie: Sie weist Eingaben ab, die nach einem Angriff aussehen, bevor sie
// irgendwo landen, und macht Sondierungen sichtbar.
//
// Einzelne Wörter reichen nicht: „select“ oder „O'Brien“ sind normaler Text.
// Deshalb zählen Treffer Punkte, und erst ab SQLThreshold gilt ein Text als SQL.
// Eindeutige Muster erreichen die Schwelle allein, schwächere nur zusammen.
const (
	SQLThreshold = 10

	weightStrong = 10
	weightMedium = 5
	weightWeak   = 3

	// maxScanLen begrenzt die Arbeit je Wert; längere Texte gibt es nur in
	// Feldern, die ohnehin nicht geprüft werden.
	maxScanLen = 64 * 1024
)

type sqlSignal struct {
	name    string
	weight  int
	pattern *regexp.Regexp
}

func signal(name string, weight int, pattern string) sqlSignal {
	return sqlSignal{name, weight, regexp.MustCompile(pattern)}
}

var sqlSignals = []sqlSignal{
	signal("union_select", weightStrong, `\bunion\s+(all\s+|distinct\s+)?select\b`),
	signal("stacked_statement", weightStrong, `;\s*(select|insert|update|delete|drop|alter|create|truncate|replace|attach|detach|pragma|exec|execute|merge|grant|revoke|vacuum)\b`),
	signal("quote_breakout", weightStrong, `['"`+"`"+`)]\s*(or|and|\|\||&&)\s+\S+\s*(=|<|>|like\b|in\b|is\b)`),
	signal("tautology", weightStrong, `\b(or|and)\s+\d+\s*(=|<|>)\s*\d+`),
	signal("string_tautology", weightStrong, `\b(or|and)\s+'[^']*'\s*=\s*'`),
	signal("quote_comment", weightStrong, `['"]\s*(--|#|/\*)`),
	signal("schema_table", weightStrong, `\b(information_schema|sqlite_master|sqlite_schema|sqlite_temp_master|pg_catalog|pg_shadow|sys\.tables|mysql\.user)\b`),
	signal("time_function", weightStrong, `\b(pg_sleep|load_extension|load_file|benchmark|randomblob|extractvalue|updatexml|dbms_pipe|utl_http)\s*\(`),
	signal("sleep_call", weightStrong, `\bsleep\s*\(\s*\d`),
	signal("server_procedure", weightStrong, `\bwaitfor\s+delay\b|\bxp_cmdshell\b|\bsp_executesql\b|\binto\s+(out|dump)file\b`),

	signal("select_from", weightMedium, `\bselect\b.{1,200}\bfrom\b`),
	signal("insert_into", weightMedium, `\binsert\s+into\b`),
	signal("delete_from", weightMedium, `\bdelete\s+from\b`),
	signal("update_set", weightMedium, `\bupdate\s+\S+\s+set\b`),
	signal("drop_object", weightMedium, `\bdrop\s+(table|database|schema|index|view|trigger)\b`),
	signal("alter_table", weightMedium, `\b(alter|truncate)\s+table\b`),
	signal("create_object", weightMedium, `\bcreate\s+(table|trigger|view)\b`),
	signal("where_clause", weightMedium, `\bwhere\s+\S+\s*(=|like\b)`),
	signal("order_by_number", weightMedium, `\b(order|group)\s+by\s+\d+`),
	signal("terminated_comment", weightMedium, `;\s*(--|#)`),

	signal("trailing_comment", weightWeak, `(--|#)\s*$`),
	signal("block_comment", weightWeak, `/\*`),
	signal("trailing_semicolon", weightWeak, `;\s*$`),
	signal("sql_function", weightWeak, `\b(char|chr|concat|ascii|substr|substring|unhex|cast|convert|group_concat)\s*\(`),
	signal("hex_literal", weightWeak, `\b0x[0-9a-f]{8,}\b`),
}

var (
	inlineComment = regexp.MustCompile(`/\*.*?\*/`)
	whitespace    = regexp.MustCompile(`\s+`)
)

// normalizeSQL hebt die üblichen Verschleierungen auf: Groß-/Kleinschreibung,
// URL-Kodierung, Kommentare statt Leerzeichen (UN/**/ION bleibt getrennt,
// aber union/**/select wird zu union select) und mehrfacher Leerraum.
func normalizeSQL(s string) string {
	if len(s) > maxScanLen {
		s = s[:maxScanLen]
	}
	if strings.Contains(s, "%") {
		if decoded, err := url.QueryUnescape(s); err == nil {
			s = decoded
		}
	}
	s = strings.ToLower(s)
	s = inlineComment.ReplaceAllString(s, " ")
	return whitespace.ReplaceAllString(s, " ")
}

// SQLScore bewertet einen Text und nennt die Signale, die angeschlagen haben.
func SQLScore(s string) (score int, signals []string) {
	normalized := normalizeSQL(s)
	for _, sig := range sqlSignals {
		if sig.pattern.MatchString(normalized) {
			score += sig.weight
			signals = append(signals, sig.name)
		}
	}
	return score, signals
}

// LooksLikeSQL meldet, ob ein Text die Schwelle erreicht.
func LooksLikeSQL(s string) bool {
	score, _ := SQLScore(s)
	return score >= SQLThreshold
}
