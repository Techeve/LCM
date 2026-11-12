package repositories_test

import (
	"os"
	"path/filepath"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// TestSnapshotIntoFuelltVorhandeneLeereDatei: Der Backup-Lauf legt die Datei
// vorher selbst mit 0600 an. Die Momentaufnahme muss genau diese Datei füllen
// (Rechte bleiben) und den Datenstand vollständig enthalten.
func TestSnapshotIntoFuelltVorhandeneLeereDatei(t *testing.T) {
	db := migrierteDB(t)
	if err := repositories.NewAptCacheRepository(db).Save(&domain.AptCache{Name: "lager", URL: "http://10.0.0.5:3142"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snap.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := repositories.NewDatabaseRepository(db).SnapshotInto(path); err != nil {
		t.Fatalf("SnapshotInto: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("Rechte %o, erwartet 0600", perm)
	}
	snap, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := snap.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	copied, err := repositories.NewAptCacheRepository(snap).FindByName("lager")
	if err != nil {
		t.Fatalf("Eintrag fehlt in der Momentaufnahme: %v", err)
	}
	if copied.URL != "http://10.0.0.5:3142" {
		t.Errorf("URL %q in der Momentaufnahme", copied.URL)
	}
}
