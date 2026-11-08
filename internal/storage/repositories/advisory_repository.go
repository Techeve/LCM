package repositories

import (
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// AdvisoryRepository verwaltet die Frühwarn-Befunde (AdvisoryFinding) samt
// ihrem Verlauf.
type AdvisoryRepository struct {
	db *gorm.DB
}

func NewAdvisoryRepository(db *gorm.DB) *AdvisoryRepository {
	return &AdvisoryRepository{db: db}
}

// ReconcileResult beschreibt, was ein Abgleich verändert hat.
type ReconcileResult struct {
	// New sind die Befunde, die es vorher gar nicht gab - nur sie sind
	// „neu" im Sinne der Frühwarnung.
	New []domain.AdvisoryFinding
	// Reopened sind zuvor behobene Befunde, die wieder auftauchen (z.B. nach
	// einem Downgrade oder einer zurückgerollten Version).
	Reopened []domain.AdvisoryFinding
	// Resolved ist die Zahl der Befunde, die nicht mehr auftauchen.
	Resolved int
}

// findingKey identifiziert einen Befund fachlich (unabhängig von der UUID) -
// dieselbe Kombination wie der Unique-Index.
type findingKey struct {
	source, advisoryID, packageName string
}

func keyOf(f *domain.AdvisoryFinding) findingKey {
	return findingKey{f.Source, f.AdvisoryID, f.PackageName}
}

// Reconcile gleicht die aktuell gemeldeten Befunde einer Quelle für einen
// Server mit dem gespeicherten Bestand ab.
//
// Der Abgleich ist bewusst KEIN Ersetzen: Neue Befunde bekommen FirstSeenAt
// (die Grundlage jeder Frühwarnung), verschwundene werden auf ResolvedAt
// gesetzt statt gelöscht, und ein wiederauftauchender Befund wird
// wiedereröffnet statt ein zweites Mal angelegt. Nur so bleibt „seit wann
// wissen wir davon" über die Zeit belastbar.
//
// Der Zeitpunkt kommt von außen (now), damit Tests ohne Uhr auskommen.
func (r *AdvisoryRepository) Reconcile(serverID uint, source string, found []domain.AdvisoryFinding, now time.Time) (ReconcileResult, error) {
	var res ReconcileResult
	ref := ServerRef(serverID)

	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing []domain.AdvisoryFinding
		if err := tx.Where("server_ref = ? AND source = ?", ref, source).Find(&existing).Error; err != nil {
			return err
		}
		byKey := make(map[findingKey]*domain.AdvisoryFinding, len(existing))
		for i := range existing {
			byKey[keyOf(&existing[i])] = &existing[i]
		}

		seen := make(map[findingKey]bool, len(found))
		for i := range found {
			f := found[i]
			f.ServerRef = ref
			f.Source = source
			key := keyOf(&f)
			seen[key] = true

			old, ok := byKey[key]
			if !ok {
				f.ID = "" // neue UUID vergibt BeforeCreate
				f.FirstSeenAt = now
				if err := tx.Create(&f).Error; err != nil {
					return err
				}
				res.New = append(res.New, f)
				continue
			}

			// Bestehender Befund: Beschreibung auffrischen (die Quelle
			// ergänzt Schwere und Fix-Version oft erst nachträglich) und
			// gegebenenfalls wiedereröffnen. FirstSeenAt bleibt unangetastet
			// - es ist der Anker der Frühwarnung.
			updates := map[string]any{
				"kind":              f.Kind,
				"installed_version": f.InstalledVersion,
				"fixed_version":     f.FixedVersion,
				"severity":          f.Severity,
				"title":             f.Title,
				"url":               f.URL,
			}
			wasResolved := old.ResolvedAt != nil
			if wasResolved {
				updates["resolved_at"] = nil
			}
			if err := tx.Model(&domain.AdvisoryFinding{}).Where("id = ?", old.ID).Updates(updates).Error; err != nil {
				return err
			}
			if wasResolved {
				reopened := *old
				reopened.ResolvedAt = nil
				res.Reopened = append(res.Reopened, reopened)
			}
		}

		// Alles, was nicht mehr gemeldet wird und noch offen ist, gilt als
		// behoben.
		var stale []string
		for key, old := range byKey {
			if seen[key] || old.ResolvedAt != nil {
				continue
			}
			stale = append(stale, old.ID)
		}
		if len(stale) > 0 {
			if err := tx.Model(&domain.AdvisoryFinding{}).Where("id IN ?", stale).
				Update("resolved_at", now).Error; err != nil {
				return err
			}
			res.Resolved = len(stale)
		}
		return nil
	})
	return res, err
}

