package storage

import (
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestRecordStorageSample: mehrere Messungen desselben Tages werden zu EINER
// Zeile (Tagesdurchschnitt) verdichtet; ein neuer Tag legt eine neue Zeile an.
func TestRecordStorageSample(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(db)

	day := "2026-07-07"
	t0 := time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)
	// Zwei Messungen am selben Tag: 40000/10000 und 40000/12000 → Ø 11000.
	if err := repo.RecordStorageSample(1, day, 40000, 10000, t0); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordStorageSample(1, day, 40000, 12000, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	hist, err := repo.FindStorageHistory(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("erwartete 1 Tages-Zeile, bekam %d", len(hist))
	}
	if hist[0].Samples != 2 {
		t.Errorf("Samples = %d, erwartet 2", hist[0].Samples)
	}
	if hist[0].DiskUsedMB != 11000 {
		t.Errorf("Tagesdurchschnitt DiskUsedMB = %d, erwartet 11000", hist[0].DiskUsedMB)
	}

	// Neuer Tag → zweite Zeile.
	if err := repo.RecordStorageSample(1, "2026-07-08", 40000, 13000, t0.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	hist, _ = repo.FindStorageHistory(1)
	if len(hist) != 2 {
		t.Fatalf("erwartete 2 Tages-Zeilen, bekam %d", len(hist))
	}
	// FindStorageHistory liefert aufsteigend nach Tag.
	if hist[0].Day != "2026-07-07" || hist[1].Day != "2026-07-08" {
		t.Errorf("Reihenfolge falsch: %q, %q", hist[0].Day, hist[1].Day)
	}
}

// TestLatestStorageSampleAt liefert den jüngsten Messzeitpunkt (Grundlage der
// stündlichen Drosselung) bzw. die Nullzeit ohne Daten.
func TestLatestStorageSampleAt(t *testing.T) {
	db, _ := Open(":memory:")
	_ = Migrate(db)
	repo := repositories.NewServerRepository(db)

	if at, err := repo.LatestStorageSampleAt(1); err != nil || !at.IsZero() {
		t.Fatalf("ohne Daten: at=%v err=%v (Nullzeit erwartet)", at, err)
	}
	want := time.Date(2026, 7, 7, 9, 30, 0, 0, time.UTC)
	_ = repo.RecordStorageSample(1, "2026-07-07", 40000, 10000, want)
	got, err := repo.LatestStorageSampleAt(1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Errorf("LatestStorageSampleAt = %v, erwartet %v", got, want)
	}
}

// TestDeleteStorageHistoryOlderThan entfernt nur Tage vor dem Stichtag.
func TestDeleteStorageHistoryOlderThan(t *testing.T) {
	db, _ := Open(":memory:")
	_ = Migrate(db)
	repo := repositories.NewServerRepository(db)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, d := range []string{"2026-01-01", "2026-03-01", "2026-06-01"} {
		if err := repo.RecordStorageSample(1, d, 40000, 10000, base); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := repo.DeleteStorageHistoryOlderThan("2026-04-01")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Errorf("gelöscht = %d, erwartet 2", deleted)
	}
	hist, _ := repo.FindStorageHistory(1)
	if len(hist) != 1 || hist[0].Day != "2026-06-01" {
		t.Errorf("verbleibende Zeilen unerwartet: %+v", hist)
	}
}

// TestClampStorageHistoryRetention begrenzt auf 90-365 Tage.
func TestClampStorageHistoryRetention(t *testing.T) {
	cases := map[int]int{0: 90, 30: 90, 90: 90, 200: 200, 365: 365, 500: 365}
	for in, want := range cases {
		if got := domain.ClampStorageHistoryRetention(in); got != want {
			t.Errorf("Clamp(%d) = %d, erwartet %d", in, got, want)
		}
	}
}

// TestComputeForecast prüft die lineare Speicherprognose (Regression über den Verlauf).
func TestComputeForecast(t *testing.T) {
	// Zu wenig Daten → InsufficientData.
	f := domain.ComputeForecast(nil, 40000)
	if !f.InsufficientData {
		t.Error("kein Verlauf: InsufficientData erwartet")
	}
	f = domain.ComputeForecast(make([]domain.StorageHistory, 3), 40000)
	if !f.InsufficientData {
		t.Error("< 7 Datenpunkte: InsufficientData erwartet")
	}

	// Flacher Verlauf (kein Zuwachs) → Unlimited.
	flat := make([]domain.StorageHistory, 10)
	for i := range flat {
		flat[i] = domain.StorageHistory{DiskTotalMB: 40000, DiskUsedMB: 10000}
	}
	f = domain.ComputeForecast(flat, 40000)
	if !f.Unlimited {
		t.Errorf("kein Zuwachs: Unlimited erwartet, DaysRemaining=%d", f.DaysRemaining)
	}

	// Sehr langsames Wachstum → Horizont > 365 Tage → Unlimited.
	slow := make([]domain.StorageHistory, 10)
	for i := range slow {
		slow[i] = domain.StorageHistory{DiskTotalMB: 100000, DiskUsedMB: int64(10000 + i*1)}
	}
	f = domain.ComputeForecast(slow, 100000)
	if !f.Unlimited {
		t.Errorf("sehr langsames Wachstum: Unlimited erwartet, DaysRemaining=%d", f.DaysRemaining)
	}

	// Bekanntes Wachstum: 10 Datenpunkte, slope ≈ 1000 MB/Tag, 51000 MB verbleibend
	// (last = 40000+9*1000=49000, remaining = 100000-49000=51000) → ~51 Tage.
	fast := make([]domain.StorageHistory, 10)
	for i := range fast {
		fast[i] = domain.StorageHistory{DiskTotalMB: 100000, DiskUsedMB: int64(40000 + i*1000)}
	}
	f = domain.ComputeForecast(fast, 100000)
	if f.Unlimited || f.InsufficientData {
		t.Fatalf("bekanntes Wachstum: konkrete Prognose erwartet, unlimited=%v insufficientData=%v", f.Unlimited, f.InsufficientData)
	}
	if f.DaysRemaining < 47 || f.DaysRemaining > 55 {
		t.Errorf("DaysRemaining = %d, erwartet ~51", f.DaysRemaining)
	}

	// Demo-Szenario db01: 30 Punkte, 310 MB/Tag, ~11410 MB verbleibend → ~37 Tage.
	db01 := make([]domain.StorageHistory, 30)
	for i := range db01 {
		db01[i] = domain.StorageHistory{DiskTotalMB: 102400, DiskUsedMB: int64(82000 + i*310)}
	}
	f = domain.ComputeForecast(db01, 102400)
	if f.Unlimited || f.InsufficientData {
		t.Fatalf("db01-Szenario: konkrete Prognose erwartet")
	}
	if f.DaysRemaining < 30 || f.DaysRemaining > 45 {
		t.Errorf("db01 DaysRemaining = %d, erwartet ~37", f.DaysRemaining)
	}
}
