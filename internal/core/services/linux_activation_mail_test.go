package services_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/services"
)

// TestAktivierungsmailBrauchtEineAdresse: Das Feld für die Adresse gab es
// immer, den Weg dorthin nicht. Fehlt sie, muss die Absage das benennen -
// nicht in einem allgemeinen Mailfehler untergehen.
func TestAktivierungsmailBrauchtEineAdresse(t *testing.T) {
	env := newTestEnv(t)
	var gesendet int
	env.LinuxUsers.WithMailer(func(string, string, []string) error { gesendet++; return nil })

	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "ohnemail"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.LinuxUsers.MailActivation(u.ID, "tok", time.Now().Add(time.Hour)); !errors.Is(err, services.ErrUserNoEmail) {
		t.Errorf("erwartet ErrUserNoEmail, bekam %v", err)
	}
	if gesendet != 0 {
		t.Error("ohne Adresse darf nichts verschickt werden")
	}
}

// TestOhneVersandKeinMailweg: Ist kein Postausgang verdrahtet, sagt LCM das -
// statt so zu tun, als sei die Mail unterwegs.
func TestOhneVersandKeinMailweg(t *testing.T) {
	env := newTestEnv(t)
	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "mitmail", Email: "u@example.org"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.LinuxUsers.MailActivation(u.ID, "tok", time.Now().Add(time.Hour)); !errors.Is(err, services.ErrMailerDisabled) {
		t.Errorf("erwartet ErrMailerDisabled, bekam %v", err)
	}
}

// TestAktivierungsmailTraegtDenLink: Der Link muss auf die
// Selbstbedienungs-Seite zeigen und das Token enthalten - sonst ist die Mail
// nur ein Gruß.
func TestAktivierungsmailTraegtDenLink(t *testing.T) {
	env := newTestEnv(t)
	var betreff, text string
	var an []string
	env.LinuxUsers.WithMailer(func(s, b string, to []string) error {
		betreff, text, an = s, b, to
		return nil
	}).WithLinkBase(func() string { return "https://lcm.example.org" })

	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{
		Username: "mmustermann", FullName: "Max Mustermann", Email: "max@example.org",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.LinuxUsers.MailActivation(u.ID, "GEHEIM123", time.Now().Add(48*time.Hour)); err != nil {
		t.Fatalf("Versand: %v", err)
	}

	if len(an) != 1 || an[0] != "max@example.org" {
		t.Errorf("falscher Empfänger: %v", an)
	}
	if !strings.Contains(text, "https://lcm.example.org/#/linux-aktivierung?token=GEHEIM123") {
		t.Errorf("der Link fehlt oder ist falsch aufgebaut:\n%s", text)
	}
	if !strings.Contains(text, "mmustermann") {
		t.Error("der Benutzername fehlt - ohne ihn weiß der Empfänger nicht, wofür der Link gilt")
	}
	if !strings.Contains(text, "Max Mustermann") {
		t.Error("die Anrede nutzt den vollen Namen nicht")
	}
	// Ohne bestehende Zugangsdaten ist es eine Einrichtung, keine Rücksetzung.
	if !strings.Contains(betreff, "festlegen") {
		t.Errorf("Betreff passt nicht zur Ersteinrichtung: %q", betreff)
	}
}

// TestRuecksetzungKlingtNichtWieEinladung: Wer seinen Zugang längst nutzt und
// eine Einladung dafür bekommt, hält sie im Zweifel für einen Angriffsversuch.
func TestRuecksetzungKlingtNichtWieEinladung(t *testing.T) {
	env := newTestEnv(t)
	var betreff, text string
	env.LinuxUsers.WithMailer(func(s, b string, _ []string) error { betreff, text = s, b; return nil })

	u, err := env.LinuxUsers.Create(services.LinuxUserCreateInput{Username: "alt", Email: "alt@example.org"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// Ein hinterlegter Schlüssel macht aus der Einrichtung eine Rücksetzung.
	if _, err := env.LinuxUsers.AddSSHKey(u.ID, "laptop", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyFakeKeyFakeKeyFakeKeyFakeKeyFakeKey test", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.LinuxUsers.MailActivation(u.ID, "tok", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Versand: %v", err)
	}
	if !strings.Contains(betreff, "zurücksetzen") {
		t.Errorf("Betreff nennt die Rücksetzung nicht: %q", betreff)
	}
	if strings.Contains(text, "eingerichtet.") {
		t.Errorf("der Text lädt zu einem Zugang ein, den es längst gibt:\n%s", text)
	}
}
