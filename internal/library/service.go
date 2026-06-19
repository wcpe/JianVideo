package library

import (
	"log"
	"os"
	"path/filepath"
	"sort"
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
		// 转义 LIKE 通配符，防止用户输入 % 或 _ 干扰查询
		escaped := strings.ReplaceAll(search, "%", "\\%")
		escaped = strings.ReplaceAll(escaped, "_", "\\_")
		query = query.Where("file_name LIKE ?", "%"+escaped+"%")
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

// BrowseDirectory 浏览指定目录下的子目录和媒体文件。
// 通过 file_path 前缀匹配一次查询所有文件，Go 层按第一级子目录分组。
func (s *Service) BrowseDirectory(libraryID int64, parentPath string) (*models.BrowseResponse, error) {
	// 规范化路径：确保以 / 结尾，用于前缀匹配
	trimmedPath := strings.TrimRight(parentPath, "/")
	prefix := trimmedPath + "/"

	// 一次 SQL 查询：获取所有 file_path 以 prefix 开头的媒体文件
	var allFiles []models.MediaFile
	if err := s.db.Where("file_path LIKE ? AND library_id = ?", prefix+"%", libraryID).
		Order("file_path ASC").Find(&allFiles).Error; err != nil {
		return nil, err
	}

	// 构建面包屑
	breadcrumbs := buildBreadcrumbs(trimmedPath)

	// Go 层聚合：按第一级子目录分组
	dirSet := make(map[string]bool)
	files := make([]models.MediaFile, 0)

	for _, f := range allFiles {
		// 去掉 parentPath 前缀，得到相对路径
		rel := strings.TrimPrefix(f.FilePath, prefix)
		// 如果包含 / 说明在子目录中
		if idx := strings.Index(rel, "/"); idx != -1 {
			dirName := rel[:idx]
			dirSet[dirName] = true
		} else {
			// 直接文件
			files = append(files, f)
		}
	}

	// 子目录排序
	dirs := make([]models.DirInfo, 0, len(dirSet))
	for name := range dirSet {
		dirs = append(dirs, models.DirInfo{
			Name: name,
			Path: prefix + name,
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })

	return &models.BrowseResponse{
		Breadcrumbs: breadcrumbs,
		Directories: dirs,
		Files:       files,
	}, nil
}

// buildBreadcrumbs 将路径拆分为面包屑段。
func buildBreadcrumbs(path string) []models.BreadcrumbItem {
	// 拆分路径
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var items []models.BreadcrumbItem
	var current string
	for _, p := range parts {
		if p == "" {
			continue
		}
		current += "/" + p
		items = append(items, models.BreadcrumbItem{
			Name: p,
			Path: current,
		})
	}
	if len(items) == 0 {
		items = append(items, models.BreadcrumbItem{Name: "/", Path: "/"})
	}
	return items
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

	// 收集所有待检查路径
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Name()), "."))
		if !videoExts[ext] {
			continue
		}
		paths = append(paths, filepath.Join(dirPath, entry.Name()))
	}

	if len(paths) == 0 {
		return 0, nil
	}

	// 批量查询已有记录，避免 N+1 查询
	var existingFiles []models.MediaFile
	if err := s.db.Where("file_path IN ?", paths).Find(&existingFiles).Error; err != nil {
		return 0, err
	}
	existingSet := make(map[string]bool, len(existingFiles))
	for _, f := range existingFiles {
		existingSet[f.FilePath] = true
	}

	count := 0
	for _, fullPath := range paths {
		if existingSet[fullPath] {
			continue
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if _, err := s.CreateMediaFile(libraryID, fullPath, info.Size()); err != nil {
			log.Printf("[WARN] 媒体文件入库失败: %s, err=%v", fullPath, err)
			continue
		}
		count++
	}
	return count, nil
}
