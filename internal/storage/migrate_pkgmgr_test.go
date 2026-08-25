package storage

import (
	"testing"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/migrations"
)

// runMigrationByName sucht eine registrierte Migration und führt sie aus.
func runMigrationByName(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	for _, m := range migrations.Registry {
		if m.Name == name {
			if err := m.Run(db); err != nil {
				t.Fatalf("migration %s: %v", name, err)
			}
			return
		}
	}
	t.Fatalf("migration %q nicht in der registry", name)
}

// TestBackfillPackageManager: die v0.3.0-Migration leitet die Paketverwaltung
// aus der Distribution ab und lässt bereits gesetzte/unbekannte Werte in Ruhe.
func TestBackfillPackageManager(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	seed := []domain.Server{
		{Name: "u", Host: "h1", ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e", OSID: "ubuntu"},
		{Name: "d", Host: "h2", ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e", OSName: "Debian GNU/Linux"},
		{Name: "r", Host: "h3", ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e", OSID: "rocky"},
		{Name: "s", Host: "h4", ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e", OSID: "opensuse-leap"},
		{Name: "x", Host: "h5", ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e", OSID: "arch"},
		{Name: "keep", Host: "h6", ServiceUser: "s", HostKeyFingerprint: "f", PrivateKeyEnc: "e", OSID: "ubuntu", PackageManager: "zypper"},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	runMigrationByName(t, db, "0.3.0-backfill-package-manager")

	want := map[string]string{"u": "apt", "d": "apt", "r": "dnf", "s": "zypper", "x": "", "keep": "zypper"}
	for name, exp := range want {
		var srv domain.Server
		if err := db.Where("name = ?", name).First(&srv).Error; err != nil {
			t.Fatal(err)
		}
		if srv.PackageManager != exp {
			t.Errorf("server %q: package_manager = %q, erwartet %q", name, srv.PackageManager, exp)
		}
	}

	// Idempotent: ein zweiter Lauf ändert nichts mehr.
	runMigrationByName(t, db, "0.3.0-backfill-package-manager")
	var u domain.Server
	db.Where("name = ?", "u").First(&u)
	if u.PackageManager != "apt" {
		t.Errorf("zweiter lauf hat wert verändert: %q", u.PackageManager)
	}
}
