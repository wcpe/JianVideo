package library

import (
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// viewRepository 封装最近查看相关的媒体字段读写（FR2-070 续4）。
type viewRepository interface {
	// SetLastViewedAt 更新活跃且未软删媒体的 last_viewed_at；返回 rowsAffected。
	SetLastViewedAt(spaceID string, mediaID int64, at time.Time) (int64, error)
	// ListRecentlyViewed 按 last_viewed_at 倒序列出最近查看媒体。
	ListRecentlyViewed(spaceID string, limit int) ([]models.MediaFile, error)
}

type gormViewRepository struct {
	db *gorm.DB
}

func newGormViewRepository(db *gorm.DB) viewRepository {
	return &gormViewRepository{db: db}
}

func (r *gormViewRepository) SetLastViewedAt(spaceID string, mediaID int64, at time.Time) (int64, error) {
	result := r.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), mediaID).
		Update("last_viewed_at", at)
	return result.RowsAffected, result.Error
}

func (r *gormViewRepository) ListRecentlyViewed(spaceID string, limit int) ([]models.MediaFile, error) {
	var items []models.MediaFile
	err := r.db.
		Where("space_id = ? AND last_viewed_at IS NOT NULL AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Order("last_viewed_at DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}
