package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// AptCacheRepository verwaltet die zentralen APT-Caches
// (Einstellungen → APT-Cache).
type AptCacheRepository struct {
	db *gorm.DB
}

func NewAptCacheRepository(db *gorm.DB) *AptCacheRepository {
	return &AptCacheRepository{db: db}
}

func (r *AptCacheRepository) List() ([]domain.AptCache, error) {
	var caches []domain.AptCache
	if err := r.db.Order("name").Find(&caches).Error; err != nil {
		return nil, err
	}
	return caches, nil
}

func (r *AptCacheRepository) FindByID(id uint) (*domain.AptCache, error) {
	var cache domain.AptCache
	if err := r.db.First(&cache, id).Error; err != nil {
		return nil, translate(err)
	}
	return &cache, nil
}

// FindByName liefert den Cache mit diesem Namen (ErrNotFound, wenn keiner).
func (r *AptCacheRepository) FindByName(name string) (*domain.AptCache, error) {
	var cache domain.AptCache
	if err := r.db.Where("name = ?", name).First(&cache).Error; err != nil {
		return nil, translate(err)
	}
	return &cache, nil
}

// FindByURL liefert den Cache mit dieser Adresse (ErrNotFound, wenn keiner).
func (r *AptCacheRepository) FindByURL(url string) (*domain.AptCache, error) {
	var cache domain.AptCache
	if err := r.db.Where("url = ?", url).First(&cache).Error; err != nil {
		return nil, translate(err)
	}
	return &cache, nil
}

func (r *AptCacheRepository) Save(cache *domain.AptCache) error {
	return r.db.Save(cache).Error
}

func (r *AptCacheRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.AptCache{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