// ActiveForServer liefert die offenen Befunde eines Servers.
func (r *AdvisoryRepository) ActiveForServer(serverID uint) ([]domain.AdvisoryFinding, error) {
	var out []domain.AdvisoryFinding
	err := r.db.Where("server_ref = ? AND resolved_at IS NULL", ServerRef(serverID)).
		Order("severity, package_name").Find(&out).Error
	return out, err
}

// AdvisoryRow ist ein Befund samt betroffenem Server für die globale Sicht.
type AdvisoryRow struct {
	ID               string     `json:"id"`
	ServerID         uint       `json:"server_id"`
	ServerName       string     `json:"server_name"`
	Source           string     `json:"source"`
	AdvisoryID       string     `json:"advisory_id"`
	Kind             string     `json:"kind"`
	PackageName      string     `json:"package_name"`
	InstalledVersion string     `json:"installed_version"`
	FixedVersion     string     `json:"fixed_version"`
	Severity         string     `json:"severity"`
	Title            string     `json:"title"`
	URL              string     `json:"url"`
	Exploited        bool       `json:"exploited"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	ResolvedAt       *time.Time `json:"resolved_at"`
	AcknowledgedBy   string     `json:"acknowledged_by"`
}

// AdvisoryPage ist eine Seite der Frühwarn-Befunde samt Gesamtzahl und
// Schwere-Verteilung über ALLE Treffer (nicht nur die Seite).
type AdvisoryPage struct {
	Items    []AdvisoryRow  `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Summary  map[string]int `json:"summary"`
}

// AdvisoryFilter grenzt die Befundliste ein.
type AdvisoryFilter struct {
	IncludeResolved bool
	// MinSeverity zeigt nur Befunde ab dieser Schwere (leer = alle).
	MinSeverity string
	Page        int
	PageSize    int
}

// advisorySeverityOrder sortiert per SQL nach Schwere. Schadpakete stehen
// ganz oben - ihre gespeicherte Schwere ist oft leer, ihre Dringlichkeit
// aber die höchste von allen (siehe AdvisoryFinding.EffectiveSeverity).
const advisorySeverityOrder = "CASE WHEN advisory_findings.kind = 'malware' THEN 0 ELSE" +
	" CASE advisory_findings.severity" +
	" WHEN 'critical' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 WHEN 'low' THEN 4 ELSE 5 END END"

// severityAtLeastSQL liefert die Schwere-Stufen ab der geforderten Schwelle.
// Die Auswahl passiert über eine Werteliste statt über eine Rangfunktion:
// So bleibt die Bedingung für jeden lesbar, der die Abfrage später anfasst.
func severityAtLeastSQL(min string) []string {
	order := []string{domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow}
	for i, s := range order {
		if s == min {
			return order[:i+1]
		}
	}
	return nil
}

