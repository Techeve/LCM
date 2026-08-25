package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
)

// TestEnsureOnboardingKeyIdempotent: der Onboarding-Key wird einmal erzeugt
// und bei weiteren Aufrufen nicht überschrieben.
func TestEnsureOnboardingKeyIdempotent(t *testing.T) {
	env := newTestEnv(t)
	if err := env.Settings.EnsureOnboardingKey(); err != nil {
		t.Fatal(err)
	}
	s1, _ := env.Settings.Get()
	if s1.OnboardingPubKey == "" || !strings.HasPrefix(s1.OnboardingPubKey, "ssh-ed25519 ") {
		t.Fatalf("public key fehlt/ungültig: %q", s1.OnboardingPubKey)
	}
	if s1.OnboardingKeyEnc == "" {
		t.Fatal("verschlüsselter private key fehlt")
	}
	priv, err := env.Settings.OnboardingPrivateKey()
	if err != nil || !strings.Contains(priv, "PRIVATE KEY") {
		t.Fatalf("private key nicht entschlüsselbar: %v", err)
	}

	// Zweiter Aufruf ändert nichts.
	if err := env.Settings.EnsureOnboardingKey(); err != nil {
		t.Fatal(err)
	}
	s2, _ := env.Settings.Get()
	if s2.OnboardingPubKey != s1.OnboardingPubKey {
		t.Error("onboarding-key wurde beim zweiten aufruf überschrieben")
	}
}

// TestJoinWithSystemKey: Join per System-SSH-Key nutzt den Onboarding-Key
// für den initialen Login (statt Passwort).
func TestJoinWithSystemKey(t *testing.T) {
	env := newTestEnv(t)
	if err := env.Settings.EnsureOnboardingKey(); err != nil {
		t.Fatal(err)
	}
	priv, _ := env.Settings.OnboardingPrivateKey()

	env.Dialer.Responses = map[string]sshx.FakeResponse{
		"apt-get dnf zypper": {Output: "apt-get\n"}, // Debian → apt (Join prüft das)
		"sudo -n id -u":      {Output: "0\n"},       // Service-User erreicht root
		"os-release":         {Output: "NAME=\"Debian GNU/Linux\"\n"},
	}
	env.Dialer.KeyPEMs = nil
	// Passwort-Login würde scheitern (gehärteter Server) - darf gar nicht
	// erst versucht werden.
	env.Dialer.FailPassword = errors.New("password auth disabled")

	server, err := env.Servers.Join(services.JoinRequest{
		Name: "hardened01", Host: "10.0.0.9", Port: 22, LoginUser: "root",
		AuthMethod: services.AuthMethodKey, ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("join per key fehlgeschlagen: %v", err)
	}
	if server.ID == 0 {
		t.Fatal("server nicht angelegt")
	}
	// Der erste Dial (Login) lief per Key mit dem Onboarding-Key.
	if len(env.Dialer.KeyPEMs) == 0 || env.Dialer.KeyPEMs[0] != priv {
		t.Error("login lief nicht mit dem system-onboarding-key")
	}
}

// TestPasswordAuthUnavailableMessage: bietet der Server keine
// Passwort-Anmeldung, liefert DialPassword die klare, übersetzte Meldung.
func TestPasswordAuthUnavailableMessage(t *testing.T) {
	env := newTestEnv(t)
	env.Dialer.FailPassword = errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none], no supported methods remain")

	_, err := env.Servers.Join(services.JoinRequest{
		Name: "x", Host: "10.0.0.9", LoginUser: "root", LoginPassword: "egal",
		AuthMethod: services.AuthMethodPassword, ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if !errors.Is(err, sshx.ErrPasswordAuthUnavailable) {
		t.Fatalf("erwartete ErrPasswordAuthUnavailable, bekam %v", err)
	}
}

// TestJoinKeyWithoutOnboardingKey: Key-Login ohne vorhandenen Onboarding-Key
// liefert einen klaren Fehler.
func TestJoinKeyWithoutOnboardingKey(t *testing.T) {
	env := newTestEnv(t)
	// EnsureOnboardingKey NICHT aufgerufen → kein Key vorhanden.
	_, err := env.Servers.Join(services.JoinRequest{
		Name: "x", Host: "10.0.0.9", LoginUser: "root",
		AuthMethod: services.AuthMethodKey, ConfirmedFingerprint: env.Dialer.Fingerprint, Actor: "admin",
	})
	if !errors.Is(err, services.ErrNoOnboardingKey) {
		t.Fatalf("erwartete ErrNoOnboardingKey, bekam %v", err)
	}
}
