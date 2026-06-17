package library

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// Service 媒体库业务逻辑。
type Service struct {
	db *gorm.DB
}

// NewService 创建媒体库服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// CreateLibraryPath 添加媒体库目录。
func (s *Service) CreateLibraryPath(path, dirType, label string) (*models.LibraryPath, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	lp := &models.LibraryPath{
		Path:    absPath,
		Type:    dirType,
		Label:   label,
		Enabled: 1,
	}
	if err := s.db.Create(lp).Error; err != nil {
		return nil, err
	}
	return lp, nil
}

// ListLibraryPaths 查询所有媒体库目录。
func (s *Service) ListLibraryPaths() ([]models.LibraryPath, error) {
	var items []models.LibraryPath
	if err := s.db.Order("id").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetLibraryPathByID 根据 ID 获取媒体库目录。
func (s *Service) GetLibraryPathByID(id int64) (*models.LibraryPath, error) {
	var lp models.LibraryPath
	if err := s.db.First(&lp, id).Error; err != nil {
		return nil, err
	}
	return &lp, nil
}

// DeleteLibraryPath 删除媒体库目录及其关联的媒体文件记录。
func (s *Service) DeleteLibraryPath(id int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ?", id).Delete(&models.MediaFile{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.LibraryPath{}, id).Error
	})
}

// CreateMediaFile 添加媒体文件记录。
func (s *Service) CreateMediaFile(libraryID int64, filePath string, fileSize int64) (*models.MediaFile, error) {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")

	mf := &models.MediaFile{
		LibraryID:  libraryID,
		FilePath:   filePath,
		FileName:   filepath.Base(filePath),
		FileSize:   fileSize,
		Format:     ext,
		AddedAt:    time.Now(),
		ModifiedAt: time.Now(),
	}
	if err := s.db.Create(mf).Error; err != nil {
		return nil, err
	}
	return mf, nil
}

// ListMediaFiles 分页查询媒体文件列表。
func (s *Service) ListMediaFiles(libraryID int64, sort, search string, page, pageSize int) ([]models.MediaFile, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var total int64
	query := s.db.Model(&models.MediaFile{})
	if libraryID > 0 {
		query = query.Where("library_id = ?", libraryID)
	}
	if search != "" {
		query = query.Where("file_name LIKE ?", "%"+search+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []models.MediaFile
	// 排序
	switch sort {
	case "time_asc":
		query = query.Order("added_at ASC")
	case "name":
		query = query.Order("file_name ASC")
	default:
		query = query.Order("added_at DESC")
	}

	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// GetMediaFileByID 根据 ID 获取媒体文件。
func (s *Service) GetMediaFileByID(id int64) (*models.MediaFile, error) {
	var mf models.MediaFile
	if err := s.db.First(&mf, id).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// DeleteMediaFile 删除单条媒体文件记录。
func (s *Service) DeleteMediaFile(id int64) error {
	return s.db.Delete(&models.MediaFile{}, id).Error
}

// GetMediaFileByPath 根据文件路径查询媒体文件。
func (s *Service) GetMediaFileByPath(filePath string) (*models.MediaFile, error) {
	var mf models.MediaFile
	if err := s.db.Where("file_path = ?", filePath).First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// DeleteMediaFileByPath 根据文件路径删除媒体文件记录。
func (s *Service) DeleteMediaFileByPath(filePath string) error {
	return s.db.Where("file_path = ?", filePath).Delete(&models.MediaFile{}).Error
}

// ScanLibrary 扫描指定目录，索引所有视频文件。
func (s *Service) ScanLibrary(libraryID int64, dirPath string) (int, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return 0, err
	}

	videoExts := map[string]bool{
		"mp4": true, "mkv": true, "avi": true, "mov": true,
		"webm": true, "flv": true, "wmv": true, "ts": true,
		"m4v": true, "mpg": true, "mpeg": true, "3gp": true,
		"rmvb": true, "rm": true,
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Name()), "."))
		if !videoExts[ext] {
			continue
		}

		fullPath := filepath.Join(dirPath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// 检查是否已存在
		var existing models.MediaFile
		if err := s.db.Where("file_path = ?", fullPath).First(&existing).Error; err == nil {
			// 已存在，跳过
			continue
		}

		if _, err := s.CreateMediaFile(libraryID, fullPath, info.Size()); err != nil {
			continue
		}
		count++
	}
	return count, nil
}
