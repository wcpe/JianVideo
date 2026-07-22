package library

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// albumRepository 封装相册与成员关联的持久化（FR2-070 续3）。
type albumRepository interface {
	Create(album *models.Album) error
	ListWithCount(spaceID string) ([]AlbumWithCount, error)
	GetByID(spaceID string, id int64) (*models.Album, error)
	// DeleteCascade 删除相册及成员关联；相册不存在返回错误。
	DeleteCascade(spaceID string, id int64) error
	// FirstOrCreateItem 幂等写入相册成员。
	FirstOrCreateItem(item *models.AlbumItem) error
	DeleteItem(spaceID string, albumID, mediaID int64) error
	CountItem(spaceID string, albumID, mediaID int64) (int64, error)
	ListItemMediaIDs(spaceID string, albumID int64) ([]int64, error)
	// ListMediaByIDs 按 ID 批量取未软删媒体（不保证顺序）。
	ListMediaByIDs(spaceID string, mediaIDs []int64) ([]models.MediaFile, error)
}

type gormAlbumRepository struct {
	db *gorm.DB
}

func newGormAlbumRepository(db *gorm.DB) albumRepository {
	return &gormAlbumRepository{db: db}
}

func (r *gormAlbumRepository) Create(album *models.Album) error {
	return r.db.Create(album).Error
}

func (r *gormAlbumRepository) ListWithCount(spaceID string) ([]AlbumWithCount, error) {
	spaceID = normalizeSpaceID(spaceID)
	var result []AlbumWithCount
	err := r.db.Table("albums").
		Select("albums.*, COUNT(album_items.id) AS item_count").
		Joins("LEFT JOIN album_items ON album_items.space_id = albums.space_id AND album_items.album_id = albums.id").
		Where("albums.space_id = ?", spaceID).
		Group("albums.id").
		Order("albums.created_at DESC").
		Scan(&result).Error
	return result, err
}

func (r *gormAlbumRepository) GetByID(spaceID string, id int64) (*models.Album, error) {
	var album models.Album
	if err := r.db.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).First(&album).Error; err != nil {
		return nil, err
	}
	return &album, nil
}

func (r *gormAlbumRepository) DeleteCascade(spaceID string, id int64) error {
	spaceID = normalizeSpaceID(spaceID)
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ? AND album_id = ?", spaceID, id).Delete(&models.AlbumItem{}).Error; err != nil {
			return err
		}
		result := tx.Where("space_id = ? AND id = ?", spaceID, id).Delete(&models.Album{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("相册不存在")
		}
		return nil
	})
}

func (r *gormAlbumRepository) FirstOrCreateItem(item *models.AlbumItem) error {
	return r.db.Where(models.AlbumItem{SpaceID: item.SpaceID, AlbumID: item.AlbumID, MediaID: item.MediaID}).
		Attrs(models.AlbumItem{AddedAt: item.AddedAt}).FirstOrCreate(item).Error
}

func (r *gormAlbumRepository) DeleteItem(spaceID string, albumID, mediaID int64) error {
	return r.db.Where("space_id = ? AND album_id = ? AND media_id = ?", normalizeSpaceID(spaceID), albumID, mediaID).
		Delete(&models.AlbumItem{}).Error
}

func (r *gormAlbumRepository) CountItem(spaceID string, albumID, mediaID int64) (int64, error) {
	var count int64
	err := r.db.Model(&models.AlbumItem{}).
		Where("space_id = ? AND album_id = ? AND media_id = ?", normalizeSpaceID(spaceID), albumID, mediaID).
		Count(&count).Error
	return count, err
}

func (r *gormAlbumRepository) ListItemMediaIDs(spaceID string, albumID int64) ([]int64, error) {
	var items []models.AlbumItem
	if err := r.db.Where("space_id = ? AND album_id = ?", normalizeSpaceID(spaceID), albumID).
		Order("added_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.MediaID)
	}
	return ids, nil
}

func (r *gormAlbumRepository) ListMediaByIDs(spaceID string, mediaIDs []int64) ([]models.MediaFile, error) {
	if len(mediaIDs) == 0 {
		return []models.MediaFile{}, nil
	}
	var files []models.MediaFile
	err := r.db.Where("space_id = ? AND id IN ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), mediaIDs).
		Find(&files).Error
	return files, err
}
