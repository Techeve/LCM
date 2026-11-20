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

// TestLoginGuardSweepsStaleEntries: Einträge, deren Sperre abgelaufen und
// deren letzter Fehlversuch älter als das Zerfallsfenster ist, verschwinden
// beim nächsten Fehlversuch - auch weit unter jedem Füllstand. Ein frischer
// Zähler bleibt stehen.
func TestLoginGuardSweepsStaleEntries(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	g := newLoginGuard()
	g.now = func() time.Time { return now }

	for i := 0; i < maxLoginFails; i++ {
		g.fail("gesperrt")
	}
	g.fail("einmal")
	now = now.Add(loginDecayWindow + time.Second)
	g.fail("frisch")
	now = now.Add(loginSweepInterval)
	g.fail("neu")

	for _, gone := range []string{"gesperrt", "einmal"} {
		if _, ok := g.state[gone]; ok {
			t.Errorf("abgelaufener Eintrag %q liegt noch in der Map", gone)
		}
	}
	if st := g.state["frisch"]; st == nil || st.fails != 1 {
		t.Errorf("frischer Zähler wurde weggeräumt: %+v", st)
	}
	if len(g.state) != 2 {
		t.Errorf("%d Einträge, erwartet 2 (frisch, neu)", len(g.state))
	}
}

// TestLoginGuardUncountKeepsEarlierFails: Eine erfolgreiche Anmeldung nimmt
// nur ihren eigenen, vorab gezählten Versuch zurück. Die Fehlversuche davor
// bleiben, der nächste verhängt die Sperre.
func TestLoginGuardUncountKeepsEarlierFails(t *testing.T) {
	g := newLoginGuard()
	for i := 0; i < maxLoginFails-1; i++ {
		g.fail("ip:203.0.113.9")
	}
	// Der erfolgreiche Versuch erreicht die Schwelle - und nimmt sich zurück.
	if locked, _ := g.beginAttempt("ip:203.0.113.9", maxLoginFails); locked {
		t.Fatal("vor dem fünften Versuch schon gesperrt")
	}
	g.uncount("ip:203.0.113.9", maxLoginFails)
	if locked, _ := g.blocked("ip:203.0.113.9"); locked {
		t.Fatal("die eigene Anmeldung darf nicht sperren")
	}
	if st := g.state["ip:203.0.113.9"]; st == nil || st.fails != maxLoginFails-1 {
		t.Fatalf("Fehlversuche vor der Anmeldung verloren: %+v", st)
	}
	g.fail("ip:203.0.113.9")
	if locked, _ := g.blocked("ip:203.0.113.9"); !locked {
		t.Error("der nächste Fehlversuch muss sperren")
	}
}

// TestLoginGuardUncountWithoutFails: Ohne frühere Fehlversuche bleibt nach
// einer Anmeldung kein Eintrag zurück.
func TestLoginGuardUncountWithoutFails(t *testing.T) {
	g := newLoginGuard()
	g.beginAttempt("ip:203.0.113.9", maxLoginFails)
	g.uncount("ip:203.0.113.9", maxLoginFails)
	if len(g.state) != 0 {
		t.Errorf("%d Einträge nach einer Anmeldung ohne Fehlversuch", len(g.state))
	}
}
