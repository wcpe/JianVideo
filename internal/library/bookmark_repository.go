package library

import (
	"context"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

type bookmarkRepository interface {
	List(context.Context, string, int64) ([]models.MediaBookmark, error)
	CreateTx(context.Context, *gorm.DB, *models.MediaBookmark) error
	GetTx(context.Context, *gorm.DB, string, int64, string) (*models.MediaBookmark, error)
	UpdateCASTx(context.Context, *gorm.DB, *models.MediaBookmark, int64) (bool, error)
	DeleteCASTx(context.Context, *gorm.DB, string, int64, string, int64) (bool, error)
}

type gormBookmarkRepository struct {
	db *gorm.DB
}

func newGormBookmarkRepository(db *gorm.DB) bookmarkRepository {
	return &gormBookmarkRepository{db: db}
}

func (r *gormBookmarkRepository) List(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaBookmark, error) {
	var bookmarks []models.MediaBookmark
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Order("position_ms ASC, created_at ASC, id ASC").Find(&bookmarks).Error
	return bookmarks, err
}

func (r *gormBookmarkRepository) CreateTx(ctx context.Context, tx *gorm.DB, bookmark *models.MediaBookmark) error {
	return tx.WithContext(ctx).Create(bookmark).Error
}

func (r *gormBookmarkRepository) GetTx(ctx context.Context, tx *gorm.DB, spaceID string, mediaID int64, id string) (*models.MediaBookmark, error) {
	var bookmark models.MediaBookmark
	err := tx.WithContext(ctx).Where("space_id = ? AND media_id = ? AND id = ?", normalizeSpaceID(spaceID), mediaID, id).First(&bookmark).Error
	return &bookmark, err
}

func (r *gormBookmarkRepository) UpdateCASTx(ctx context.Context, tx *gorm.DB, bookmark *models.MediaBookmark, expectedRevision int64) (bool, error) {
	result := tx.WithContext(ctx).Model(&models.MediaBookmark{}).
		Where("space_id = ? AND media_id = ? AND id = ? AND revision = ?", bookmark.SpaceID, bookmark.MediaID, bookmark.ID, expectedRevision).
		Updates(map[string]any{
			"position_ms": bookmark.PositionMS, "title": bookmark.Title, "note": bookmark.Note,
			"revision": bookmark.Revision, "updated_at": bookmark.UpdatedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *gormBookmarkRepository) DeleteCASTx(ctx context.Context, tx *gorm.DB, spaceID string, mediaID int64, id string, revision int64) (bool, error) {
	result := tx.WithContext(ctx).Where("space_id = ? AND media_id = ? AND id = ? AND revision = ?", normalizeSpaceID(spaceID), mediaID, id, revision).
		Delete(&models.MediaBookmark{})
	return result.RowsAffected == 1, result.Error
}
