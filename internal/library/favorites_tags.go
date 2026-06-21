package library

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// MediaFilter 媒体列表筛选条件（FR-41）。
// Favorite 为 nil 表示不按收藏过滤；TagID>0 表示仅返回打了该标签的媒体。
type MediaFilter struct {
	LibraryID int64
	Sort      string
	Search    string
	Favorite  *bool
	TagID     int64
}

// ListMediaFilesFiltered 按筛选条件分页查询媒体文件列表（FR-41）。
// 在原有 library_id/search/排序之上，新增收藏与标签过滤。
func (s *Service) ListMediaFilesFiltered(filter MediaFilter, page, pageSize int) ([]models.MediaFile, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := s.db.Model(&models.MediaFile{})
	if filter.LibraryID > 0 {
		query = query.Where("library_id = ?", filter.LibraryID)
	}
	if filter.Search != "" {
		// 转义 LIKE 通配符，防止用户输入 % 或 _ 干扰查询
		escaped := strings.ReplaceAll(filter.Search, "%", "\\%")
		escaped = strings.ReplaceAll(escaped, "_", "\\_")
		query = query.Where("file_name LIKE ?", "%"+escaped+"%")
	}
	if filter.Favorite != nil {
		query = query.Where("favorite = ?", *filter.Favorite)
	}
	if filter.TagID > 0 {
		// 子查询限定打了该标签的媒体，避免联表导致的重复行
		query = query.Where("id IN (?)",
			s.db.Model(&models.TagMapping{}).Select("media_id").Where("tag_id = ?", filter.TagID))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	switch filter.Sort {
	case "time_asc":
		query = query.Order("added_at ASC")
	case "name":
		query = query.Order("file_name ASC")
	default:
		query = query.Order("added_at DESC")
	}

	var items []models.MediaFile
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// SetMediaFavorite 设置或取消媒体收藏标记（FR-41）。重复设同值幂等。
func (s *Service) SetMediaFavorite(id int64, favorite bool) (*models.MediaFile, error) {
	result := s.db.Model(&models.MediaFile{}).Where("id = ?", id).Update("favorite", favorite)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		// 区分「媒体不存在」与「值未变化」：再查一次确认存在性
		var count int64
		if err := s.db.Model(&models.MediaFile{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	return s.GetMediaFileByID(id)
}

// CreateTag 创建标签（FR-41）。名按去首尾空白规整，非空即可；同名复用已有标签。
func (s *Service) CreateTag(name string) (*models.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("标签名不能为空")
	}
	tag := models.Tag{Name: name}
	if err := s.db.Where(models.Tag{Name: name}).FirstOrCreate(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// ListTags 列出全部标签，按名升序（FR-41）。
func (s *Service) ListTags() ([]models.Tag, error) {
	var tags []models.Tag
	if err := s.db.Order("name ASC").Find(&tags).Error; err != nil {
		return nil, err
	}
	return tags, nil
}

// ListMediaTags 列出指定媒体的标签，按名升序（FR-41）。
func (s *Service) ListMediaTags(mediaID int64) ([]models.Tag, error) {
	var tags []models.Tag
	if err := s.db.
		Joins("JOIN tag_mappings ON tag_mappings.tag_id = tags.id").
		Where("tag_mappings.media_id = ?", mediaID).
		Order("tags.name ASC").
		Find(&tags).Error; err != nil {
		return nil, err
	}
	return tags, nil
}

// AddMediaTag 给媒体打标签（FR-41）。媒体或标签不存在则报错；重复打同标签幂等。
func (s *Service) AddMediaTag(mediaID, tagID int64) error {
	if err := s.ensureMediaExists(mediaID); err != nil {
		return err
	}
	if err := s.ensureTagExists(tagID); err != nil {
		return err
	}
	mapping := models.TagMapping{TagID: tagID, MediaID: mediaID}
	// (tag_id, media_id) 唯一索引保证去重；已存在则不再插入
	return s.db.Where(models.TagMapping{TagID: tagID, MediaID: mediaID}).
		FirstOrCreate(&mapping).Error
}

// RemoveMediaTag 解除媒体与标签的绑定（FR-41）。绑定不存在视为幂等成功。
func (s *Service) RemoveMediaTag(mediaID, tagID int64) error {
	return s.db.Where("media_id = ? AND tag_id = ?", mediaID, tagID).
		Delete(&models.TagMapping{}).Error
}

// ensureMediaExists 校验媒体记录存在。
func (s *Service) ensureMediaExists(mediaID int64) error {
	var count int64
	if err := s.db.Model(&models.MediaFile{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("媒体文件不存在")
	}
	return nil
}

// ensureTagExists 校验标签存在。
func (s *Service) ensureTagExists(tagID int64) error {
	var count int64
	if err := s.db.Model(&models.Tag{}).Where("id = ?", tagID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("标签不存在")
	}
	return nil
}
