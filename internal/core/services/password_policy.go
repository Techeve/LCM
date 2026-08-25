package services

import (
	"strings"
	"unicode"
)

// Passwort-Policy. Der Server ist die ALLEINIGE Autorität: die Prüfung in der
// UI (frontend/src/lib/passwordPolicy.js) spiegelt diese Regeln nur für
// sofortiges Feedback beim Tippen - sie ist bewusst kein Sicherheitsmerkmal
// und kann umgangen werden. Jede Stelle, die ein Passwort setzt, MUSS
// CheckPasswordPolicy aufrufen.
//
// Die Problem-Codes sind maschinenlesbar, damit die Oberfläche sie
// zweisprachig ausgeben kann; die deutsche Meldung ist der Fallback für
// direkte API-Nutzer.

const (
	// PasswordMinLength ist die harte Untergrenze für neue Passwörter.
	PasswordMinLength = 12
	// PasswordMaxLength deckelt die Eingabe. argon2id kommt mit langen
	// Eingaben zurecht; die Grenze verhindert Speicher-/Rechen-Missbrauch
	// über absurd große Request-Bodies.
	PasswordMaxLength = 200
	// PasswordPassphraseLength ist die Länge, ab der ein Passwort als
	// Passphrase gilt: dann genügen 2 statt 3 Zeichenklassen. Länge schlägt
	// Komplexität (NIST SP 800-63B) - „richtiges pferd batterie klammer"
	// soll erlaubt sein, „Sommer1!" nicht.
	PasswordPassphraseLength = 20
	// passwordMinUnique ist die Mindestzahl VERSCHIEDENER Zeichen.
	passwordMinUnique = 6
)

// Problem-Codes der Passwort-Policy (stabil - die UI übersetzt sie).
const (
	PwProblemTooShort     = "too_short"
	PwProblemTooLong      = "too_long"
	PwProblemClasses      = "needs_classes"
	PwProblemLowVariety   = "low_variety"
	PwProblemIdentity     = "contains_identity"
	PwProblemContextWord  = "context_word"
	PwProblemCommon       = "common_password"
	PwProblemSequence     = "sequence"
	PwProblemRepeat       = "repeat"
	PwProblemWhitespace   = "whitespace_edges"
	PwProblemControlChars = "control_chars"
)

// PasswordProblem ist ein einzelner Regelverstoß.
type PasswordProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PasswordCheck ist das Ergebnis der Policy-Prüfung.
type PasswordCheck struct {
	OK       bool              `json:"ok"`
	Score    int               `json:"score"` // 0..4, rein informativ für die UI
	Problems []PasswordProblem `json:"problems"`
}

// PasswordIdentity sind die personenbezogenen Angaben, die NICHT im Passwort
// vorkommen dürfen. Alle Felder sind optional.
type PasswordIdentity struct {
	Username  string
	Email     string
	FirstName string
	LastName  string
}

// PasswordPolicyError transportiert die KONKRETEN Regelverstöße bis zur API,
// damit die Oberfläche nicht raten muss, was zu ändern ist. errors.Is(err,
// ErrWeakPassword) bleibt bewusst wahr - alle bestehenden Controller mappen
// darüber weiterhin auf HTTP 422.
type PasswordPolicyError struct {
	Check PasswordCheck
}

func (e *PasswordPolicyError) Error() string {
	msgs := make([]string, 0, len(e.Check.Problems))
	for _, p := range e.Check.Problems {
		msgs = append(msgs, p.Message)
	}
	if len(msgs) == 0 {
		return ErrWeakPassword.Error()
	}
	return "passwort zu schwach: " + strings.Join(msgs, "; ")
}

// Is macht den typisierten Fehler zu einem ErrWeakPassword - so bleiben alle
// vorhandenen errors.Is-Prüfungen in den Controllern gültig.
func (e *PasswordPolicyError) Is(target error) bool { return target == ErrWeakPassword }

// EnforcePasswordPolicy ist der EINZIGE Weg, ein Passwort freizugeben. Jede
// Stelle, die ein Passwort setzt, ruft diese Funktion auf - so kann die Policy
// nicht an einer Stelle veralten (frühere Fassung prüfte an vier Stellen
// getrennt nur die Länge).
func EnforcePasswordPolicy(password string, identity PasswordIdentity) error {
	if check := CheckPasswordPolicy(password, identity); !check.OK {
		return &PasswordPolicyError{Check: check}
	}
	return nil
}

