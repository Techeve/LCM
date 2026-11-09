package repositories

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// capabilityChunk begrenzt die Server-IDs je Abfrage: SQLite kennt eine
// Obergrenze für Variablen je Anweisung.
const capabilityChunk = 500

// CapabilityRepository verwaltet die Betriebsfähigkeit der Server (eine Zeile
// je Server und Kategorie).
type CapabilityRepository struct {
	db *gorm.DB
}

func NewCapabilityRepository(db *gorm.DB) *CapabilityRepository {
	return &CapabilityRepository{db: db}
}

// FindForServer liefert alle Kategorien eines Servers.
func (r *CapabilityRepository) FindForServer(serverID uint) ([]domain.ServerCapability, error) {
	var rows []domain.ServerCapability
	if err := r.db.Where("server_id = ?", serverID).Order("category").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindForServers liefert die Kategorien vieler Server mit einer Abfrage je
// Block - das Dashboard fragt alle Server auf einmal, nicht einen nach dem
// anderen.
func (r *CapabilityRepository) FindForServers(ids []uint) (map[uint][]domain.ServerCapability, error) {
	out := make(map[uint][]domain.ServerCapability, len(ids))
	for start := 0; start < len(ids); start += capabilityChunk {
		chunk := ids[start:min(start+capabilityChunk, len(ids))]
		var rows []domain.ServerCapability
		if err := r.db.Where("server_id IN ?", chunk).Order("category").Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.ServerID] = append(out[row.ServerID], row)
		}
	}
	return out, nil
}

// Find liefert eine Kategorie eines Servers; nil, wenn es sie noch nicht gibt
// (noch kein Job dieser Art gelaufen).
func (r *CapabilityRepository) Find(serverID uint, category string) (*domain.ServerCapability, error) {
	var c domain.ServerCapability
	err := r.db.Where("server_id = ? AND category = ?", serverID, category).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Save legt die Zeile an oder überschreibt die bestehende desselben (Server,
// Kategorie). Schlüssel ist dieses Paar, nicht die ID: Zwei Jobs eines
// Servers, die gleichzeitig eine neue Kategorie anlegen, erzeugten sonst zwei
// Zeilen, und der Unique-Index ließe den zweiten scheitern.
//
// Die ID wird bewusst nicht geschrieben (und CreatedAt bleibt beim Update
// stehen); c wird nicht aktualisiert.
func (r *CapabilityRepository) Save(c *domain.ServerCapability) error {
	return r.db.Omit("id").Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "server_id"}, {Name: "category"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"updated_at", "state", "failing_since", "consecutive_failures", "last_success_at",
			"last_error_class", "last_error_message", "last_error_params",
			"last_job_id", "last_job_type", "last_job_name", "last_rule_id",
			"warning_class", "warning_message",
		}),
	}).Create(c).Error
}
