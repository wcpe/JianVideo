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

// CreateAlbum 新建相册。名称必填，描述可选。
func (s *Service) CreateAlbum(name, description string) (*models.Album, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("相册名称不能为空")
	}
	album := &models.Album{
		Name:        name,
		Description: strings.TrimSpace(description),
	}
	if err := s.db.Create(album).Error; err != nil {
		return nil, err
	}
	return album, nil
}

// ListAlbums 列出全部相册，按创建时间倒序，并附带成员数量。
func (s *Service) ListAlbums() ([]AlbumWithCount, error) {
	var albums []models.Album
	if err := s.db.Order("created_at DESC").Find(&albums).Error; err != nil {
		return nil, err
	}

	result := make([]AlbumWithCount, 0, len(albums))
	for _, a := range albums {
		var count int64
		if err := s.db.Model(&models.AlbumItem{}).Where("album_id = ?", a.ID).Count(&count).Error; err != nil {
			return nil, err
		}
		result = append(result, AlbumWithCount{Album: a, ItemCount: count})
	}
	return result, nil
}

// GetAlbumByID 根据 ID 获取相册。
func (s *Service) GetAlbumByID(id int64) (*models.Album, error) {
	var album models.Album
	if err := s.db.First(&album, id).Error; err != nil {
		return nil, err
	}
	return &album, nil
}

// DeleteAlbum 删除相册：在同一事务内删除相册及其全部成员记录。
// 仅删除逻辑集合，不触碰源文件与 media_files 记录。
func (s *Service) DeleteAlbum(id int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("album_id = ?", id).Delete(&models.AlbumItem{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&models.Album{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("相册不存在")
		}
		return nil
	})
}

// AddAlbumItem 把媒体加入相册。校验相册与媒体均存在；同一媒体重复加入做幂等处理。
func (s *Service) AddAlbumItem(albumID, mediaID int64) error {
	if _, err := s.GetAlbumByID(albumID); err != nil {
		return err
	}
	if _, err := s.GetMediaFileByID(mediaID); err != nil {
		return err
	}

	item := models.AlbumItem{AlbumID: albumID, MediaID: mediaID, AddedAt: time.Now()}
	// 唯一索引 (album_id, media_id) 保证不重复；已存在则幂等返回
	return s.db.Where(models.AlbumItem{AlbumID: albumID, MediaID: mediaID}).
		Attrs(models.AlbumItem{AddedAt: item.AddedAt}).
		FirstOrCreate(&item).Error
}

// RemoveAlbumItem 从相册移出指定媒体。
func (s *Service) RemoveAlbumItem(albumID, mediaID int64) error {
	return s.db.Where("album_id = ? AND media_id = ?", albumID, mediaID).
		Delete(&models.AlbumItem{}).Error
}

// IsMediaInAlbum 判断指定媒体是否为相册成员（FR-43 分享范围校验用）。
func (s *Service) IsMediaInAlbum(albumID, mediaID int64) (bool, error) {
	var cnt int64
	if err := s.db.Model(&models.AlbumItem{}).
		Where("album_id = ? AND media_id = ?", albumID, mediaID).
		Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// ListAlbumItems 列出相册内的媒体成员，按加入时间排序。
func (s *Service) ListAlbumItems(albumID int64) ([]models.MediaFile, error) {
	var items []models.AlbumItem
	if err := s.db.Where("album_id = ?", albumID).Order("added_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []models.MediaFile{}, nil
	}

	mediaIDs := make([]int64, 0, len(items))
	for _, it := range items {
		mediaIDs = append(mediaIDs, it.MediaID)
	}

	// 一次查询取回全部媒体，避免 N+1
	var files []models.MediaFile
	if err := s.db.Where("id IN ?", mediaIDs).Find(&files).Error; err != nil {
		return nil, err
	}

	// 按 album_items 的加入顺序重排
	byID := make(map[int64]models.MediaFile, len(files))
	for _, f := range files {
		byID[f.ID] = f
	}
	ordered := make([]models.MediaFile, 0, len(items))
	for _, it := range items {
		if f, ok := byID[it.MediaID]; ok {
			ordered = append(ordered, f)
		}
	}
	return ordered, nil
}
