package repositories_test

import (
	"errors"
	"testing"

	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// TestServerLookupsReportNotFound: Ein fehlender Server oder Deep-Scan-Lauf
// ist ErrNotFound - der rohe gorm-Fehler wurde im Controller zum 500.
func TestServerLookupsReportNotFound(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(db)

	if _, err := repo.FindDeepScanReport(4711, "fehlt"); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("FindDeepScanReport: %v, erwartet ErrNotFound", err)
	}
	if _, err := repo.NothingPending(4711); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("NothingPending: %v, erwartet ErrNotFound", err)
	}
}
