package sshx

import (
	"errors"
	"testing"
)

// Die Unterscheidung zwischen „der Server lässt keine Passwörter zu" und
// „das Passwort war falsch" - beides endet in x/crypto mit derselben
// Schlussfloskel, meint aber Gegenteiliges.

// x/crypto/ssh formuliert den Fehlschlag stets nach demselben Muster; der
// Unterschied steckt allein in der Liste der versuchten Methoden.
const (
	// Server bot nur publickey an: es blieb bei "none".
	errOnlyNone = "ssh: handshake failed: ssh: unable to authenticate, attempted methods [none], no supported methods remain"
	// Server bot Passwort an, wies die Zugangsdaten aber zurück.
	errWrongPassword = "ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain"
	// Mit keyboard-interactive als zusätzlicher Methode.
	errWrongPasswordKbd = "ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password keyboard-interactive], no supported methods remain"
)

// TestFalschesPasswortIstKeinGehaerteterServer: Der Fall aus dem Testlauf.
// Ein Tippfehler im Passwort wurde als „Server bietet keine Passwort-Anmeldung
// an (vermutlich SSH-gehärtet)" gemeldet - mit der Empfehlung, auf
// Schlüssel-Anmeldung umzustellen. Wer dem folgte, baute die sshd-Konfiguration
// um, um ein Problem zu beheben, das es nicht gab.
func TestFalschesPasswortIstKeinGehaerteterServer(t *testing.T) {
	for _, msg := range []string{errWrongPassword, errWrongPasswordKbd} {
		err := errors.New(msg)
		if isNoPasswordAuthError(err) {
			t.Errorf("abgelehntes passwort darf nicht als gehärteter server gelten:\n%s", msg)
		}
		if !isAuthRejectedError(err) {
			t.Errorf("abgelehntes passwort sollte als ablehnung erkannt werden:\n%s", msg)
		}
	}
}

// TestNurNoneVersuchtIstEinGehaerteterServer: Die Gegenprobe - wurde
// ausschließlich „none" versucht, bot der Server tatsächlich keine
// Passwort-Methode an. Diese Erkennung muss erhalten bleiben, sonst schickt
// LCM Nutzer gehärteter Server auf die Suche nach einem Tippfehler.
func TestNurNoneVersuchtIstEinGehaerteterServer(t *testing.T) {
	err := errors.New(errOnlyNone)
	if !isNoPasswordAuthError(err) {
		t.Errorf("nur 'none' versucht muss als gehärteter server gelten:\n%s", errOnlyNone)
	}
}

// TestMethodenlisteWirdNichtVerwechselt: „[none]" darf nicht in „[none
// password]" hineingelesen werden - die schließende Klammer ist das einzige,
// was die beiden Fälle im Text trennt.
func TestMethodenlisteWirdNichtVerwechselt(t *testing.T) {
	if isNoPasswordAuthError(errors.New("attempted methods [none password]")) {
		t.Error("'[none password]' wurde fälschlich als '[none]' gelesen")
	}
	if !isNoPasswordAuthError(errors.New("attempted methods [none]")) {
		t.Error("'[none]' wurde nicht erkannt")
	}
}
