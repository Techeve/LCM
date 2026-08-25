package services

import (
	"errors"
	"strings"
	"testing"
)

// TestPasswordPolicyRejects prüft, dass die Policy alle Klassen schwacher
// Passwörter ablehnt - und mit dem ERWARTETEN Code begründet, weil die
// Oberfläche daraus ihre übersetzte Meldung baut.
func TestPasswordPolicyRejects(t *testing.T) {
	id := PasswordIdentity{
		Username: "tgratscher", Email: "tony.gratscher@techeve.de",
		FirstName: "Tony", LastName: "Gratscher",
	}
	cases := []struct {
		name     string
		password string
		wantCode string
	}{
		{"zu kurz", "Kurz1!xy", PwProblemTooShort},
		{"nur Kleinbuchstaben", "einfachnurtext", PwProblemClasses},
		{"Standard-Passwort", "Passwort123!", PwProblemCommon},
		{"Standard-Passwort mit Leetspeak", "P4ssw0rt!2026", PwProblemCommon},
		{"enthält Benutzernamen", "Xq7-tgratscher-Wolke", PwProblemIdentity},
		{"enthält Vornamen", "Nordwind-Tony-42!", PwProblemIdentity},
		{"enthält Vornamen in Leetspeak", "Nordwind-T0ny-42!", PwProblemIdentity},
		{"enthält E-Mail-Teil", "Segel-gratscher-9!", PwProblemIdentity},
		{"Ziffernfolge", "Wolke1234!Regen", PwProblemSequence},
		{"Tastaturfolge", "Wolke-qwertz-9!X", PwProblemSequence},
		{"Zeichenwiederholung", "Wolkeaaa!9Regen", PwProblemRepeat},
		{"wiederholter Block", "Ab1!Ab1!Ab1!Ab1!", PwProblemRepeat},
		{"zu wenig verschiedene Zeichen", "Aa1!Aa1!Aa1!", PwProblemRepeat},
		{"vom Kontextwort dominiert", "admin-admin-1A!", PwProblemContextWord},
		{"Leerzeichen am Rand", " Wolke7-Nordlicht!Kx ", PwProblemWhitespace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := CheckPasswordPolicy(tc.password, id)
			if check.OK {
				t.Fatalf("%q wurde akzeptiert, sollte aber abgelehnt werden", tc.password)
			}
			var codes []string
			for _, p := range check.Problems {
				codes = append(codes, p.Code)
			}
			found := false
			for _, c := range codes {
				if c == tc.wantCode {
					found = true
				}
			}
			if !found {
				t.Errorf("erwarteter Code %q fehlt; bekam: %v", tc.wantCode, codes)
			}
		})
	}
}

// TestPasswordPolicyAccepts stellt sicher, dass die Policy brauchbare
// Passwörter NICHT blockiert - eine zu strenge Regel treibt Nutzer sonst zu
// Zetteln am Monitor.
func TestPasswordPolicyAccepts(t *testing.T) {
	id := PasswordIdentity{
		Username: "tgratscher", Email: "tony.gratscher@techeve.de",
		FirstName: "Tony", LastName: "Gratscher",
	}
	good := []string{
		"Wolke7-Nordlicht!Kx",
		"Zeder8-Kastanie!Brunnen",
		"korrekt Pferd Batterie Klammer 9",    // lange Passphrase, 2 Klassen genügen
		"vier wilde muscheln tanzen im hafen", // 35 Zeichen, nur Kleinbuchstaben + Leerzeichen
	}
	for _, pw := range good {
		check := CheckPasswordPolicy(pw, id)
		if !check.OK {
			var msgs []string
			for _, p := range check.Problems {
				msgs = append(msgs, p.Code)
			}
			t.Errorf("%q wurde abgelehnt (%s), sollte aber zulässig sein", pw, strings.Join(msgs, ", "))
		}
		if check.Score < 2 {
			t.Errorf("%q: Score %d ist zu niedrig für ein zulässiges Passwort", pw, check.Score)
		}
	}
}

// TestPasswordPolicyLengthCountsRunes deckt die Byte-vs-Zeichen-Falle ab:
// „len()" hätte Umlaute doppelt gezählt und ein zu kurzes Passwort durchgelassen.
func TestPasswordPolicyLengthCountsRunes(t *testing.T) {
	// 10 Zeichen, aber 15 Bytes (jeder Umlaut 2 Bytes) - muss zu kurz sein.
	check := CheckPasswordPolicy("Äöü1!Äöü2!", PasswordIdentity{})
	if check.OK {
		t.Fatal("10-Zeichen-Passwort wurde akzeptiert - Länge wird offenbar in Bytes gezählt")
	}
	if check.Problems[0].Code != PwProblemTooShort {
		t.Errorf("erwartet %q, bekam %q", PwProblemTooShort, check.Problems[0].Code)
	}
}

// TestEnforcePasswordPolicyErrorIsWeakPassword sichert die Fehlerkette ab:
// die Controller mappen über errors.Is(err, ErrWeakPassword) auf HTTP 422.
func TestEnforcePasswordPolicyErrorIsWeakPassword(t *testing.T) {
	err := EnforcePasswordPolicy("kurz", PasswordIdentity{})
	if err == nil {
		t.Fatal("schwaches Passwort muss einen Fehler liefern")
	}
	var policyErr *PasswordPolicyError
	if !errors.As(err, &policyErr) {
		t.Fatal("Fehler muss ein *PasswordPolicyError sein, damit die API die Gründe ausliefern kann")
	}
	if len(policyErr.Check.Problems) == 0 {
		t.Error("der Fehler muss die konkreten Regelverstöße mitführen")
	}
	if !errors.Is(err, ErrWeakPassword) {
		t.Error("errors.Is(err, ErrWeakPassword) muss wahr bleiben - sonst antwortet die API mit 500 statt 422")
	}
}
