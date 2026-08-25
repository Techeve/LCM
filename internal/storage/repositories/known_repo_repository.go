package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// KnownRepoRepository verwaltet den pflegbaren Katalog bekannter
// Paketquellen (Einstellungen → Repositories).
type KnownRepoRepository struct {
	db *gorm.DB
}

func NewKnownRepoRepository(db *gorm.DB) *KnownRepoRepository {
	return &KnownRepoRepository{db: db}
}

func (r *KnownRepoRepository) List() ([]domain.KnownRepo, error) {
	var repos []domain.KnownRepo
	if err := r.db.Order("name").Find(&repos).Error; err != nil {
		return nil, err
	}
	return repos, nil
}

func (r *KnownRepoRepository) FindByID(id uint) (*domain.KnownRepo, error) {
	var repo domain.KnownRepo
	if err := r.db.First(&repo, id).Error; err != nil {
		return nil, translate(err)
	}
	return &repo, nil
}

func (r *KnownRepoRepository) FindByKey(key string) (*domain.KnownRepo, error) {
	var repo domain.KnownRepo
	if err := r.db.Where("key = ?", key).First(&repo).Error; err != nil {
		return nil, translate(err)
	}
	return &repo, nil
}

func (r *KnownRepoRepository) Create(repo *domain.KnownRepo) error {
	return r.db.Create(repo).Error
}

func (r *KnownRepoRepository) Update(repo *domain.KnownRepo) error {
	return r.db.Save(repo).Error
}

func (r *KnownRepoRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.KnownRepo{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
