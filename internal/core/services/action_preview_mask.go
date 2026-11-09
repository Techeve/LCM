package services

import (
	"regexp"
	"strings"
)

// previewMask ersetzt Geheimnisse in der Befehlsvorschau.
const previewMask = "••••"

// Muster, die über redactSecrets (SSH-Protokoll) hinausgehen. Die Vorschau
// zeigt auch frei geschriebene Skripte (Custom-Aktionen), in denen Geheimnisse
// in jeder Form stehen können - hier werden die häufigsten abgedeckt.
var (
	// Umgebungsvariablen mit sprechendem Namen: DB_PASSWORD=…, API_TOKEN=…
	// Bewusst nur GROSSGESCHRIEBENE Namen, sonst träfe es Optionen wie
	// StrictHostKeyChecking=no.
	rePreviewEnvSecret = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*(?:KEY|SECRET|TOKEN|PASS|PASSWORD|PWD|CREDENTIAL|AUTH)[A-Z0-9_]*=)("[^"]*"|'[^']*'|\S+)`)

	// Optionen mit Leerzeichen statt '=': --password geheim, --token abc.
	rePreviewFlagSecret = regexp.MustCompile(`(?i)(--(?:password|passwd|token|secret|api-key|apikey|auth-token)\s+)("[^"]*"|'[^']*'|\S+)`)

	// HTTP-Kopfzeilen: Authorization: Bearer …, Authorization: Basic …
	reAuthorizationHeader = regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic|token)\s+)([^\s"']+)`)

	// Passwort über stdin an sudo: echo 'geheim' | sudo -S …
	reSudoStdin = regexp.MustCompile(`((?:echo|printf)\s+(?:-n\s+)?(?:'%s\\n'\s+)?)("[^"]*"|'[^']*'|\S+)(\s*\|\s*sudo\s+-S\b)`)

	// Base64-verpackte Geheimnisse, die erst auf dem Server entpackt werden
	// (Passwort der CrowdSec-LAPI): printf '%s' '…' | base64 -d.
	reBase64Secret = regexp.MustCompile(`(printf '%s' ')([A-Za-z0-9+/=]+)(' \| base64 -d)`)

	// Enrollment-Key der CrowdSec-Console.
	reConsoleEnroll = regexp.MustCompile(`(cscli console enroll\s+)([A-Za-z0-9]+)`)

	// Private Schlüssel im Klartext.
	rePrivateKey = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)
)

// maskPreviewSecrets ersetzt Passwörter, Tokens, Schlüssel und sudo-Eingaben
// durch ••••. Es baut auf der Redaction des SSH-Protokolls auf, damit die
// Vorschau nichts zeigt, was das Protokoll verbirgt.
func maskPreviewSecrets(s string) string {
	s = redactSecrets(s)
	s = rePreviewEnvSecret.ReplaceAllString(s, "${1}"+previewMask)
	s = rePreviewFlagSecret.ReplaceAllString(s, "${1}"+previewMask)
	s = reAuthorizationHeader.ReplaceAllString(s, "${1}"+previewMask)
	s = reSudoStdin.ReplaceAllString(s, "${1}"+previewMask+"${3}")
	s = reBase64Secret.ReplaceAllString(s, "${1}"+previewMask+"${3}")
	s = reConsoleEnroll.ReplaceAllString(s, "${1}"+previewMask)
	s = rePrivateKey.ReplaceAllString(s, previewMask)
	return strings.ReplaceAll(s, redactPlaceholder, previewMask)
}
