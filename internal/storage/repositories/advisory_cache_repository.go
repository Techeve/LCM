package repositories

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// AdvisoryCacheRepository verwaltet den lokalen Zwischenspeicher der
// Online-Abfragen: welche Advisories zu einem Paketstand gehören
// (AdvisoryCacheEntry) und wie ein Advisory beschrieben ist
// (AdvisoryDetail).
type AdvisoryCacheRepository struct {
	db *gorm.DB
}

func NewAdvisoryCacheRepository(db *gorm.DB) *AdvisoryCacheRepository {
	return &AdvisoryCacheRepository{db: db}
}

// FreshEntries liefert die Cache-Einträge zu den gefragten purls, die noch
// innerhalb der TTL liegen. ttlMinutes <= 0 schaltet den Cache ab - dann
// kommt nichts zurück und jeder purl wird neu abgefragt.
func (r *AdvisoryCacheRepository) FreshEntries(purls []string, now time.Time, ttlMinutes int) (map[string]domain.AdvisoryCacheEntry, error) {
	out := map[string]domain.AdvisoryCacheEntry{}
	if ttlMinutes <= 0 || len(purls) == 0 {
		return out, nil
	}
	cutoff := now.Add(-time.Duration(ttlMinutes) * time.Minute)
	// In Blöcken abfragen: SQLite begrenzt die Zahl der Parameter je
	// Anweisung, und die purl-Liste kann in einer größeren Flotte einige
	// tausend Einträge umfassen.
	for _, chunk := range chunkStrings(purls, 500) {
		var rows []domain.AdvisoryCacheEntry
		if err := r.db.Where("purl IN ? AND checked_at >= ?", chunk, cutoff).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.Purl] = row
		}
	}
	return out, nil
}

// PutEntries schreibt Abfrageergebnisse fort (Upsert je purl).
func (r *AdvisoryCacheRepository) PutEntries(entries []domain.AdvisoryCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "purl"}},
		DoUpdates: clause.AssignmentColumns([]string{"source", "advisory_ids", "checked_at"}),
	}).CreateInBatches(entries, 200).Error
}

// Details liefert die gespeicherten Beschreibungen zu den gefragten
// Advisory-Kennungen.
func (r *AdvisoryCacheRepository) Details(ids []string) (map[string]domain.AdvisoryDetail, error) {
	out := map[string]domain.AdvisoryDetail{}
	for _, chunk := range chunkStrings(ids, 500) {
		var rows []domain.AdvisoryDetail
		if err := r.db.Where("advisory_id IN ?", chunk).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.AdvisoryID] = row
		}
	}
	return out, nil
}

// AllDetails liefert alle gespeicherten Beschreibungen. Die Anreicherung
// braucht sie vollständig, weil sie über die Aliase abgleicht - welche
// Kennung zu welcher CVE gehört, lässt sich nicht vorab eingrenzen. Der
// Bestand ist klein: eine Zeile je jemals gesehenem Advisory.
func (r *AdvisoryCacheRepository) AllDetails() ([]domain.AdvisoryDetail, error) {
	var rows []domain.AdvisoryDetail
	err := r.db.Find(&rows).Error
	return rows, err
}

// PutDetails schreibt Beschreibungen fort (Upsert je Advisory-Kennung).
func (r *AdvisoryCacheRepository) PutDetails(details []domain.AdvisoryDetail) error {
	if len(details) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "advisory_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source", "kind", "severity", "title", "url", "fixed_versions", "aliases", "modified", "fetched_at",
		}),
	}).CreateInBatches(details, 200).Error
}

// PurgeEntriesOlderThan räumt Cache-Einträge weg, die niemand mehr abfragt -
// nach einem Distributions-Upgrade veralten die alten purls auf einen Schlag
// und würden sonst dauerhaft liegen bleiben. Die Beschreibungen (Details)
// bleiben: sie sind klein, unveränderlich und für den Verlauf nützlich.
func (r *AdvisoryCacheRepository) PurgeEntriesOlderThan(cutoff time.Time) (int64, error) {
	res := r.db.Where("checked_at < ?", cutoff).Delete(&domain.AdvisoryCacheEntry{})
	return res.RowsAffected, res.Error
}

// chunkStrings zerlegt eine Liste in Blöcke fester Größe.
func chunkStrings(in []string, size int) [][]string {
	if size <= 0 || len(in) == 0 {
		return nil
	}
	out := make([][]string, 0, (len(in)+size-1)/size)
	for start := 0; start < len(in); start += size {
		end := min(start+size, len(in))
		out = append(out, in[start:end])
	}
	return out
}

// --- Trefferquote --------------------------------------------------------------

// RecordRun schreibt das Ergebnis eines Durchgangs fort: wie viele purls aus
// dem Zwischenspeicher kamen und für wie viele nachgefragt werden musste.
func (r *AdvisoryCacheRepository) RecordRun(hits, misses int, now time.Time) error {
	var stats domain.AdvisoryCacheStats
	if err := r.db.First(&stats, 1).Error; err != nil {
		// Erster Durchgang: Zählung beginnt jetzt.
		stats = domain.AdvisoryCacheStats{ID: 1, SinceAt: now}
	}
	stats.Hits += int64(hits)
	stats.Misses += int64(misses)
	stats.Runs++
	stats.UpdatedAt = &now
	return r.db.Save(&stats).Error
}

// CacheSnapshot beschreibt den aktuellen Inhalt des Zwischenspeichers.
type CacheSnapshot struct {
	// Entries ist die Zahl gemerkter Paketstände, Fresh davon die innerhalb
	// der eingestellten Gültigkeit.
	Entries int64 `json:"entries"`
	Fresh   int64 `json:"fresh"`
	// Details ist die Zahl gespeicherter Advisory-Beschreibungen.
	Details int64 `json:"details"`
	// OldestAt ist der älteste Eintrag (Nullzeit = keiner vorhanden).
	OldestAt *time.Time `json:"oldest_at,omitempty"`
}

// Stats liefert Zähler und Momentaufnahme zusammen.
func (r *AdvisoryCacheRepository) Stats(now time.Time, ttlMinutes int) (*domain.AdvisoryCacheStats, *CacheSnapshot, error) {
	var stats domain.AdvisoryCacheStats
	if err := r.db.First(&stats, 1).Error; err != nil {
		stats = domain.AdvisoryCacheStats{} // noch nie gelaufen
	}

	var snap CacheSnapshot
	if err := r.db.Model(&domain.AdvisoryCacheEntry{}).Count(&snap.Entries).Error; err != nil {
		return nil, nil, err
	}
	if err := r.db.Model(&domain.AdvisoryDetail{}).Count(&snap.Details).Error; err != nil {
		return nil, nil, err
	}
	if ttlMinutes > 0 {
		cutoff := now.Add(-time.Duration(ttlMinutes) * time.Minute)
		if err := r.db.Model(&domain.AdvisoryCacheEntry{}).
			Where("checked_at >= ?", cutoff).Count(&snap.Fresh).Error; err != nil {
			return nil, nil, err
		}
	}
	var oldest domain.AdvisoryCacheEntry
	if err := r.db.Order("checked_at ASC").First(&oldest).Error; err == nil {
		t := oldest.CheckedAt
		snap.OldestAt = &t
	}
	return &stats, &snap, nil
}
