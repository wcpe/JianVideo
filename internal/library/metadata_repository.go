package library

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

type metadataRepository interface {
	Upsert(context.Context, *models.MediaMetadata, map[string]any) error
	List(context.Context, string, int64) ([]models.MediaMetadata, error)
	MarkStale(context.Context, string, int64) error
	CountMedia(context.Context, string, int64) (int64, error)
	CountMediaThroughID(context.Context, string, int64, int64) (int64, error)
	NextMediaBatch(context.Context, string, int64, int64, int) ([]models.MediaFile, error)
}

type gormMetadataRepository struct {
	db *gorm.DB
}

func newGormMetadataRepository(db *gorm.DB) metadataRepository {
	return &gormMetadataRepository{db: db}
}

func (r *gormMetadataRepository) Upsert(ctx context.Context, row *models.MediaMetadata, mediaUpdates map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conflict := clause.OnConflict{
			Columns:   []clause.Column{{Name: "space_id"}, {Name: "media_id"}, {Name: "source"}},
			DoUpdates: clause.AssignmentColumns([]string{"tool", "tool_version", "raw_json", "normalized_json", "parsed_at", "stale"}),
		}
		if err := tx.Clauses(conflict).Create(row).Error; err != nil {
			return err
		}
		if len(mediaUpdates) == 0 {
			return nil
		}
		return tx.Model(&models.MediaFile{}).
			Where("space_id = ? AND id = ?", row.SpaceID, row.MediaID).
			Updates(mediaUpdates).Error
	})
}

func (r *gormMetadataRepository) List(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaMetadata, error) {
	var items []models.MediaMetadata
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Order("source ASC").Find(&items).Error
	return items, err
}

func (r *gormMetadataRepository) MarkStale(ctx context.Context, spaceID string, mediaID int64) error {
	return r.db.WithContext(ctx).Model(&models.MediaMetadata{}).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Update("stale", true).Error
}

func (r *gormMetadataRepository) CountMedia(ctx context.Context, spaceID string, libraryID int64) (int64, error) {
	query := metadataMediaQuery(r.db.WithContext(ctx), spaceID, libraryID)
	var count int64
	err := query.Count(&count).Error
	return count, err
}

func (r *gormMetadataRepository) CountMediaThroughID(ctx context.Context, spaceID string, libraryID, lastID int64) (int64, error) {
	if lastID <= 0 {
		return 0, nil
	}
	var count int64
	err := metadataMediaQuery(r.db.WithContext(ctx), spaceID, libraryID).Where("id <= ?", lastID).Count(&count).Error
	return count, err
}

func (r *gormMetadataRepository) NextMediaBatch(ctx context.Context, spaceID string, libraryID, lastID int64, limit int) ([]models.MediaFile, error) {
	var rows []models.MediaFile
	err := metadataMediaQuery(r.db.WithContext(ctx), spaceID, libraryID).
		Where("id > ?", lastID).Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func metadataMediaQuery(db *gorm.DB, spaceID string, libraryID int64) *gorm.DB {
	query := db.Model(&models.MediaFile{}).
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID))
	if libraryID > 0 {
		query = query.Where("library_id = ?", libraryID)
	}
	return query
}
