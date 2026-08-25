package services

import "regexp"

// redactPlaceholder ersetzt erkannte Geheimnisse in Protokollen.
const redactPlaceholder = "«redacted»"

// Redaction-Muster für die SSH-Protokollierung. Ziel ist, dass Passwörter
// und andere Geheimnisse NICHT im Klartext in den Protokollen landen, ohne
// den Protokollwert (Nachvollziehbarkeit) zu zerstören.
var (
	// chpasswd bekommt Credentials via "printf '%s:%s' user 'secret' | chpasswd"
	// (siehe provisionScript). Gezielt an der %s:%s-printf verankert (robust
	// auch bei shell-escapten Quotes '\'' durch die sudo-Verpackung) und nur
	// bis zur Pipe - so bleibt der restliche Kontext im Protokoll erhalten.
	reChpasswd = regexp.MustCompile(`printf\s+(?:'\\''|')%s:%s(?:'\\''|').*?\|\s*chpasswd`)

	// curl/wget-Credentials: -u user:pass bzw. --user=user:pass.
	reCurlUser = regexp.MustCompile(`((?:-u|--user)[ =][^\s:]+:)(\S+)`)

	// Generische Schlüssel=Wert-Geheimnisse. Verlangt einen '='/':'-Trenner,
	// damit z.B. "PasswordAuthentication no" NICHT fälschlich getroffen wird.
	reKeyValueSecret = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|auth[_-]?token)(\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)`)

	// Basic-Auth in URLs: https://user:pass@host -> pass unkenntlich.
	reURLBasicAuth = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+:)([^@/\s]+)(@)`)

	// apt auth.conf im netrc-Format ("password <secret>", Leerzeichen statt
	// '='): geschrieben von der Enterprise-Kanal-Umstellung via
	// "printf 'machine %s\nlogin %s\npassword %s\n' URL LOGIN SECRET".
	// Gezielt am Format-String verankert (robust auch bei shell-escapten
	// Quotes '\'' durch die sudo-Verpackung) - das dritte Argument ist der
	// Zugangsschlüssel.
	reAptAuthSecret = regexp.MustCompile(`(printf\s+(?:'\\''|')machine %s\\nlogin %s\\npassword %s\\n(?:'\\''|')\s+(?:'\\''|')[^']*(?:'\\''|')\s+(?:'\\''|')[^']*(?:'\\''|')\s+(?:'\\''|'))([^']*)((?:'\\''|'))`)
)

// redactSecrets entfernt bekannte Geheimnisse aus einer Kommandozeile oder
// deren Ausgabe. Idempotent und robust gegenüber mehreren Fundstellen.
func redactSecrets(s string) string {
	if s == "" {
		return s
	}
	s = reChpasswd.ReplaceAllString(s, "printf '%s:%s' "+redactPlaceholder+" | chpasswd")
	s = reCurlUser.ReplaceAllString(s, "${1}"+redactPlaceholder)
	s = reKeyValueSecret.ReplaceAllString(s, "${1}${2}"+redactPlaceholder)
	s = reURLBasicAuth.ReplaceAllString(s, "${1}"+redactPlaceholder+"${3}")
	s = reAptAuthSecret.ReplaceAllString(s, "${1}"+redactPlaceholder+"${3}")
	return s
}

// Grenzen für die gespeicherte Kommando-Ausgabe: sehr lange Ausgaben (z.B.
// ein volles apt-upgrade) werden in der Mitte gekürzt - Anfang UND Ende
// bleiben erhalten (Fehler stehen oft am Ende).
const (
	maxOutputBytes  = 60_000
	keepHeadBytes   = 40_000
	keepTailBytes   = 16_000
	truncationNotic = "\n\n… [Ausgabe gekürzt] …\n\n"
)

// truncateOutput kürzt eine übergroße Ausgabe mittig.
func truncateOutput(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:keepHeadBytes] + truncationNotic + s[len(s)-keepTailBytes:]
}
