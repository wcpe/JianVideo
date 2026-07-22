package library

import (
	"fmt"
	"strings"
	"time"

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
	if err := s.albumRepo.Create(album); err != nil {
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
	return s.albumRepo.ListWithCount(spaceID)
}

// GetAlbumByID 获取默认 Space 相册。
func (s *Service) GetAlbumByID(id int64) (*models.Album, error) {
	return s.GetAlbumByIDInSpace(models.DefaultSpaceID, id)
}

// GetAlbumByIDInSpace 获取指定 Space 相册。
func (s *Service) GetAlbumByIDInSpace(spaceID string, id int64) (*models.Album, error) {
	return s.albumRepo.GetByID(spaceID, id)
}

// DeleteAlbum 删除默认 Space 相册。
func (s *Service) DeleteAlbum(id int64) error {
	return s.DeleteAlbumInSpace(models.DefaultSpaceID, id)
}

// DeleteAlbumInSpace 删除指定 Space 相册及成员关联。
func (s *Service) DeleteAlbumInSpace(spaceID string, id int64) error {
	return s.albumRepo.DeleteCascade(spaceID, id)
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
	return s.albumRepo.FirstOrCreateItem(&item)
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
	return s.albumRepo.DeleteItem(spaceID, albumID, mediaID)
}

// IsMediaInAlbum 判断默认 Space 媒体是否为相册成员。
func (s *Service) IsMediaInAlbum(albumID, mediaID int64) (bool, error) {
	return s.IsMediaInAlbumInSpace(models.DefaultSpaceID, albumID, mediaID)
}

// IsMediaInAlbumInSpace 判断指定 Space 媒体是否为相册成员。
func (s *Service) IsMediaInAlbumInSpace(spaceID string, albumID, mediaID int64) (bool, error) {
	count, err := s.albumRepo.CountItem(spaceID, albumID, mediaID)
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
	mediaIDs, err := s.albumRepo.ListItemMediaIDs(spaceID, albumID)
	if err != nil {
		return nil, err
	}
	if len(mediaIDs) == 0 {
		return []models.MediaFile{}, nil
	}
	files, err := s.albumRepo.ListMediaByIDs(spaceID, mediaIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]models.MediaFile, len(files))
	for _, file := range files {
		byID[file.ID] = file
	}
	ordered := make([]models.MediaFile, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		if file, ok := byID[id]; ok {
			ordered = append(ordered, file)
		}
	}
	return ordered, nil
}
