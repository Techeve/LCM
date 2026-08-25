package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// ProfileBlockRepository verwaltet die Regelbausteine samt ihrer Varianten.
type ProfileBlockRepository struct {
	db *gorm.DB
}

func NewProfileBlockRepository(db *gorm.DB) *ProfileBlockRepository {
	return &ProfileBlockRepository{db: db}
}

func (r *ProfileBlockRepository) FindAll() ([]domain.ProfileBlock, error) {
	var blocks []domain.ProfileBlock
	// Mitgelieferte zuerst - sie sind der Bezugspunkt für eigene.
	err := r.db.Preload("Variants", orderByID).Order("builtin DESC, name").Find(&blocks).Error
	return blocks, err
}

func (r *ProfileBlockRepository) FindByID(id uint) (*domain.ProfileBlock, error) {
	var block domain.ProfileBlock
	if err := r.db.Preload("Variants", orderByID).First(&block, id).Error; err != nil {
		return nil, translate(err)
	}
	return &block, nil
}

func (r *ProfileBlockRepository) FindBySlug(slug string) (*domain.ProfileBlock, error) {
	var block domain.ProfileBlock
	if err := r.db.Preload("Variants", orderByID).Where("slug = ?", slug).First(&block).Error; err != nil {
		return nil, translate(err)
	}
	return &block, nil
}

func (r *ProfileBlockRepository) Create(b *domain.ProfileBlock) error {
	return r.db.Create(b).Error
}

// Update ersetzt Kopfdaten und Varianten - ein Baustein beschreibt einen
// Soll-Zustand, eine entfernte Variante darf nicht zurückbleiben.
func (r *ProfileBlockRepository) Update(b *domain.ProfileBlock) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.ProfileBlock{ID: b.ID}).
			Updates(map[string]any{
				"name": b.Name, "description": b.Description, "params": b.Params,
				"name_en": b.NameEN, "description_en": b.DescriptionEN,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("block_id = ?", b.ID).Delete(&domain.ProfileBlockVariant{}).Error; err != nil {
			return err
		}
		for i := range b.Variants {
			b.Variants[i].ID, b.Variants[i].BlockID = 0, b.ID
		}
		if len(b.Variants) == 0 {
			return nil
		}
		return tx.Create(&b.Variants).Error
	})
}

func (r *ProfileBlockRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("block_id = ?", id).Delete(&domain.ProfileBlockVariant{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&domain.ProfileBlock{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// UsageOf zählt, wie viele Profile einen Baustein verwenden. Ein Baustein zu
// ändern verändert Rechte auf allen Servern, die ihn über irgendein Profil
// nutzen - das muss vor dem Speichern dastehen.
func (r *ProfileBlockRepository) UsageOf(blockID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.ProfileBlockUse{}).Where("block_id = ?", blockID).Count(&n).Error
	return n, err
}

// ProfileNamesUsing liefert die Namen der Profile, die den Baustein nutzen.
func (r *ProfileBlockRepository) ProfileNamesUsing(blockID uint) ([]string, error) {
	var names []string
	err := r.db.Model(&domain.PrivilegeProfile{}).
		Where("id IN (SELECT profile_id FROM profile_block_uses WHERE block_id = ?)", blockID).
		Order("name").Pluck("name", &names).Error
	return names, err
}
