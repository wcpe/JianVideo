package library

import (
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// tagRepository 封装收藏标记与标签/映射的持久化（FR2-070 续3）。
type tagRepository interface {
	// UpdateFavorite 更新收藏标记；返回 rowsAffected。
	UpdateFavorite(spaceID string, mediaID int64, favorite bool) (int64, error)
	// CountMedia 统计指定 Space 下媒体是否存在。
	CountMedia(spaceID string, mediaID int64) (int64, error)
	// FirstOrCreateTag 按 Space+名称查找或创建标签。
	FirstOrCreateTag(tag *models.Tag) error
	// ListTags 列出 Space 内全部标签（按名升序）。
	ListTags(spaceID string) ([]models.Tag, error)
	// ListMediaTags 列出媒体绑定的标签（按名升序）。
	ListMediaTags(mediaID int64) ([]models.Tag, error)
	// FirstOrCreateMapping 幂等写入标签映射。
	FirstOrCreateMapping(mapping *models.TagMapping) error
	// DeleteMapping 删除标签映射（不存在视为成功）。
	DeleteMapping(mediaID, tagID int64) error
	// CountTag 统计标签是否存在。
	CountTag(tagID int64) (int64, error)
}

type gormTagRepository struct {
	db *gorm.DB
}

func newGormTagRepository(db *gorm.DB) tagRepository {
	return &gormTagRepository{db: db}
}

func (r *gormTagRepository) UpdateFavorite(spaceID string, mediaID int64, favorite bool) (int64, error) {
	result := r.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), mediaID).
		Update("favorite", favorite)
	return result.RowsAffected, result.Error
}

func (r *gormTagRepository) CountMedia(spaceID string, mediaID int64) (int64, error) {
	var count int64
	err := r.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), mediaID).
		Count(&count).Error
	return count, err
}

func (r *gormTagRepository) FirstOrCreateTag(tag *models.Tag) error {
	return r.db.Where(models.Tag{SpaceID: tag.SpaceID, Name: tag.Name}).FirstOrCreate(tag).Error
}

func (r *gormTagRepository) ListTags(spaceID string) ([]models.Tag, error) {
	var tags []models.Tag
	err := r.db.Where("space_id = ?", normalizeSpaceID(spaceID)).Order("name ASC").Find(&tags).Error
	return tags, err
}

func (r *gormTagRepository) ListMediaTags(mediaID int64) ([]models.Tag, error) {
	var tags []models.Tag
	err := r.db.
		Joins("JOIN tag_mappings ON tag_mappings.tag_id = tags.id").
		Where("tag_mappings.media_id = ?", mediaID).
		Order("tags.name ASC").
		Find(&tags).Error
	return tags, err
}

func (r *gormTagRepository) FirstOrCreateMapping(mapping *models.TagMapping) error {
	return r.db.Where(models.TagMapping{TagID: mapping.TagID, MediaID: mapping.MediaID}).
		FirstOrCreate(mapping).Error
}

func (r *gormTagRepository) DeleteMapping(mediaID, tagID int64) error {
	return r.db.Where("media_id = ? AND tag_id = ?", mediaID, tagID).
		Delete(&models.TagMapping{}).Error
}

func (r *gormTagRepository) CountTag(tagID int64) (int64, error) {
	var count int64
	err := r.db.Model(&models.Tag{}).Where("id = ?", tagID).Count(&count).Error
	return count, err
}
