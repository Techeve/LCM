package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// IPAllowlistRepository verwaltet die benannten IP-Allowlists
// (Einstellungen → Allowlists).
type IPAllowlistRepository struct {
	db *gorm.DB
}

func NewIPAllowlistRepository(db *gorm.DB) *IPAllowlistRepository {
	return &IPAllowlistRepository{db: db}
}

func (r *IPAllowlistRepository) List() ([]domain.IPAllowlist, error) {
	var lists []domain.IPAllowlist
	if err := r.db.Order("name").Find(&lists).Error; err != nil {
		return nil, err
	}
	return lists, nil
}

func (r *IPAllowlistRepository) FindByID(id uint) (*domain.IPAllowlist, error) {
	var list domain.IPAllowlist
	if err := r.db.First(&list, id).Error; err != nil {
		return nil, translate(err)
	}
	return &list, nil
}

func (r *IPAllowlistRepository) FindByName(name string) (*domain.IPAllowlist, error) {
	var list domain.IPAllowlist
	if err := r.db.Where("name = ?", name).First(&list).Error; err != nil {
		return nil, translate(err)
	}
	return &list, nil
}

// FindByIDs lädt mehrere Listen auf einmal (für die Auflösung referenzierter
// Allowlists). Nicht gefundene IDs werden schlicht ausgelassen.
func (r *IPAllowlistRepository) FindByIDs(ids []uint) ([]domain.IPAllowlist, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var lists []domain.IPAllowlist
	if err := r.db.Where("id IN ?", ids).Find(&lists).Error; err != nil {
		return nil, err
	}
	return lists, nil
}

func (r *IPAllowlistRepository) Create(list *domain.IPAllowlist) error {
	return r.db.Create(list).Error
}

func (r *IPAllowlistRepository) Update(list *domain.IPAllowlist) error {
	return r.db.Save(list).Error
}

func (r *IPAllowlistRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.IPAllowlist{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