// passwordContextWords sind Wörter aus dem Umfeld dieser Anwendung. Sie sind
// nicht verboten, dürfen ein Passwort aber nicht dominieren (siehe
// checkContextWords) - „lcm-admin-2026" ist damit raus, „Nordlicht-lcm-Wal!"
// bleibt erlaubt.
var passwordContextWords = []string{
	"lcm", "techeve", "admin", "administrator", "root", "server", "linux",
	"passwort", "password", "kennwort", "login", "secret", "geheim",
	"backup", "sicherung",
}

// passwordCommonBases ist eine kuratierte Liste der weltweit häufigsten
// Passwörter samt deutscher Klassiker. Verglichen wird die NORMALISIERTE
// Form (siehe normalizePasswordBase), damit „P4ssw0rt!23" genauso auffliegt
// wie „passwort".
var passwordCommonBases = map[string]bool{
	"password": true, "passwort": true, "kennwort": true, "passwd": true,
	"123456": true, "12345678": true, "123456789": true, "1234567890": true,
	"qwerty": true, "qwertz": true, "qwertzuiop": true, "qwertyuiop": true,
	"asdfgh": true, "asdfghjkl": true, "yxcvbnm": true, "zxcvbnm": true,
	"letmein": true, "welcome": true, "willkommen": true, "monkey": true,
	"dragon": true, "master": true, "sunshine": true, "princess": true,
	"football": true, "baseball": true, "iloveyou": true, "ichliebedich": true,
	"admin": true, "administrator": true, "root": true, "toor": true,
	"superman": true, "batman": true, "trustno": true, "whatever": true,
	"starwars": true, "pokemon": true, "hallo": true, "hello": true,
	"schatz": true, "sommer": true, "winter": true, "fruehling": true,
	"herbst": true, "deutschland": true, "berlin": true, "muenchen": true,
	"hamburg": true, "bayern": true, "borussia": true, "schalke": true,
	"arschloch": true, "scheisse": true, "ficken": true, "geheim": true,
	"test": true, "testtest": true, "demo": true, "default": true,
	"changeme": true, "aendermich": true, "secret": true, "geheimnis": true,
	"willkommen1": true, "computer": true, "internet": true, "system": true,
	"server": true, "linux": true, "ubuntu": true, "debian": true,
	"mustermann": true, "maxmustermann": true, "abcdef": true, "abcdefg": true,
	"qazwsx": true, "zaqwsx": true, "aaaaaa": true, "111111": true,
	"000000": true, "121212": true, "abc123": true, "a1b2c3": true,
	"passwort1": true, "password1": true, "michael": true, "thomas": true,
	"andreas": true, "stefan": true, "sabine": true, "julia": true,
}

// passwordSequences sind Zeichenreihen, aus denen Läufe (vorwärts wie
// rückwärts) als vorhersagbar gelten: Tastaturreihen (DE + EN) sowie
// Alphabet und Ziffern.
var passwordSequences = []string{
	"abcdefghijklmnopqrstuvwxyz",
	"0123456789",
	"qwertzuiopü", "asdfghjklöä", "yxcvbnm", // DE-Tastatur
	"qwertyuiop", "asdfghjkl", "zxcvbnm", // EN-Tastatur
	"1qaz2wsx", "qazwsxedc", // gängige Zickzack-Muster
}

// passwordLeetReplacer normalisiert gängige Zeichenersetzungen, damit
// „P@ssw0rt" auf „passwort" zurückfällt.
var passwordLeetReplacer = strings.NewReplacer(
	"@", "a", "4", "a", "8", "b", "(", "c", "3", "e", "6", "g",
	"1", "i", "!", "i", "|", "i", "0", "o", "5", "s", "$", "s",
	"7", "t", "+", "t", "2", "z", "9", "g",
)

