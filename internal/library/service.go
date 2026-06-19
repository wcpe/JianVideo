package library

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/smb"
)

// Service 媒体库业务逻辑。
type Service struct {
	db       *gorm.DB
	smbCreds *smb.CredentialStore
}

// NewService 创建媒体库服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// SetSMBCredentialStore 设置 SMB 凭据存储器。
func (s *Service) SetSMBCredentialStore(store *smb.CredentialStore) {
	s.smbCreds = store
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
// 根据 dirType 分发到本地扫描或 SMB 扫描。
func (s *Service) ScanLibrary(libraryID int64, dirPath string) (int, error) {
	return s.ScanLibraryWithType(libraryID, dirPath, "local")
}

// ScanLibraryWithType 按类型扫描指定目录，索引所有视频文件。
func (s *Service) ScanLibraryWithType(libraryID int64, dirPath, dirType string) (int, error) {
	switch dirType {
	case "smb":
		return s.scanSMBLibrary(libraryID, dirPath)
	default:
		return s.scanLocalLibrary(libraryID, dirPath)
	}
}

// scanLocalLibrary 扫描本地目录。
func (s *Service) scanLocalLibrary(libraryID int64, dirPath string) (int, error) {
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

	return s.indexVideoFiles(libraryID, paths)
}

// scanSMBLibrary 扫描 SMB 共享目录。
func (s *Service) scanSMBLibrary(libraryID int64, smbPath string) (int, error) {
	// smbPath 格式: host/share/path
	parts := strings.SplitN(smbPath, "/", 3)
	if len(parts) < 2 {
		return 0, fmt.Errorf("SMB 路径格式错误，应为 host/share[/path]: %s", smbPath)
	}

	host := parts[0]
	share := parts[1]
	remotePath := ""
	if len(parts) == 3 {
		remotePath = parts[2]
	}

	// 从凭据存储加载凭据
	creds, err := s.smbCreds.Load("")
	if err != nil {
		return 0, fmt.Errorf("加载 SMB 凭据失败: %w", err)
	}
	creds.Host = host
	creds.Share = share

	client := smb.NewClient(*creds)
	shareFS, err := client.EnsureConnected(context.Background())
	if err != nil {
		return 0, fmt.Errorf("连接 SMB 失败: %w", err)
	}
	defer client.Disconnect()

	smbFS := smb.NewFS(shareFS)

	// 遍历 SMB 目录
	var paths []string
	err = fs.WalkDir(smbFS, remotePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读的条目
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		if !isVideoExt(ext) {
			return nil
		}
		// 使用 smb://host/share/path 格式作为唯一标识
		paths = append(paths, "smb://"+host+"/"+share+"/"+path)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("遍历 SMB 目录失败: %w", err)
	}

	if len(paths) == 0 {
		return 0, nil
	}

	return s.indexSMBMediaFiles(libraryID, paths, smbFS)
}

// indexVideoFiles 将本地视频文件路径批量入库。
func (s *Service) indexVideoFiles(libraryID int64, paths []string) (int, error) {
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

// indexSMBMediaFiles 将 SMB 视频文件路径批量入库。
func (s *Service) indexSMBMediaFiles(libraryID int64, paths []string, smbFS *smb.FS) (int, error) {
	var existingFiles []models.MediaFile
	if err := s.db.Where("file_path IN ?", paths).Find(&existingFiles).Error; err != nil {
		return 0, err
	}
	existingSet := make(map[string]bool, len(existingFiles))
	for _, f := range existingFiles {
		existingSet[f.FilePath] = true
	}

	count := 0
	for _, smbPath := range paths {
		if existingSet[smbPath] {
			continue
		}

		// 从 smb:// URL 中提取相对路径
		relPath := strings.TrimPrefix(smbPath, "smb://")
		// 跳过 host/share/ 前缀
		parts := strings.SplitN(relPath, "/", 3)
		if len(parts) < 3 {
			continue
		}
		filePath := parts[2]

		info, err := smbFS.Stat(filePath)
		if err != nil {
			continue
		}

		if _, err := s.CreateMediaFile(libraryID, smbPath, info.Size()); err != nil {
			log.Printf("[WARN] SMB 媒体文件入库失败: %s, err=%v", smbPath, err)
			continue
		}
		count++
	}
	return count, nil
}

// isVideoExt 判断扩展名是否为视频格式。
func isVideoExt(ext string) bool {
	switch ext {
	case "mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "ts",
		"m4v", "mpg", "mpeg", "3gp", "rmvb", "rm":
		return true
	}
	return false
}
