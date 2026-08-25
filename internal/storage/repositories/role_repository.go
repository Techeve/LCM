package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// RoleRepository verwaltet Rollen und Permissions.
type RoleRepository struct {
	db *gorm.DB
}

func NewRoleRepository(db *gorm.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

func (r *RoleRepository) Create(role *domain.Role) error {
	return r.db.Create(role).Error
}

func (r *RoleRepository) FindByName(name string) (*domain.Role, error) {
	var role domain.Role
	if err := r.db.Preload("Permissions").Where("name = ?", name).First(&role).Error; err != nil {
		return nil, translate(err)
	}
	return &role, nil
}

func (r *RoleRepository) FindAll() ([]domain.Role, error) {
	var roles []domain.Role
	if err := r.db.Preload("Permissions").Order("id").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// EnsurePermission legt eine Permission an, falls sie noch nicht existiert.
func (r *RoleRepository) EnsurePermission(code, description string) (*domain.Permission, error) {
	var perm domain.Permission
	err := r.db.Where("code = ?", code).First(&perm).Error
	if err == nil {
		return &perm, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	perm = domain.Permission{Code: code, Description: description}
	if err := r.db.Create(&perm).Error; err != nil {
		return nil, err
	}
	return &perm, nil
}

// SetPermissions ersetzt die Permission-Zuordnung einer Rolle vollständig.
func (r *RoleRepository) SetPermissions(role *domain.Role, perms []domain.Permission) error {
	return r.db.Model(role).Association("Permissions").Replace(perms)
}
