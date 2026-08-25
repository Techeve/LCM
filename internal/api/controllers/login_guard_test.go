package controllers

import (
	"testing"
	"time"
)

func TestLoginGuardLocksAfterThreshold(t *testing.T) {
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	now := base
	g := newLoginGuard()
	g.now = func() time.Time { return now }

	// Bis zur Schwelle (maxLoginFails) ist noch nicht gesperrt.
	for i := 0; i < maxLoginFails-1; i++ {
		g.fail("admin")
		if locked, _ := g.blocked("admin"); locked {
			t.Fatalf("nach %d Fehlversuchen bereits gesperrt (zu früh)", i+1)
		}
	}
	// Der maxLoginFails-te Fehlversuch verhängt die Sperre.
	g.fail("admin")
	locked, rem := g.blocked("admin")
	if !locked || rem <= 0 {
		t.Fatalf("nach %d Fehlversuchen erwartet: gesperrt; bekam locked=%v rem=%v", maxLoginFails, locked, rem)
	}

	// Schlüssel ist case-insensitive/getrimmt.
	if l, _ := g.blocked("  ADMIN "); !l {
		t.Error("Sperre sollte case-insensitive greifen")
	}

	// Nach Ablauf der Sperrzeit wieder frei.
	now = now.Add(loginLockoutMax + time.Second)
	if l, _ := g.blocked("admin"); l {
		t.Error("nach Ablauf der Sperrzeit sollte wieder frei sein")
	}
}

func TestLoginGuardResetOnSuccess(t *testing.T) {
	g := newLoginGuard()
	for i := 0; i < maxLoginFails; i++ {
		g.fail("bob")
	}
	if l, _ := g.blocked("bob"); !l {
		t.Fatal("bob sollte nach den Fehlversuchen gesperrt sein")
	}
	g.reset("bob")
	if l, _ := g.blocked("bob"); l {
		t.Error("nach reset (erfolgreicher Login) darf keine Sperre mehr bestehen")
	}
}

func TestLoginGuardIndependentKeys(t *testing.T) {
	g := newLoginGuard()
	for i := 0; i < maxLoginFails; i++ {
		g.fail("admin")
	}
	// Eine Sperre für "admin" darf ein anderes Konto nicht betreffen.
	if l, _ := g.blocked("carol"); l {
		t.Error("Sperre eines Kontos darf andere Konten nicht sperren")
	}
}
