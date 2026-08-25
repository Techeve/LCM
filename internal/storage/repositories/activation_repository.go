package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// ActivationRepository verwaltet die zeitlich begrenzten Aktivierungslinks.
type ActivationRepository struct {
	db *gorm.DB
}

func NewActivationRepository(db *gorm.DB) *ActivationRepository {
	return &ActivationRepository{db: db}
}

func (r *ActivationRepository) Create(link *domain.ActivationLink) error {
	return r.db.Create(link).Error
}

// FindByTokenHash liefert einen Link anhand des Token-Hashes.
func (r *ActivationRepository) FindByTokenHash(hash string) (*domain.ActivationLink, error) {
	var link domain.ActivationLink
	if err := r.db.Where("token_hash = ?", hash).First(&link).Error; err != nil {
		return nil, translate(err)
	}
	return &link, nil
}

// MarkConsumed setzt den Verbrauchszeitpunkt.
func (r *ActivationRepository) MarkConsumed(link *domain.ActivationLink) error {
	return r.db.Save(link).Error
}
