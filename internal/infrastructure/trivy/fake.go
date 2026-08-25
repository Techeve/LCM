package trivy

import (
	"context"
	"time"

	"LCM/internal/core/domain"
)

// Fake ist ein Scanner für Tests und den Fall „kein Trivy installiert".
// IsAvailable steuert Available(); ScanFunc/ScanImageFunc erlauben
// kanonische Funde je Ziel bzw. Image.
type Fake struct {
	IsAvailable   bool
	ScanFunc      func(target Target) ([]domain.Vulnerability, error)
	ScanImageFunc func(ref string) ([]domain.Vulnerability, error)
	// Calls/ImageCalls zählen die Aufrufe (für Assertions in Tests).
	Calls      int
	ImageCalls []string
	// InfoVal ist der gemeldete DB-Stand; ohne Setzung meldet der Fake eine
	// aktuelle Datenbank, damit bestehende Tests unverändert durchlaufen.
	InfoVal *domain.CVEDBStatus
	// UpdateDBFunc erlaubt es, einen fehlgeschlagenen Download zu simulieren.
	UpdateDBFunc func() (string, error)
	UpdateCalls  int
	// CacheStatsVal setzt den gemeldeten Zwischenspeicher-Zustand.
	CacheStatsVal *CacheStats
}

// Available meldet die konfigurierte Verfügbarkeit.
func (f *Fake) Available() bool { return f != nil && f.IsAvailable }

// Scan ruft ScanFunc (falls gesetzt), sonst liefert es keine Funde.
func (f *Fake) Scan(_ context.Context, target Target) ([]domain.Vulnerability, error) {
	f.Calls++
	if f.ScanFunc != nil {
		return f.ScanFunc(target)
	}
	return nil, nil
}

// ScanImage ruft ScanImageFunc (falls gesetzt), sonst keine Funde.
func (f *Fake) ScanImage(_ context.Context, ref string) ([]domain.Vulnerability, error) {
	f.ImageCalls = append(f.ImageCalls, ref)
	if f.ScanImageFunc != nil {
		return f.ScanImageFunc(ref)
	}
	return nil, nil
}

// Info liefert InfoVal, ersatzweise eine soeben gebaute Datenbank. Der
// Default ist bewusst „frisch": Ein Test, der sich fuer den DB-Stand nicht
// interessiert, soll nicht ungewollt einen Ueberalterungs-Befund ausloesen.
func (f *Fake) Info(_ context.Context) domain.CVEDBStatus {
	if f.InfoVal != nil {
		return *f.InfoVal
	}
	if !f.Available() {
		return domain.CVEDBStatus{Available: false, Freshness: domain.CVEDBUnknown}
	}
	now := time.Now()
	next := now.Add(24 * time.Hour)
	return domain.CVEDBStatus{
		Available: true, Version: "fake", UpdatedAt: &now, NextUpdate: &next,
		Freshness: domain.CVEDBFresh,
	}
}

// UpdateDB ruft UpdateDBFunc (falls gesetzt), sonst ein erfolgreicher No-op.
func (f *Fake) UpdateDB(_ context.Context) (string, error) {
	f.UpdateCalls++
	if f.UpdateDBFunc != nil {
		return f.UpdateDBFunc()
	}
	return "fake: datenbank aktualisiert", nil
}

// CacheStats liefert CacheStatsVal (Standard: leerer Zustand).
func (f *Fake) CacheStats() CacheStats {
	if f.CacheStatsVal != nil {
		return *f.CacheStatsVal
	}
	return CacheStats{}
}
