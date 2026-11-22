package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"LCM/internal/infrastructure/acmex"
)

// windowListener spielt den Listener für HTTP-01 und zählt Öffnen und Schließen.
type windowListener struct {
	opened  []string
	closed  int
	running bool
	err     error
}

func (l *windowListener) Start(addr string) error {
	if l.err != nil {
		return l.err
	}
	if !l.running {
		l.opened = append(l.opened, addr)
	}
	l.running = true
	return nil
}

func (l *windowListener) Stop()         { l.closed++; l.running = false }
func (l *windowListener) Running() bool { return l.running }

func newWindow(listener *windowListener) (challengeWindow, *ACMEService) {
	svc := &ACMEService{tokens: acmex.NewChallengeStore(), listener: listener, httpListen: ":80"}
	return challengeWindow{svc: svc}, svc
}

// Der Listener ist vor der ersten Prüfung zu, geht mit dem ersten Token auf
// (und bleibt für weitere Namen derselben Bestellung offen) und mit dem Ende
// des Laufs wieder zu.
func TestChallengeWindowOpensWithTheFirstToken(t *testing.T) {
	listener := &windowListener{}
	window, svc := newWindow(listener)
	ctx := context.Background()
	if listener.Running() {
		t.Fatal("der Listener ist vor der Prüfung offen")
	}

	if err := window.Present(ctx, "lcm.example.com", "tok-1", "tok-1.key"); err != nil {
		t.Fatal(err)
	}
	if err := window.Present(ctx, "agent.example.com", "tok-2", "tok-2.key"); err != nil {
		t.Fatal(err)
	}
	if !listener.Running() || len(listener.opened) != 1 || listener.opened[0] != ":80" {
		t.Errorf("Listener: offen %v, geöffnet %v", listener.Running(), listener.opened)
	}
	if key, ok := svc.tokens.KeyAuthorization("tok-1"); !ok || key != "tok-1.key" {
		t.Errorf("Token nicht ausgelegt: %q %v", key, ok)
	}

	if err := window.CleanUp(ctx, "lcm.example.com", "tok-1", "tok-1.key"); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.tokens.KeyAuthorization("tok-1"); ok {
		t.Error("aufgeräumtes Token ist noch offen")
	}

	svc.closeListener() // das Ende des Laufs
	if listener.Running() {
		t.Error("nach dem Lauf ist der Listener noch offen")
	}
}

// Lässt sich der Port nicht öffnen, scheitert die Prüfung mit dem Grund, und
// kein Token bleibt ausgelegt. Die Meldung steht im Zustand.
func TestChallengeWindowBindFailureIsTheError(t *testing.T) {
	listener := &windowListener{err: errors.New("acme-listener: :80 lässt sich nicht öffnen - CAP_NET_BIND_SERVICE")}
	window, svc := newWindow(listener)

	err := window.Present(context.Background(), "lcm.example.com", "tok", "tok.key")
	if err == nil || !strings.Contains(err.Error(), "CAP_NET_BIND_SERVICE") {
		t.Fatalf("Present: %v", err)
	}
	if _, ok := svc.tokens.KeyAuthorization("tok"); ok {
		t.Error("ein Token liegt aus, obwohl der Port nicht aufging")
	}
	if !strings.Contains(svc.listenErr, "CAP_NET_BIND_SERVICE") {
		t.Errorf("Grund fehlt im Zustand: %q", svc.listenErr)
	}

	listener.err = nil
	if err := window.Present(context.Background(), "lcm.example.com", "tok", "tok.key"); err != nil || svc.listenErr != "" {
		t.Errorf("nach einem Erfolg: %v, Zustand %q", err, svc.listenErr)
	}
}
