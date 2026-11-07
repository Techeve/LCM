package repositories_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// Bestehende Befunde lesen und danach ändern: Laufen solche Abgleiche
// gleichzeitig, darf keiner an „database is locked" scheitern. SQLite wartet
// beim Wechsel vom Lesen zum Schreiben nicht, deshalb nur eine echte Datei-DB.
func TestGleichzeitigeAbgleicheScheiternNichtAnDerSperre(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AdvisoryFinding{}); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewAdvisoryRepository(db)

	const servers = 8
	found := func(version string) []domain.AdvisoryFinding {
		list := make([]domain.AdvisoryFinding, 20)
		for i := range list {
			list[i] = domain.AdvisoryFinding{
				AdvisoryID: fmt.Sprintf("OSV-%d", i), Kind: "vulnerability",
				PackageName: "paket", InstalledVersion: version,
			}
		}
		return list
	}

	now := time.Now()
	for id := uint(1); id <= servers; id++ {
		if _, err := repo.Reconcile(id, "osv", found("1"), now); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, servers*10)
	for id := uint(1); id <= servers; id++ {
		wg.Add(1)
		go func(id uint) {
			defer wg.Done()
			for round := 0; round < 10; round++ {
				if _, err := repo.Reconcile(id, "osv", found(fmt.Sprint(round)), now); err != nil {
					errs <- err
				}
			}
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
