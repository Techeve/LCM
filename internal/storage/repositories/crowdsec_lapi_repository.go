package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// CrowdSecLapiRepository verwaltet die zentralen CrowdSec-LAPIs
// (Einstellungen → CrowdSec).
type CrowdSecLapiRepository struct {
	db *gorm.DB
}

func NewCrowdSecLapiRepository(db *gorm.DB) *CrowdSecLapiRepository {
	return &CrowdSecLapiRepository{db: db}
}

func (r *CrowdSecLapiRepository) List() ([]domain.CrowdSecLapi, error) {
	var lapis []domain.CrowdSecLapi
	if err := r.db.Order("name").Find(&lapis).Error; err != nil {
		return nil, err
	}
	return lapis, nil
}

func (r *CrowdSecLapiRepository) FindByID(id uint) (*domain.CrowdSecLapi, error) {
	var lapi domain.CrowdSecLapi
	if err := r.db.First(&lapi, id).Error; err != nil {
		return nil, translate(err)
	}
	return &lapi, nil
}

// FindByName liefert die LAPI mit diesem Namen (ErrNotFound, wenn keine).
func (r *CrowdSecLapiRepository) FindByName(name string) (*domain.CrowdSecLapi, error) {
	var lapi domain.CrowdSecLapi
	if err := r.db.Where("name = ?", name).First(&lapi).Error; err != nil {
		return nil, translate(err)
	}
	return &lapi, nil
}

// FindByURL liefert die LAPI mit dieser Adresse (ErrNotFound, wenn keine).
func (r *CrowdSecLapiRepository) FindByURL(url string) (*domain.CrowdSecLapi, error) {
	var lapi domain.CrowdSecLapi
	if err := r.db.Where("url = ?", url).First(&lapi).Error; err != nil {
		return nil, translate(err)
	}
	return &lapi, nil
}

func (r *CrowdSecLapiRepository) Count() (int64, error) {
	var n int64
	err := r.db.Model(&domain.CrowdSecLapi{}).Count(&n).Error
	return n, err
}

func (r *CrowdSecLapiRepository) Save(lapi *domain.CrowdSecLapi) error {
	return r.db.Save(lapi).Error
}

func (r *CrowdSecLapiRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.CrowdSecLapi{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