// CheckPasswordPolicy prüft ein Passwort gegen die vollständige Policy.
// identity darf leer sein (z.B. beim Anlegen, bevor der Name feststeht) -
// dann entfallen nur die personenbezogenen Prüfungen.
func CheckPasswordPolicy(password string, identity PasswordIdentity) PasswordCheck {
	var problems []PasswordProblem
	add := func(code, msg string) {
		problems = append(problems, PasswordProblem{Code: code, Message: msg})
	}

	// Länge wird über RUNEN gezählt - sonst zählte ein Umlaut doppelt und
	// „müßiggängerisch" wäre fälschlich „lang genug".
	runes := []rune(password)
	length := len(runes)

	switch {
	case length < PasswordMinLength:
		add(PwProblemTooShort, "Passwort muss mindestens 12 Zeichen lang sein")
	case length > PasswordMaxLength:
		add(PwProblemTooLong, "Passwort darf höchstens 200 Zeichen lang sein")
	}
	// Zu kurz/zu lang: die inhaltlichen Prüfungen sind dann nicht aussagekräftig.
	if length < PasswordMinLength || length > PasswordMaxLength {
		return PasswordCheck{OK: false, Score: 0, Problems: problems}
	}

	if strings.TrimSpace(password) != password {
		add(PwProblemWhitespace, "Passwort darf nicht mit Leerzeichen beginnen oder enden")
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			add(PwProblemControlChars, "Passwort darf keine Steuerzeichen enthalten")
			break
		}
	}

	classes := countPasswordClasses(runes)
	required := 3
	if length >= PasswordPassphraseLength {
		// Lange Passphrasen brauchen weniger Zeichenklassen - Länge liefert
		// die Entropie.
		required = 2
	}
	if classes < required {
		add(PwProblemClasses, "Passwort braucht mehr Vielfalt: Groß-, Kleinbuchstaben, Ziffern und Sonderzeichen (mindestens 3 Arten; ab 20 Zeichen genügen 2)")
	}

	if countUniqueRunes(runes) < passwordMinUnique {
		add(PwProblemLowVariety, "Passwort verwendet zu wenige verschiedene Zeichen")
	}

	if code, ok := checkIdentityTokens(password, identity); !ok {
		add(code, "Passwort darf weder Benutzernamen noch Vor-/Nachnamen oder E-Mail-Adresse enthalten")
	}

	if !checkContextWords(password) {
		add(PwProblemContextWord, "Passwort besteht im Wesentlichen aus einem naheliegenden Begriff (z.B. „admin“, „passwort“, „lcm“)")
	}

	if isCommonPassword(password) {
		add(PwProblemCommon, "Passwort ist ein bekanntes Standard-Passwort und damit sofort zu erraten")
	}

	if hasSequenceRun(password, 4) {
		add(PwProblemSequence, "Passwort enthält eine vorhersagbare Zeichenfolge (z.B. „1234“, „abcd“, „qwertz“)")
	}

	if hasRepeatRun(runes, 3) || isRepeatedBlock(password) {
		add(PwProblemRepeat, "Passwort enthält zu viele Wiederholungen")
	}

	return PasswordCheck{
		OK:       len(problems) == 0,
		Score:    passwordScore(runes, classes, len(problems)),
		Problems: problems,
	}
}

// countPasswordClasses zählt die vertretenen Zeichenklassen: Kleinbuchstaben,
// Großbuchstaben, Ziffern, sonstige (Sonderzeichen/Symbole/Leerzeichen).
func countPasswordClasses(runes []rune) int {
	var lower, upper, digit, other bool
	for _, r := range runes {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	n := 0
	for _, ok := range []bool{lower, upper, digit, other} {
		if ok {
			n++
		}
	}
	return n
}

func countUniqueRunes(runes []rune) int {
	seen := map[rune]bool{}
	for _, r := range runes {
		seen[unicode.ToLower(r)] = true
	}
	return len(seen)
}

// checkIdentityTokens lehnt Passwörter ab, die den Benutzernamen, Vor-/
// Nachnamen oder den lokalen Teil der E-Mail enthalten. Verglichen wird
// normalisiert (klein, ohne Leet), damit „T0nyGratscher!" nicht durchrutscht.
func checkIdentityTokens(password string, identity PasswordIdentity) (string, bool) {
	haystack := normalizePasswordBase(password)
	if haystack == "" {
		return "", true
	}
	tokens := []string{identity.Username, identity.FirstName, identity.LastName}
	if local, _, found := strings.Cut(identity.Email, "@"); found {
		tokens = append(tokens, local)
		// „vorname.nachname@…" ebenfalls in seine Teile zerlegen.
		tokens = append(tokens, strings.FieldsFunc(local, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})...)
	}
	for _, token := range tokens {
		t := normalizePasswordBase(token)
		// Kürzere Token (< 3) wären zu unspezifisch und würden harmlose
		// Passwörter grundlos ablehnen.
		if len(t) < 3 {
			continue
		}
		if strings.Contains(haystack, t) {
			return PwProblemIdentity, false
		}
	}
	return "", true
}

// checkContextWords lehnt Passwörter ab, die von einem naheliegenden Begriff
// DOMINIERT werden: nach Entfernen des Begriffs bleibt zu wenig übrig.
// „admin-admin-2026" fliegt raus, „Nordlicht-lcm-Wal!" bleibt zulässig.
func checkContextWords(password string) bool {
	base := normalizePasswordBase(password)
	if base == "" {
		return true
	}
	for _, word := range passwordContextWords {
		if !strings.Contains(base, word) {
			continue
		}
		if len(strings.ReplaceAll(base, word, "")) < 8 {
			return false
		}
	}
	return true
}