// Global listet die Frühwarn-Befunde über die sichtbaren Server hinweg,
// seitenweise und kritischste zuerst.
func (r *AdvisoryRepository) Global(scope AccessScope, f AdvisoryFilter) (*AdvisoryPage, error) {
	base := func() *gorm.DB {
		q := scope.scopeServers(r.db.Table("advisory_findings").
			Joins("JOIN servers ON servers.ref = advisory_findings.server_ref"))
		if !f.IncludeResolved {
			q = q.Where("advisory_findings.resolved_at IS NULL")
		}
		if levels := severityAtLeastSQL(f.MinSeverity); levels != nil {
			// Schadpakete bleiben IMMER sichtbar: Ihre Schwere steht in den
			// Quellen meist gar nicht, sie über eine Schwelle wegzufiltern
			// hieße, den schlimmsten Fall auszublenden.
			q = q.Where("advisory_findings.kind = ? OR advisory_findings.severity IN ?",
				domain.AdvisoryKindMalware, levels)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}
	summary := map[string]int{}
	var sumRows []struct {
		Severity string
		Kind     string
		N        int
	}
	if err := base().Select("advisory_findings.severity AS severity, advisory_findings.kind AS kind, COUNT(*) AS n").
		Group("advisory_findings.severity, advisory_findings.kind").Scan(&sumRows).Error; err != nil {
		return nil, err
	}
	for _, s := range sumRows {
		sev := s.Severity
		if s.Kind == domain.AdvisoryKindMalware {
			sev = domain.SeverityCritical
		}
		if sev == "" {
			sev = domain.SeverityUnknown
		}
		summary[sev] += s.N
	}

	var rows []AdvisoryRow
	q := base().
		Select("advisory_findings.id, servers.id AS server_id, servers.name AS server_name," +
			" advisory_findings.source, advisory_findings.advisory_id, advisory_findings.kind," +
			" advisory_findings.package_name, advisory_findings.installed_version," +
			" advisory_findings.fixed_version, advisory_findings.severity, advisory_findings.title," +
			" advisory_findings.url, advisory_findings.exploited, advisory_findings.first_seen_at," +
			" advisory_findings.resolved_at, advisory_findings.acknowledged_by").
		// Erst nach Schwere, dann nach Alter: Das Neue ist bei einer
		// Frühwarnung interessant, das Kritische aber dringender.
		Order(advisorySeverityOrder).Order("advisory_findings.first_seen_at DESC").
		Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].ServerName = decryptField(rows[i].ServerName)
	}
	return &AdvisoryPage{Items: rows, Total: total, Page: f.Page, PageSize: f.PageSize, Summary: summary}, nil
}

// SetExploited setzt die Ausnutzungs-Markierung auf genau die angegebenen
// Advisory-Kennungen und nimmt sie überall sonst zurück.
//
// Der zweite Teil ist der wichtigere: Wird eine Lücke von der Quelle nicht
// mehr als ausgenutzt geführt, muss die Markierung verschwinden - sonst
// bliebe eine Dringlichkeit stehen, die niemand mehr belegen kann. Rückgabe
// ist die Zahl der jetzt markierten Befunde.
func (r *AdvisoryRepository) SetExploited(advisoryIDs []string) (int64, error) {
	var marked int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Erst alles zurücksetzen, dann die Treffer markieren. Der umgekehrte
		// Weg über ein NOT IN wäre eine Falle: Die Liste umfasst einige
		// tausend Kennungen, und SQLite begrenzt die Parameter je Anweisung.
		if err := tx.Model(&domain.AdvisoryFinding{}).
			Where("exploited = ?", true).Update("exploited", false).Error; err != nil {
			return err
		}
		for _, chunk := range chunkStrings(advisoryIDs, 500) {
			res := tx.Model(&domain.AdvisoryFinding{}).
				Where("advisory_id IN ?", chunk).Update("exploited", true)
			if res.Error != nil {
				return res.Error
			}
			marked += res.RowsAffected
		}
		return nil
	})
	return marked, err
}

// Acknowledge nimmt einen Befund zur Kenntnis - er löst danach keinen Alarm
// mehr aus, bleibt aber sichtbar.
func (r *AdvisoryRepository) Acknowledge(scope AccessScope, id, actor string, now time.Time) error {
	// Über den Scope prüfen, ob der Befund zu einem sichtbaren Server gehört
	// - sonst könnte ein eingeschränkter Benutzer fremde Befunde quittieren.
	var refs []string
	if err := scope.scopeServers(r.db.Table("advisory_findings").
		Select("advisory_findings.id").
		Joins("JOIN servers ON servers.ref = advisory_findings.server_ref")).
		Where("advisory_findings.id = ?", id).Scan(&refs).Error; err != nil {
		return err
	}
	if len(refs) == 0 {
		return ErrNotFound
	}
	return r.db.Model(&domain.AdvisoryFinding{}).Where("id = ?", id).
		Updates(map[string]any{"acknowledged_by": actor, "acknowledged_at": now}).Error
}
