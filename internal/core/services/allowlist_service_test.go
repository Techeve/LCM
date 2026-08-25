package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestSaveIPAllowlistCanonicalizes prüft, dass Einträge kanonisiert,
// dedupliziert und sortiert gespeichert werden (IPv4/IPv6 + CIDR).
func TestSaveIPAllowlistCanonicalizes(t *testing.T) {
	env := newTestEnv(t)
	saved, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{
		Name:    "Büro",
		Entries: "10.0.0.5\n10.0.0.5\n192.168.1.0/24\n2001:db8::1",
	}, "admin")
	if err != nil {
		t.Fatalf("speichern: %v", err)
	}
	entries := saved.EntryList()
	if len(entries) != 3 {
		t.Fatalf("erwartet 3 einträge (dedupliziert), bekam %d: %v", len(entries), entries)
	}
	// Kanonisch + sortiert.
	joined := strings.Join(entries, ",")
	for _, want := range []string{"10.0.0.5", "192.168.1.0/24", "2001:db8::1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("eintrag %q fehlt: %v", want, entries)
		}
	}
}

// TestSaveIPAllowlistValidation lehnt ungültige Namen/Einträge ab.
func TestSaveIPAllowlistValidation(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "", Entries: "10.0.0.1"}, "admin"); !errors.Is(err, services.ErrIPAllowlistInvalid) {
		t.Errorf("leerer name muss abgelehnt werden, bekam %v", err)
	}
	if _, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "Test", Entries: "kein-ip"}, "admin"); !errors.Is(err, services.ErrIPAllowlistInvalid) {
		t.Errorf("ungültiger eintrag muss abgelehnt werden, bekam %v", err)
	}
	// Doppelter Name.
	if _, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "Doppelt", Entries: "10.0.0.1"}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "Doppelt", Entries: "10.0.0.2"}, "admin"); !errors.Is(err, services.ErrIPAllowlistInvalid) {
		t.Errorf("doppelter name muss abgelehnt werden, bekam %v", err)
	}
}

// TestExpandIPAllowlists vereint mehrere Listen (dedupliziert, sortiert).
func TestExpandIPAllowlists(t *testing.T) {
	env := newTestEnv(t)
	a, _ := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "A", Entries: "10.0.0.1\n10.0.0.2"}, "admin")
	b, _ := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "B", Entries: "10.0.0.2\n192.168.0.0/16"}, "admin")

	ips, err := env.Settings.ExpandIPAllowlists([]uint{a.ID, b.ID})
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// Union dedupliziert: 10.0.0.1, 10.0.0.2, 192.168.0.0/16.
	if len(ips) != 3 {
		t.Fatalf("erwartet 3 (dedupliziert), bekam %d: %v", len(ips), ips)
	}
	// Nicht vorhandene ID ist ein FEHLER (R2-071/R2-075): früher wurde sie
	// still ausgelassen - je nach Pfad schloss das den Port wortlos oder
	// hebelte den Aussperrschutz aus.
	if _, err := env.Settings.ExpandIPAllowlists([]uint{99999}); err == nil || !strings.Contains(err.Error(), "99999") {
		t.Errorf("unbekannte id muss einen benennenden Fehler liefern, bekam %v", err)
	}
	// Leere Eingabe → nil.
	if ips, _ := env.Settings.ExpandIPAllowlists(nil); ips != nil {
		t.Errorf("leere eingabe sollte nil sein, bekam %v", ips)
	}
}

// TestDeleteIPAllowlist entfernt eine Liste.
func TestDeleteIPAllowlist(t *testing.T) {
	env := newTestEnv(t)
	l, _ := env.Settings.SaveIPAllowlist(domain.IPAllowlist{Name: "Weg", Entries: "10.0.0.1"}, "admin")
	if err := env.Settings.DeleteIPAllowlist(l.ID, "admin"); err != nil {
		t.Fatalf("löschen: %v", err)
	}
	if err := env.Settings.DeleteIPAllowlist(l.ID, "admin"); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("doppeltes löschen: erwartet ErrNotFound, bekam %v", err)
	}
}
