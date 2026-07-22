package library

import (
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// spaceRepository 封装 Space 基础查询（FR2-070 续17）。
type spaceRepository interface {
	// HasTable 是否存在 spaces 表。
	HasTable() bool
	// Exists 判断 Space 是否存在。
	Exists(spaceID string) (bool, error)
	// GetByID 按 id 取 Space。
	GetByID(spaceID string) (*models.Space, error)
}

type gormSpaceRepository struct {
	db *gorm.DB
}

func newGormSpaceRepository(db *gorm.DB) spaceRepository {
	return &gormSpaceRepository{db: db}
}

func (r *gormSpaceRepository) HasTable() bool {
	return r.db.Migrator().HasTable(&models.Space{})
}

func (r *gormSpaceRepository) Exists(spaceID string) (bool, error) {
	if !r.HasTable() {
		return false, nil
	}
	var count int64
	if err := r.db.Model(&models.Space{}).Where("id = ?", normalizeSpaceID(spaceID)).Count(&count).Error; err != nil {
		return false, err
	}
	return count == 1, nil
}

func (r *gormSpaceRepository) GetByID(spaceID string) (*models.Space, error) {
	var space models.Space
	if err := r.db.Where("id = ?", normalizeSpaceID(spaceID)).First(&space).Error; err != nil {
		return nil, err
	}
	return &space, nil
}
