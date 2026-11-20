package repositories

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// PortScanRepository hält den letzten Portscan je Server.
type PortScanRepository struct {
	db *gorm.DB
}

func NewPortScanRepository(db *gorm.DB) *PortScanRepository {
	return &PortScanRepository{db: db}
}

// Save legt den Lauf ab und ersetzt dabei den vorigen desselben Servers.
func (r *PortScanRepository) Save(scan *domain.PortScan) error {
	return r.db.Omit("id").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "server_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"scanned_at", "job_id", "result"}),
	}).Create(scan).Error
}

// Latest liefert den letzten Lauf eines Servers; ErrNotFound, wenn er noch
// nie gescannt wurde. Den Zugriff auf den Server prüft der Aufrufer.
func (r *PortScanRepository) Latest(serverID uint) (*domain.PortScan, error) {
	var scan domain.PortScan
	if err := r.db.Where("server_id = ?", serverID).First(&scan).Error; err != nil {
		return nil, translate(err)
	}
	return &scan, nil
}
