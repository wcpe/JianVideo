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
	"sync"
	"time"

	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/smb"
)

// Service 媒体库业务逻辑。
type Service struct {
	db         *gorm.DB
	smbCreds   *smb.CredentialStore
	smbCredsMu sync.RWMutex
}

// 媒体类型常量。
const (
	MediaTypeVideo = "video"
	MediaTypeImage = "image"
)

var builtInMediaExtensions = map[string]string{
	"mp4": MediaTypeVideo, "mkv": MediaTypeVideo, "avi": MediaTypeVideo, "mov": MediaTypeVideo,
	"webm": MediaTypeVideo, "flv": MediaTypeVideo, "wmv": MediaTypeVideo, "ts": MediaTypeVideo,
	"m4v": MediaTypeVideo, "mpg": MediaTypeVideo, "mpeg": MediaTypeVideo, "3gp": MediaTypeVideo,
	"rmvb": MediaTypeVideo, "rm": MediaTypeVideo,
	"jpg": MediaTypeImage, "jpeg": MediaTypeImage, "png": MediaTypeImage, "gif": MediaTypeImage,
	"webp": MediaTypeImage, "bmp": MediaTypeImage, "tif": MediaTypeImage, "tiff": MediaTypeImage,
	"heic": MediaTypeImage, "heif": MediaTypeImage,
}

// NewService 创建媒体库服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// SetSMBCredentialStore 设置 SMB 凭据存储器。
func (s *Service) SetSMBCredentialStore(store *smb.CredentialStore) {
	s.smbCredsMu.Lock()
	defer s.smbCredsMu.Unlock()
	s.smbCreds = store
}

// CreateLibraryPath 添加媒体库目录。
func (s *Service) CreateLibraryPath(path, dirType, label string) (*models.LibraryPath, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("路径不能为空")
	}

	if dirType == "" {
		dirType = "local"
	}
	if dirType != "local" && dirType != "smb" {
		return nil, fmt.Errorf("目录类型不支持")
	}

	storedPath := path
	if dirType == "local" {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("本地路径不可访问: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("本地路径不是目录")
		}
		storedPath, err = filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		storedPath = filepath.ToSlash(storedPath)
	} else {
		storedPath = normalizeSMBLibraryPath(path)
	}

	lp := &models.LibraryPath{
		Path:    storedPath,
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
		if err := tx.Where("library_id = ?", id).Delete(&models.MediaExtension{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&models.LibraryPath{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("目录不存在")
		}
		return nil
	})
}

// CreateMediaFile 添加媒体文件记录。
func (s *Service) CreateMediaFile(libraryID int64, filePath string, fileSize int64) (*models.MediaFile, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("文件路径不能为空")
	}

	// 统一存储为正斜杠，保证跨平台 LIKE 查询一致
	filePath = filepath.ToSlash(filePath)

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
	result := s.db.Delete(&models.MediaFile{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("媒体文件不存在")
	}
	return nil
}

