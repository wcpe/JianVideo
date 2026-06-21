package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/smb"
)

// 重命名相关业务错误（供上层映射为对应 HTTP 状态码）。
var (
	// ErrInvalidNewName 新文件名为空或包含非法字符。
	ErrInvalidNewName = errors.New("新文件名不合法")
	// ErrRenameTargetExists 目标文件名已存在。
	ErrRenameTargetExists = errors.New("目标文件已存在")
	// ErrRenameUnsupported 该媒体文件不支持重命名（如 SMB 远程文件）。
	ErrRenameUnsupported = errors.New("该媒体文件暂不支持重命名")
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
	// 相机 RAW 系列（经外部 ImageMagick 转 JPEG 显示，见 FR-37 / ADR-0030）
	"cr2": MediaTypeImage, "nef": MediaTypeImage, "arw": MediaTypeImage, "dng": MediaTypeImage,
	"rw2": MediaTypeImage, "raf": MediaTypeImage, "orf": MediaTypeImage, "srw": MediaTypeImage,
	"pef": MediaTypeImage,
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

// LibraryPathView 媒体库目录视图：在目录记录基础上附带已索引媒体数量。
// 供 FR-23 存储库卡片展示，不改动 LibraryPath 数据模型。
type LibraryPathView struct {
	models.LibraryPath
	// MediaCount 该库已索引（未软删）的媒体文件数量
	MediaCount int64 `json:"media_count"`
}

// ListLibraryPathViews 查询所有媒体库目录并附带各库的已索引媒体数量。
// 媒体数量按 library_id 分组一次查询统计，排除已软删（deleted_at 非空）记录，
// 与回收站（FR-25）口径一致，避免按库 N+1 计数。
func (s *Service) ListLibraryPathViews() ([]LibraryPathView, error) {
	paths, err := s.ListLibraryPaths()
	if err != nil {
		return nil, err
	}

	// 一次 GROUP BY 查询各库未软删媒体数量
	type countRow struct {
		LibraryID int64
		Count     int64
	}
	var rows []countRow
	if err := s.db.Model(&models.MediaFile{}).
		Select("library_id, COUNT(*) AS count").
		Where("deleted_at IS NULL").
		Group("library_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	countByLibrary := make(map[int64]int64, len(rows))
	for _, r := range rows {
		countByLibrary[r.LibraryID] = r.Count
	}

	views := make([]LibraryPathView, len(paths))
	for i, p := range paths {
		views[i] = LibraryPathView{LibraryPath: p, MediaCount: countByLibrary[p.ID]}
	}
	return views, nil
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
	// 填充媒体时间与 EXIF（FR-31）：图片提取 EXIF，视频读 creation_time，
	// 再按 exif → 文件名 → 创建 → 修改时间降级链定 media_time。
	// 经可注入函数变量调用，便于测试观测扫描期富化并发。
	enrichMediaMetadataFn(mf)
	if err := s.db.Create(mf).Error; err != nil {
		return nil, err
	}
	return mf, nil
}

// ListMediaFiles 分页查询媒体文件列表。
func (s *Service) ListMediaFiles(libraryID int64, sort, search string, page, pageSize int) ([]models.MediaFile, int64, error) {
	return s.ListMediaFilesFiltered(MediaFilter{LibraryID: libraryID, Sort: sort, Search: search}, page, pageSize)
}

// GetMediaFileByID 根据 ID 获取媒体文件。
// 排除已软删项（deleted_at 非空），使详情/播放/raw/缩略图等正常访问路径对回收站中的媒体视为不存在（FR-25 访问隔离）。
// 还原需读取软删记录，由 RestoreMediaFile 自身的查询完成，不经此方法。
func (s *Service) GetMediaFileByID(id int64) (*models.MediaFile, error) {
	var mf models.MediaFile
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// DeleteMediaFile 软删除单条媒体文件记录（FR-25）。
// 仅置 deleted_at 标记进回收站，不物理删除数据库记录、不动磁盘源文件。
func (s *Service) DeleteMediaFile(id int64) error {
	now := time.Now()
	result := s.db.Model(&models.MediaFile{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("媒体文件不存在")
	}
	return nil
}

// ListDeletedMediaFiles 列出全部已软删的媒体文件（回收站，FR-25），按软删时间倒序。
func (s *Service) ListDeletedMediaFiles() ([]models.MediaFile, error) {
	var items []models.MediaFile
	if err := s.db.Where("deleted_at IS NOT NULL").
		Order("deleted_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// RestoreMediaFile 从回收站还原媒体文件（FR-25）：清空 deleted_at 回到常规列表。
func (s *Service) RestoreMediaFile(id int64) error {
	result := s.db.Model(&models.MediaFile{}).
		Where("id = ? AND deleted_at IS NOT NULL", id).
		Update("deleted_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("回收站中不存在该媒体文件")
	}
	return nil
}

// RenameMediaFile 重命名媒体文件：磁盘改名 + 更新数据库 + 失效旧缩略图。
// newName 仅允许单层文件名（不含路径分隔符），目标已存在时拒绝。
// 先磁盘原子改名，再更新数据库；数据库更新失败时尽力回滚磁盘改名。
func (s *Service) RenameMediaFile(id int64, newName string) (*models.MediaFile, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == "." || newName == ".." || strings.ContainsAny(newName, "/\\") {
		return nil, ErrInvalidNewName
	}

	mf, err := s.GetMediaFileByID(id)
	if err != nil {
		return nil, err
	}

	// SMB 远程文件不支持磁盘改名
	if strings.HasPrefix(mf.FilePath, "smb://") {
		return nil, ErrRenameUnsupported
	}

	oldDiskPath := filepath.FromSlash(mf.FilePath)
	newDiskPath := filepath.Join(filepath.Dir(oldDiskPath), newName)
	newPathSlash := filepath.ToSlash(newDiskPath)

	// 名称未变化，直接返回
	if newPathSlash == mf.FilePath {
		return mf, nil
	}

	// 目标在磁盘或库内已存在则拒绝，避免覆盖
	if _, statErr := os.Stat(newDiskPath); statErr == nil {
		return nil, ErrRenameTargetExists
	}
	if _, dupErr := s.GetMediaFileByLibraryAndPath(mf.LibraryID, newPathSlash); dupErr == nil {
		return nil, ErrRenameTargetExists
	}

	// 先磁盘原子改名
	if err := os.Rename(oldDiskPath, newDiskPath); err != nil {
		return nil, fmt.Errorf("重命名磁盘文件失败: %w", err)
	}

	// 再更新数据库，失败则尽力回滚磁盘改名
	format := strings.TrimPrefix(filepath.Ext(newName), ".")
	updates := map[string]any{
		"file_path":   newPathSlash,
		"file_name":   newName,
		"format":      format,
		"modified_at": time.Now(),
	}
	if err := s.db.Model(&models.MediaFile{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		if rbErr := os.Rename(newDiskPath, oldDiskPath); rbErr != nil {
			log.Printf("[ERROR] 重命名回滚磁盘文件失败: %s -> %s, err=%v", newDiskPath, oldDiskPath, rbErr)
		}
		return nil, fmt.Errorf("更新媒体文件记录失败: %w", err)
	}

	// 旧缩略图按旧路径 hash 命名，重命名后失效，尽力删除并为新文件重新生成
	if rmErr := os.Remove(FindThumbnailPath(mf.FilePath)); rmErr != nil && !os.IsNotExist(rmErr) {
		log.Printf("[WARN] 删除旧缩略图失败: %v", rmErr)
	}
	go GenerateThumbnail(newDiskPath)

	mf.FilePath = newPathSlash
	mf.FileName = newName
	mf.Format = format
	return mf, nil
}

// UpdateDisplayName 设置或清除媒体的库内显示名（FR-30）。
// 仅更新数据库 display_name 列，不动磁盘文件名与路径；去首尾空白后落库，空串表示清除。
// 记录不存在时返回 gorm.ErrRecordNotFound。
func (s *Service) UpdateDisplayName(id int64, displayName string) (*models.MediaFile, error) {
	displayName = strings.TrimSpace(displayName)
	result := s.db.Model(&models.MediaFile{}).Where("id = ?", id).Update("display_name", displayName)
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

// ScanLibrary 异步扫描指定目录，立即返回。
// 根据 dirType 分发到本地扫描或 SMB 扫描。
func (s *Service) ScanLibrary(libraryID int64, dirPath string) {
	s.StartAsyncScan(libraryID, dirPath, "local")
}

// ScanLibraryWithType 按类型同步扫描指定目录，索引所有媒体文件。
// 供 watcher 轮询等内部同步调用使用。
func (s *Service) ScanLibraryWithType(libraryID int64, dirPath, dirType string) (int, error) {
	switch dirType {
	case "smb":
		return s.scanSMBLibrary(libraryID, dirPath)
	default:
		return s.scanLocalLibrary(libraryID, dirPath)
	}
}

// StartAsyncScan 按类型启动异步扫描，立即返回。
// 实际扫描在后台 goroutine 中执行，进度通过 GetScanStatus 查询。
func (s *Service) StartAsyncScan(libraryID int64, dirPath, dirType string) {
	updateScanStatus(func(ss *ScanStatus) {
		*ss = ScanStatus{
			Status:       "scanning",
			LibraryID:    libraryID,
			CurrentPath:  "",
			TotalFiles:   0,
			ScannedFiles: 0,
			StartedAt:    time.Now(),
		}
	})

	go func() {
		count, err := s.ScanLibraryWithType(libraryID, dirPath, dirType)

		if err != nil {
			updateScanStatus(func(ss *ScanStatus) {
				ss.Status = "error"
				ss.Error = err.Error()
				ss.CompletedAt = time.Now()
			})
			log.Printf("[ERROR] 异步扫描失败: libraryID=%d, err=%v", libraryID, err)
			return
		}

		updateScanStatus(func(ss *ScanStatus) {
			ss.Status = "completed"
			ss.ScannedFiles = count
			ss.CompletedAt = time.Now()
		})
		log.Printf("[INFO] 异步扫描完成: libraryID=%d, count=%d", libraryID, count)
	}()
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

	// 记录待扫描文件总数
	updateScanStatus(func(ss *ScanStatus) {
		ss.TotalFiles = len(paths)
	})

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
	masterPwd, err := smb.MasterPassword()
	if err != nil {
		return 0, fmt.Errorf("加载 SMB 凭据失败: %w", err)
	}
	creds, err := store.Load(masterPwd)
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

// scanEnrichMaxConcurrency 扫描期单文件富化（含 ffprobe）并发上限硬顶，
// 与缩略图并发上限一致，避免大库扫描瞬间炸出大量 ffprobe 子进程。
const scanEnrichMaxConcurrency = 4

// scanEnrichConcurrency 返回扫描期富化并发上限：min(scanEnrichMaxConcurrency, CPU 核数)。
func scanEnrichConcurrency() int {
	n := runtime.NumCPU()
	if n > scanEnrichMaxConcurrency {
		n = scanEnrichMaxConcurrency
	}
	if n < 1 {
		n = 1
	}
	return n
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

	// 先确定待入库的新文件固定列表（去重后），再有界并发处理。
	type pendingFile struct {
		fullPath   string
		normalized string
	}
	pending := make([]pendingFile, 0, len(paths))
	for i, fullPath := range paths {
		normalized := normalizedPaths[i]
		if existingSet[normalized] {
			continue
		}
		pending = append(pending, pendingFile{fullPath: fullPath, normalized: normalized})
	}

	// 用固定容量信号量并发处理新文件：单文件内富化（含 ffprobe）仍同步，
	// 总并发不超过 cap，避免大库扫描串行 ffprobe 过慢、又不致瞬间炸出大量子进程（FR-31）。
	sem := make(chan struct{}, scanEnrichConcurrency())
	var wg sync.WaitGroup
	var count int64 // 入库成功计数，并发下用原子操作
	for _, pf := range pending {
		wg.Add(1)
		sem <- struct{}{} // 获取令牌，超出上限时在此排队
		go func(pf pendingFile) {
			defer wg.Done()
			defer func() { <-sem }() // 释放令牌

			info, err := os.Stat(pf.fullPath)
			if err != nil {
				return
			}
			if _, err := s.CreateMediaFile(libraryID, pf.normalized, info.Size()); err != nil {
				log.Printf("[WARN] 媒体文件入库失败: %s, err=%v", pf.normalized, err)
				return
			}
			// SQLite WAL 串行写、计数走原子，进度状态经 updateScanStatus 互斥更新，均并发安全
			done := atomic.AddInt64(&count, 1)

			// 异步生成缩略图，不阻塞入库
			go GenerateThumbnail(pf.fullPath)

			// 每处理 10 个文件更新一次进度
			if done%10 == 0 {
				updateScanStatus(func(ss *ScanStatus) {
					ss.ScannedFiles = int(done)
					ss.CurrentPath = pf.normalized
				})
			}
		}(pf)
	}
	wg.Wait()

	total := int(atomic.LoadInt64(&count))
	// 最终进度更新
	updateScanStatus(func(ss *ScanStatus) {
		ss.ScannedFiles = total
	})
	return total, nil
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
