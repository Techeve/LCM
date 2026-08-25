package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// CustomActionRepository verwaltet die benutzerdefinierten Aktionen
// (wiederverwendbare Command-Listen für Gruppen-Rules).
type CustomActionRepository struct {
	db *gorm.DB
}

func NewCustomActionRepository(db *gorm.DB) *CustomActionRepository {
	return &CustomActionRepository{db: db}
}

func (r *CustomActionRepository) Create(a *domain.CustomAction) error {
	return r.db.Create(a).Error
}

func (r *CustomActionRepository) Update(a *domain.CustomAction) error {
	return r.db.Save(a).Error
}

func (r *CustomActionRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.CustomAction{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *CustomActionRepository) FindByID(id uint) (*domain.CustomAction, error) {
	var a domain.CustomAction
	if err := r.db.First(&a, id).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}

func (r *CustomActionRepository) FindAll() ([]domain.CustomAction, error) {
	var actions []domain.CustomAction
	if err := r.db.Order("name").Find(&actions).Error; err != nil {
		return nil, err
	}
	return actions, nil
}

// CountRulesUsing zählt Gruppen-Rules, die eine bestimmte Custom-Aktion
// referenzieren (Command trägt die Action-ID) - für den Löschschutz.
func (r *CustomActionRepository) CountRulesUsing(id uint, actionIDStr string) (int64, error) {
	var n int64
	err := r.db.Model(&domain.Rule{}).
		Where("type = ? AND command = ?", domain.RuleTypeCustom, actionIDStr).
		Count(&n).Error
	return n, err
}