// isCommonPassword prüft gegen die Liste bekannter Standard-Passwörter.
//
// Verglichen werden MEHRERE Normalformen, weil sich die Tricks überlagern:
// „Passwort123!" versteckt sich hinter angehängten Ziffern, „P@ssw0rt" hinter
// Zeichenersetzungen. Eine einzige Form genügt nicht - die Leet-Ersetzung
// wandelt Ziffern in Buchstaben und macht das Abschneiden angehängter Zahlen
// unmöglich („passwort123" → „passwortize"). Deshalb beide Wege getrennt.
func isCommonPassword(password string) bool {
	for _, cand := range passwordCandidates(password) {
		if passwordCommonBases[cand] {
			return true
		}
	}
	return false
}

// passwordCandidates liefert die Vergleichsformen eines Passworts:
// reine Buchstaben/Ziffern (klein), dieselbe Form ohne angehängte Ziffern,
// und die Leet-normalisierte Form.
func passwordCandidates(password string) []string {
	plain := stripToAlnum(strings.ToLower(strings.TrimSpace(password)))
	trimmed := strings.TrimRight(plain, "0123456789")
	out := make([]string, 0, 4)
	for _, c := range []string{
		plain,
		trimmed,
		normalizePasswordBase(password),
		// Erst Ziffern abschneiden, DANN Leet auflösen: „P4ssw0rt!2026"
		// wird nur so zu „passwort". Andersherum verwandelt die
		// Leet-Ersetzung die angehängte Jahreszahl in Buchstaben, und das
		// Abschneiden greift ins Leere.
		stripToAlnum(passwordLeetReplacer.Replace(trimmed)),
	} {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// stripToAlnum behält nur Buchstaben und Ziffern.
func stripToAlnum(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizePasswordBase bringt ein Passwort auf eine Vergleichsform: klein,
// Leet-Ersetzungen rückgängig, alle Nicht-Buchstaben/Ziffern entfernt.
func normalizePasswordBase(s string) string {
	return stripToAlnum(passwordLeetReplacer.Replace(strings.ToLower(strings.TrimSpace(s))))
}

// hasSequenceRun meldet einen Lauf von mindestens n Zeichen aus einer
// bekannten Reihe (Alphabet, Ziffern, Tastaturreihe) - vorwärts wie rückwärts.
func hasSequenceRun(password string, n int) bool {
	lower := strings.ToLower(password)
	for _, seq := range passwordSequences {
		runes := []rune(seq)
		for i := 0; i+n <= len(runes); i++ {
			window := string(runes[i : i+n])
			if strings.Contains(lower, window) || strings.Contains(lower, reverseString(window)) {
				return true
			}
		}
	}
	return false
}

// hasRepeatRun meldet n-fach dasselbe Zeichen hintereinander ("aaa").
func hasRepeatRun(runes []rune, n int) bool {
	run := 1
	for i := 1; i < len(runes); i++ {
		if unicode.ToLower(runes[i]) == unicode.ToLower(runes[i-1]) {
			run++
			if run >= n {
				return true
			}
			continue
		}
		run = 1
	}
	return false
}

// isRepeatedBlock meldet ein Passwort, das nur ein kurzer, wiederholter
// Block ist ("abcabcabcabc") - nach außen lang, tatsächlich kurz.
func isRepeatedBlock(password string) bool {
	runes := []rune(strings.ToLower(password))
	n := len(runes)
	for size := 1; size <= n/2; size++ {
		if n%size != 0 {
			continue
		}
		block := string(runes[:size])
		if strings.Repeat(block, n/size) == string(runes) {
			return true
		}
	}
	return false
}

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// passwordScore liefert eine grobe Stärke von 0 (unbrauchbar) bis 4 (sehr
// stark) für die Anzeige. Rein informativ - verbindlich ist PasswordCheck.OK.
func passwordScore(runes []rune, classes, problems int) int {
	if problems > 0 {
		// Mit Regelverstoß nie mehr als „schwach".
		if problems >= 3 {
			return 0
		}
		return 1
	}
	score := 2
	switch {
	case len(runes) >= 24:
		score += 2
	case len(runes) >= 16:
		score++
	}
	if classes == 4 {
		score++
	}
	if score > 4 {
		score = 4
	}
	return score
}