// GetMediaFileByPath 根据文件路径查询媒体文件。
func (s *Service) GetMediaFileByPath(filePath string) (*models.MediaFile, error) {
	// 统一为正斜杠，与存储格式一致
	filePath = filepath.ToSlash(filePath)
	var mf models.MediaFile
	if err := s.db.Where("file_path = ?", filePath).First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// GetMediaFileByLibraryAndPath 根据媒体库和文件路径查询媒体文件。
func (s *Service) GetMediaFileByLibraryAndPath(libraryID int64, filePath string) (*models.MediaFile, error) {
	filePath = filepath.ToSlash(filePath)
	var mf models.MediaFile
	if err := s.db.Where("library_id = ? AND file_path = ?", libraryID, filePath).First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// DeleteMediaFileByPath 根据文件路径删除媒体文件记录。
func (s *Service) DeleteMediaFileByPath(filePath string) error {
	filePath = filepath.ToSlash(filePath)
	return s.db.Where("file_path = ?", filePath).Delete(&models.MediaFile{}).Error
}

// DeleteMediaFileByLibraryAndPath 根据媒体库和文件路径删除媒体文件记录。
func (s *Service) DeleteMediaFileByLibraryAndPath(libraryID int64, filePath string) error {
	filePath = filepath.ToSlash(filePath)
	return s.db.Where("library_id = ? AND file_path = ?", libraryID, filePath).Delete(&models.MediaFile{}).Error
}

// BrowseDirectory 浏览指定目录下的子目录和媒体文件。
// 通过 file_path 前缀匹配一次查询所有文件，Go 层按第一级子目录分组。
func (s *Service) BrowseDirectory(libraryID int64, parentPath string) (*models.BrowseResponse, error) {
	// 统一路径分隔符为 /，防止 Windows filepath.Clean 把 / 转成 \
	parentPath = strings.ReplaceAll(parentPath, `\`, `/`)
	// 规范化路径，防止路径遍历
	if strings.Contains(parentPath, "..") {
		return nil, fmt.Errorf("非法路径: parentPath 不能包含 ..")
	}

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
		// 去掉 parentPath 前缀，得到相对路径；Windows 路径需要兼容盘符大小写差异
		rel, ok := trimPathPrefix(f.FilePath, prefix)
		if !ok {
			continue
		}
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
			Path: joinSlashPath(trimmedPath, name),
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
	// 统一路径分隔符为 /，再拆分
	normalized := strings.ReplaceAll(path, `\`, `/`)
	parts := strings.Split(strings.Trim(normalized, "/"), "/")
	var items []models.BreadcrumbItem
	var current string
	for _, p := range parts {
		if p == "" {
			continue
		}
		if isWindowsDrivePart(p) && current == "" {
			current = p
		} else {
			current = strings.TrimRight(current, "/") + "/" + p
		}
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

func joinSlashPath(parent, name string) string {
	if parent == "" || parent == "/" {
		return "/" + name
	}
	return strings.TrimRight(parent, "/") + "/" + name
}

func trimPathPrefix(path, prefix string) (string, bool) {
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix), true
	}
	if strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)) {
		return path[len(prefix):], true
	}
	return "", false
}

func isWindowsDrivePart(part string) bool {
	if len(part) != 2 || part[1] != ':' {
		return false
	}
	ch := part[0]
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

func normalizeSMBLibraryPath(path string) string {
	p := strings.TrimSpace(path)
	p = strings.TrimPrefix(p, "smb://")
	p = strings.TrimLeft(p, `\\`)
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.Trim(p, "/")
}

// ScanLibrary 扫描指定目录，索引所有媒体文件。
// 根据 dirType 分发到本地扫描或 SMB 扫描。
func (s *Service) ScanLibrary(libraryID int64, dirPath string) (int, error) {
	return s.ScanLibraryWithType(libraryID, dirPath, "local")
}

// ScanLibraryWithType 按类型扫描指定目录，索引所有媒体文件。
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
	policy, err := s.mediaExtensionPolicy(libraryID)
	if err != nil {
		return 0, err
	}

	// 收集所有待检查路径
	var paths []string
	err = filepath.WalkDir(dirPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !policy.IsMediaFile(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return 0, err
	}

	if len(paths) == 0 {
		return 0, nil
	}

	return s.indexMediaFiles(libraryID, paths)
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

	policy, err := s.mediaExtensionPolicy(libraryID)
	if err != nil {
		return 0, err
	}

	// 从凭据存储加载凭据
	s.smbCredsMu.RLock()
	store := s.smbCreds
	s.smbCredsMu.RUnlock()
	if store == nil {
		return 0, fmt.Errorf("未配置 SMB 凭据")
	}
	creds, err := store.Load("")
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
		if !policy.IsMediaFile(d.Name()) {
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

// indexMediaFiles 将本地媒体文件路径批量入库。
func (s *Service) indexMediaFiles(libraryID int64, paths []string) (int, error) {
	// 统一所有路径为正斜杠，保证跨平台查询和去重一致
	normalizedPaths := make([]string, len(paths))
	for i, p := range paths {
		normalizedPaths[i] = filepath.ToSlash(p)
	}

	// 批量查询已有记录，避免 N+1 查询
	var existingFiles []models.MediaFile
	if err := s.db.Where("library_id = ? AND file_path IN ?", libraryID, normalizedPaths).Find(&existingFiles).Error; err != nil {
		return 0, err
	}
	existingSet := make(map[string]bool, len(existingFiles))
	for _, f := range existingFiles {
		existingSet[f.FilePath] = true
	}

	count := 0
	for i, fullPath := range paths {
		normalized := normalizedPaths[i]
		if existingSet[normalized] {
			continue
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if _, err := s.CreateMediaFile(libraryID, normalized, info.Size()); err != nil {
			log.Printf("[WARN] 媒体文件入库失败: %s, err=%v", normalized, err)
			continue
		}
		count++
	}
	return count, nil
}

// indexSMBMediaFiles 将 SMB 媒体文件路径批量入库。
func (s *Service) indexSMBMediaFiles(libraryID int64, paths []string, smbFS *smb.FS) (int, error) {
	var existingFiles []models.MediaFile
	if err := s.db.Where("library_id = ? AND file_path IN ?", libraryID, paths).Find(&existingFiles).Error; err != nil {
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

// ListMediaExtensions 查询指定媒体库的媒体后缀。
func (s *Service) ListMediaExtensions(libraryID int64) ([]models.MediaExtension, error) {
	custom, err := s.listCustomMediaExtensions(libraryID)
	if err != nil {
		return nil, err
	}

	items := make([]models.MediaExtension, 0, len(builtInMediaExtensions)+len(custom))
	for ext, mediaType := range builtInMediaExtensions {
		items = append(items, models.MediaExtension{LibraryID: libraryID, Extension: ext, Type: mediaType, IsBuiltIn: 1})
	}
	items = append(items, custom...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Extension == items[j].Extension {
			return items[i].IsBuiltIn > items[j].IsBuiltIn
		}
		return items[i].Extension < items[j].Extension
	})
	return items, nil
}

// AddMediaExtension 添加媒体库自定义后缀。
func (s *Service) AddMediaExtension(libraryID int64, extension, mediaType string) error {
	ext := normalizeExtension(extension)
	if libraryID <= 0 {
		return fmt.Errorf("媒体库 ID 无效")
	}
	if !isAllowedCustomExtension(ext) {
		return fmt.Errorf("后缀格式不支持")
	}
	if mediaType != MediaTypeVideo && mediaType != MediaTypeImage {
		return fmt.Errorf("媒体类型不支持")
	}
	if err := s.ensureLibraryPathExists(libraryID); err != nil {
		return err
	}
	if _, ok := builtInMediaExtensions[ext]; ok {
		return nil
	}

	item := models.MediaExtension{LibraryID: libraryID, Extension: ext, Type: mediaType, IsBuiltIn: 0}
	return s.db.Where(models.MediaExtension{LibraryID: libraryID, Extension: ext}).Attrs(item).FirstOrCreate(&item).Error
}

// IsMediaFile 判断文件是否为内置支持的媒体文件。
func (s *Service) IsMediaFile(path string) bool {
	_, ok := mediaTypeByExtension(normalizeExtension(filepath.Ext(path)))
	return ok
}

// IsMediaFileForLibrary 判断文件是否为指定媒体库支持的媒体文件。
func (s *Service) IsMediaFileForLibrary(libraryID int64, path string) bool {
	_, ok := s.MediaTypeByPathForLibrary(libraryID, path)
	return ok
}

// IsImageFile 判断文件是否为支持的图片文件。
func (s *Service) IsImageFile(path string) bool {
	mediaType, ok := mediaTypeByExtension(normalizeExtension(filepath.Ext(path)))
	return ok && mediaType == MediaTypeImage
}

// MediaTypeByPathForLibrary 根据媒体库与路径后缀获取媒体类型。
func (s *Service) MediaTypeByPathForLibrary(libraryID int64, path string) (string, bool) {
	policy, err := s.mediaExtensionPolicy(libraryID)
	if err != nil {
		return "", false
	}
	return policy.MediaTypeByPath(path)
}

type mediaExtensionPolicy map[string]string

func (p mediaExtensionPolicy) IsMediaFile(path string) bool {
	_, ok := p.MediaTypeByPath(path)
	return ok
}

func (p mediaExtensionPolicy) MediaTypeByPath(path string) (string, bool) {
	ext := normalizeExtension(filepath.Ext(path))
	if ext == "" {
		return "", false
	}
	mediaType, ok := p[ext]
	return mediaType, ok
}

func mediaTypeByExtension(ext string) (string, bool) {
	if ext == "" {
		return "", false
	}
	mediaType, ok := builtInMediaExtensions[ext]
	return mediaType, ok
}

func (s *Service) mediaExtensionPolicy(libraryID int64) (mediaExtensionPolicy, error) {
	policy := make(mediaExtensionPolicy, len(builtInMediaExtensions))
	for ext, mediaType := range builtInMediaExtensions {
		policy[ext] = mediaType
	}
	if s.db == nil || libraryID <= 0 {
		return policy, nil
	}
	custom, err := s.listCustomMediaExtensions(libraryID)
	if err != nil {
		return nil, err
	}
	for _, item := range custom {
		policy[item.Extension] = item.Type
	}
	return policy, nil
}

func (s *Service) listCustomMediaExtensions(libraryID int64) ([]models.MediaExtension, error) {
	var custom []models.MediaExtension
	query := s.db.Order("extension ASC")
	if libraryID > 0 {
		query = query.Where("library_id = ?", libraryID)
	}
	if err := query.Find(&custom).Error; err != nil {
		return nil, err
	}
	return custom, nil
}

func (s *Service) ensureLibraryPathExists(libraryID int64) error {
	var count int64
	if err := s.db.Model(&models.LibraryPath{}).Where("id = ?", libraryID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("媒体库目录不存在")
	}
	return nil
}

func normalizeExtension(extension string) string {
	ext := strings.TrimSpace(strings.ToLower(extension))
	ext = strings.TrimPrefix(ext, ".")
	return ext
}

func isAllowedCustomExtension(ext string) bool {
	if ext == "" || len(ext) > 16 {
		return false
	}
	for _, ch := range ext {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return true
}
