package library

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// AlbumWithCount 相册及其成员数量，用于列表展示。
type AlbumWithCount struct {
	models.Album
	ItemCount int64 `json:"item_count"`
}

// CreateAlbum 新建默认 Space 相册。
func (s *Service) CreateAlbum(name, description string) (*models.Album, error) {
	return s.CreateAlbumInSpace(models.DefaultSpaceID, name, description)
}

// CreateAlbumInSpace 新建指定 Space 相册。
func (s *Service) CreateAlbumInSpace(spaceID, name, description string) (*models.Album, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("相册名称不能为空")
	}
	album := &models.Album{SpaceID: normalizeSpaceID(spaceID), Name: name, Description: strings.TrimSpace(description)}
	if err := s.db.Create(album).Error; err != nil {
		return nil, err
	}
	return album, nil
}

// ListAlbums 列出默认 Space 相册。
func (s *Service) ListAlbums() ([]AlbumWithCount, error) {
	return s.ListAlbumsInSpace(models.DefaultSpaceID)
}

// ListAlbumsInSpace 列出指定 Space 相册并附带成员数。
func (s *Service) ListAlbumsInSpace(spaceID string) ([]AlbumWithCount, error) {
	spaceID = normalizeSpaceID(spaceID)
	var result []AlbumWithCount
	err := s.db.Table("albums").
		Select("albums.*, COUNT(album_items.id) AS item_count").
		Joins("LEFT JOIN album_items ON album_items.space_id = albums.space_id AND album_items.album_id = albums.id").
		Where("albums.space_id = ?", spaceID).
		Group("albums.id").
		Order("albums.created_at DESC").
		Scan(&result).Error
	return result, err
}

// GetAlbumByID 获取默认 Space 相册。
func (s *Service) GetAlbumByID(id int64) (*models.Album, error) {
	return s.GetAlbumByIDInSpace(models.DefaultSpaceID, id)
}

// GetAlbumByIDInSpace 获取指定 Space 相册。
func (s *Service) GetAlbumByIDInSpace(spaceID string, id int64) (*models.Album, error) {
	var album models.Album
	if err := s.db.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).First(&album).Error; err != nil {
		return nil, err
	}
	return &album, nil
}

// DeleteAlbum 删除默认 Space 相册。
func (s *Service) DeleteAlbum(id int64) error {
	return s.DeleteAlbumInSpace(models.DefaultSpaceID, id)
}

// DeleteAlbumInSpace 删除指定 Space 相册及成员关联。
func (s *Service) DeleteAlbumInSpace(spaceID string, id int64) error {
	spaceID = normalizeSpaceID(spaceID)
	return s.db.Transaction(func(tx *gorm.DB) error {
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

// AddAlbumItem 把默认 Space 媒体加入相册。
func (s *Service) AddAlbumItem(albumID, mediaID int64) error {
	return s.AddAlbumItemInSpace(models.DefaultSpaceID, albumID, mediaID)
}

// AddAlbumItemInSpace 把指定 Space 媒体加入同 Space 相册。
func (s *Service) AddAlbumItemInSpace(spaceID string, albumID, mediaID int64) error {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.GetAlbumByIDInSpace(spaceID, albumID); err != nil {
		return err
	}
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return err
	}
	item := models.AlbumItem{SpaceID: spaceID, AlbumID: albumID, MediaID: mediaID, AddedAt: time.Now()}
	return s.db.Where(models.AlbumItem{SpaceID: spaceID, AlbumID: albumID, MediaID: mediaID}).
		Attrs(models.AlbumItem{AddedAt: item.AddedAt}).FirstOrCreate(&item).Error
}

// RemoveAlbumItem 从默认 Space 相册移出媒体。
func (s *Service) RemoveAlbumItem(albumID, mediaID int64) error {
	return s.RemoveAlbumItemInSpace(models.DefaultSpaceID, albumID, mediaID)
}

// RemoveAlbumItemInSpace 从指定 Space 相册移出媒体。
func (s *Service) RemoveAlbumItemInSpace(spaceID string, albumID, mediaID int64) error {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.GetAlbumByIDInSpace(spaceID, albumID); err != nil {
		return err
	}
	return s.db.Where("space_id = ? AND album_id = ? AND media_id = ?", spaceID, albumID, mediaID).Delete(&models.AlbumItem{}).Error
}

// IsMediaInAlbum 判断默认 Space 媒体是否为相册成员。
func (s *Service) IsMediaInAlbum(albumID, mediaID int64) (bool, error) {
	return s.IsMediaInAlbumInSpace(models.DefaultSpaceID, albumID, mediaID)
}

// IsMediaInAlbumInSpace 判断指定 Space 媒体是否为相册成员。
func (s *Service) IsMediaInAlbumInSpace(spaceID string, albumID, mediaID int64) (bool, error) {
	var count int64
	err := s.db.Model(&models.AlbumItem{}).
		Where("space_id = ? AND album_id = ? AND media_id = ?", normalizeSpaceID(spaceID), albumID, mediaID).
		Count(&count).Error
	return count > 0, err
}

// ListAlbumItems 列出默认 Space 相册成员。
func (s *Service) ListAlbumItems(albumID int64) ([]models.MediaFile, error) {
	return s.ListAlbumItemsInSpace(models.DefaultSpaceID, albumID)
}

// ListAlbumItemsInSpace 列出指定 Space 相册成员。
func (s *Service) ListAlbumItemsInSpace(spaceID string, albumID int64) ([]models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.GetAlbumByIDInSpace(spaceID, albumID); err != nil {
		return nil, err
	}
	var items []models.AlbumItem
	if err := s.db.Where("space_id = ? AND album_id = ?", spaceID, albumID).Order("added_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []models.MediaFile{}, nil
	}
	mediaIDs := make([]int64, 0, len(items))
	for _, item := range items {
		mediaIDs = append(mediaIDs, item.MediaID)
	}
	var files []models.MediaFile
	if err := s.db.Where("space_id = ? AND id IN ? AND deleted_at IS NULL", spaceID, mediaIDs).Find(&files).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]models.MediaFile, len(files))
	for _, file := range files {
		byID[file.ID] = file
	}
	ordered := make([]models.MediaFile, 0, len(items))
	for _, item := range items {
		if file, ok := byID[item.MediaID]; ok {
			ordered = append(ordered, file)
		}
	}
	return ordered, nil
}
