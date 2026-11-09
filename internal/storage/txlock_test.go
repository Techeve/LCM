package storage

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Lesen und danach Schreiben in einer Transaktion ist das übliche Muster der
// Repositories. Mit einer verzögerten Transaktion scheitert es in SQLite
// sofort, sobald ein anderer schreibt („database is locked"), weil busy_timeout
// beim Wechsel zum Schreiben nicht greift. Alle Transaktionen müssen deshalb
// die Schreibsperre gleich zu Beginn holen und geordnet darauf warten.
func TestParalleleTransaktionenWartenAufDieSperre(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lcm.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE zaehler (n INTEGER)").Error; err != nil {
		t.Fatal(err)
	}

	const writers = 8
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- db.Transaction(func(tx *gorm.DB) error {
				var n int64
				if err := tx.Raw("SELECT COUNT(*) FROM zaehler").Scan(&n).Error; err != nil {
					return err
				}
				time.Sleep(20 * time.Millisecond)
				return tx.Exec("INSERT INTO zaehler (n) VALUES (?)", n).Error
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("transaktion gescheitert: %v", err)
		}
	}

	var rows int64
	if err := db.Raw("SELECT COUNT(*) FROM zaehler").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != writers {
		t.Errorf("zeilen = %d, erwartet %d", rows, writers)
	}
}
