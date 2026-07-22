package library

import (
	"context"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// bookmarkRepository 封装书签读写与 CAS（FR2-070 续23：事务入口下沉）。
// 事务入口用 *gorm.DB；审计仍由 service 经 recordBookmarkAudit 写入。
type bookmarkRepository interface {
	// RunInTx 在事务中执行 fn（可选携带 context）。
	RunInTx(ctx context.Context, fn func(tx *gorm.DB) error) error
	// List 列出媒体书签（按 position/created/id）。
	List(context.Context, string, int64) ([]models.MediaBookmark, error)
	// Get 非事务读取单条书签。
	Get(ctx context.Context, spaceID string, mediaID int64, id string) (*models.MediaBookmark, error)
	// CreateTx 事务内创建书签。
	CreateTx(context.Context, *gorm.DB, *models.MediaBookmark) error
	// GetTx 事务内读取单条书签。
	GetTx(context.Context, *gorm.DB, string, int64, string) (*models.MediaBookmark, error)
	// UpdateCASTx 事务内 revision CAS 更新；matched 表示命中。
	UpdateCASTx(context.Context, *gorm.DB, *models.MediaBookmark, int64) (bool, error)
	// DeleteCASTx 事务内 revision CAS 删除；matched 表示命中。
	DeleteCASTx(context.Context, *gorm.DB, string, int64, string, int64) (bool, error)
}

type gormBookmarkRepository struct {
	db *gorm.DB
}

func newGormBookmarkRepository(db *gorm.DB) bookmarkRepository {
	return &gormBookmarkRepository{db: db}
}

func (r *gormBookmarkRepository) RunInTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

func (r *gormBookmarkRepository) List(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaBookmark, error) {
	var bookmarks []models.MediaBookmark
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Order("position_ms ASC, created_at ASC, id ASC").Find(&bookmarks).Error
	return bookmarks, err
}

func (r *gormBookmarkRepository) Get(ctx context.Context, spaceID string, mediaID int64, id string) (*models.MediaBookmark, error) {
	return r.GetTx(ctx, r.db, spaceID, mediaID, id)
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
