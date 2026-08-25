package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// NotificationRepository verwaltet die konfigurierten
// Benachrichtigungskanäle (Provider-Instanzen).
type NotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) Create(c *domain.NotificationChannel) error {
	return r.db.Create(c).Error
}

func (r *NotificationRepository) Update(c *domain.NotificationChannel) error {
	return r.db.Save(c).Error
}

func (r *NotificationRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.NotificationChannel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *NotificationRepository) FindByID(id uint) (*domain.NotificationChannel, error) {
	var c domain.NotificationChannel
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

func (r *NotificationRepository) FindAll() ([]domain.NotificationChannel, error) {
	var channels []domain.NotificationChannel
	if err := r.db.Order("name").Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

// FindByType liefert den ersten Kanal eines Typs - genutzt für den
// verwalteten System-E-Mail-Kanal (existiert höchstens einmal).
func (r *NotificationRepository) FindByType(channelType string) (*domain.NotificationChannel, error) {
	var c domain.NotificationChannel
	if err := r.db.Where("type = ?", channelType).First(&c).Error; err != nil {
		return nil, translate(err)
	}
	return &c, nil
}
