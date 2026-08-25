package services_test

import (
	"context"
	"testing"

	"LCM/internal/core/domain"
)

// TestTrefferquoteWaechstUeberDurchgaenge: Der zweite Durchgang beantwortet
// dieselben Pakete aus dem Zwischenspeicher - genau das soll die Quote
// zeigen.
func TestTrefferquoteWaechstUeberDurchgaenge(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 30)
	seedPackages(t, env, "web01",
		domain.Package{Name: "openssl", Version: "3.0.11"},
		domain.Package{Name: "bash", Version: "5.2"})

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 1: %v", err)
	}
	rep, err := env.Advisories.CacheStats()
	if err != nil {
		t.Fatalf("CacheStats: %v", err)
	}
	// Erster Durchgang: alles muss nachgefragt werden.
	if rep.Advisory.Hits != 0 || rep.Advisory.Misses != 2 {
		t.Errorf("erster Durchgang: erwartet 0 Treffer / 2 Fehlgriffe, war %d/%d",
			rep.Advisory.Hits, rep.Advisory.Misses)
	}
	if rep.Advisory.Runs != 1 || rep.Advisory.SinceAt.IsZero() {
		t.Errorf("Laufzähler oder Beginn fehlt: %+v", rep.Advisory)
	}

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	rep, _ = env.Advisories.CacheStats()
	if rep.Advisory.Hits != 2 || rep.Advisory.Misses != 2 {
		t.Errorf("zweiter Durchgang: erwartet 2 Treffer / 2 Fehlgriffe, war %d/%d",
			rep.Advisory.Hits, rep.Advisory.Misses)
	}
	if rep.Advisory.Runs != 2 {
		t.Errorf("erwartet 2 Durchgänge, waren %d", rep.Advisory.Runs)
	}
}

// TestMomentaufnahmeZaehltFrische: Die Belegung sagt für sich noch nichts -
// erst der Anteil der noch gültigen Einträge macht sie lesbar.
func TestMomentaufnahmeZaehltFrische(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 30)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	rep, err := env.Advisories.CacheStats()
	if err != nil {
		t.Fatalf("CacheStats: %v", err)
	}
	if rep.Snapshot.Entries != 1 || rep.Snapshot.Fresh != 1 {
		t.Errorf("erwartet 1 Eintrag, davon 1 frisch - war %d/%d",
			rep.Snapshot.Entries, rep.Snapshot.Fresh)
	}
	if rep.TTLMinutes != 30 {
		t.Errorf("eingestellte Gültigkeit fehlt im Bericht: %d", rep.TTLMinutes)
	}
	if rep.Snapshot.OldestAt == nil {
		t.Error("Alter des ältesten Eintrags fehlt")
	}
}

// TestOhneZwischenspeicherKeineFrischen: Bei TTL 0 ist kein Eintrag gültig -
// die Momentaufnahme darf das nicht beschönigen.
func TestOhneZwischenspeicherKeineFrischen(t *testing.T) {
	env := newTestEnv(t)
	enableAdvisories(t, env, 0)
	seedPackages(t, env, "web01", domain.Package{Name: "openssl", Version: "3.0.11"})

	if _, err := env.Advisories.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	rep, _ := env.Advisories.CacheStats()
	if rep.Snapshot.Fresh != 0 {
		t.Errorf("ohne Zwischenspeicher darf nichts als frisch gelten, waren %d", rep.Snapshot.Fresh)
	}
}
