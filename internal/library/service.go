// Package library 提供媒体库目录管理、扫描入库、统计聚合等核心能力。
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

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/smb"
)

// 重命名相关业务错误（供上层映射为对应 HTTP 状态码）。
var (
	// ErrInvalidNewName 新文件名为空或包含非法字符。
	ErrInvalidNewName = errors.New("新文件名不合法")
	// ErrRenameTargetExists 目标文件名已存在。
	ErrRenameTargetExists = errors.New("目标文件已存在")
	// ErrRenameUnsupported 该媒体文件不支持重命名（如 SMB 远程文件）。
	ErrRenameUnsupported = errors.New("该媒体文件暂不支持重命名")
	// ErrMoveUnsupported 该媒体文件不支持移动（如 SMB 远程文件）。
	ErrMoveUnsupported = errors.New("该媒体文件暂不支持移动")
	// ErrInvalidMoveTarget 移动目标目录不合法。
	ErrInvalidMoveTarget = errors.New("移动目标目录不合法")
)

// Service 媒体库业务逻辑。
type Service struct {
	db                           *gorm.DB
	mediaRepo                    MediaQueryRepository
	metadataRepo                 metadataRepository
	chapterRepo                  chapterRepository
	bookmarkRepo                 bookmarkRepository
	metadataParser               embeddedMetadataParser
	audit                        audit.Recorder
	changeHook                   func(ScanChange)
	inferenceConfig              InferenceConfigProvider
	inferenceCompensationEnqueue InferenceCompensationEnqueuer
	inferenceCompensationWake    func()
	beforeAutoInferenceSave      func()
	inferenceBackfillBatchHook   func(int)
	now                          func() time.Time
	smbCreds                     *smb.CredentialStore
	smbCredsMu                   sync.RWMutex
}

// 媒体类型常量。
const (
	MediaTypeVideo = "video"
	MediaTypeImage = "image"
)

// 扫描模式常量（FR-27）。
const (
	// ScanModeIncremental 增量更新：只索引新增/变更文件，不对账，速度快。
	ScanModeIncremental = "incremental"
	// ScanModeFull 全量扫描：遍历全部文件并对账，库内 active 记录源文件不存在时标记 missing。
	ScanModeFull = "full"
)

// BrowseRootMarker 目录浏览顶层根标记（FR-121，取代 ADR-0037 虚拟库根）。
// parent_path 取此哨兵值时进入顶层根分支：列出各启用库推导出的卷根/共享根（去重排序）。
// 真实文件路径不含该串，故不与任何实际目录冲突。
const BrowseRootMarker = "__root__"

// 目录浏览文件排序键（FR-121）。缺省按名（BrowseSortName）。
const (
	// BrowseSortName 按文件名升序。
	BrowseSortName = "name"
	// BrowseSortSize 按文件大小升序。
	BrowseSortSize = "size"
	// BrowseSortType 按文件类型（格式后缀）升序，同类型再按名。
	BrowseSortType = "type"
	// BrowseSortTime 按修改时间升序。
	BrowseSortTime = "time"
)

// NormalizeScanMode 规范化扫描模式：仅 full 视为全量，其余（含空串/非法值）按增量。
// 供端点解析查询参数时回退，保证向后兼容。
func NormalizeScanMode(mode string) string {
	if mode == ScanModeFull {
		return ScanModeFull
	}
	return ScanModeIncremental
}

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
	return &Service{
		db: db, mediaRepo: newGormMediaRepository(db), metadataRepo: newGormMetadataRepository(db),
		chapterRepo: newGormChapterRepository(db), bookmarkRepo: newGormBookmarkRepository(db),
		metadataParser: defaultEmbeddedMetadataParser, now: time.Now,
	}
}

// WithAudit 注入审计记录器，使媒体库关键变更与审计事件同事务提交。
func (s *Service) WithAudit(rec audit.Recorder) *Service {
	s.audit = rec
	return s
}

// WithScanChangeHook 注入扫描变更失效 hook，供元数据、缩略图、hash 后续能力消费。
func (s *Service) WithScanChangeHook(fn func(ScanChange)) *Service {
	s.changeHook = fn
	return s
}

// SpaceExists 判断 Space 是否存在。缺省 Space 兼容旧测试库，不强依赖 spaces 表。
func (s *Service) SpaceExists(spaceID string) (bool, error) {
	spaceID = normalizeSpaceID(spaceID)
	if !s.db.Migrator().HasTable(&models.Space{}) {
		return false, nil
	}
	var count int64
	if err := s.db.Model(&models.Space{}).Where("id = ?", spaceID).Count(&count).Error; err != nil {
		return false, err
	}
	return count == 1, nil
}

// GetSpace 返回指定 Space 的基础归属信息。
func (s *Service) GetSpace(spaceID string) (*models.Space, error) {
	var space models.Space
	if err := s.db.Where("id = ?", normalizeSpaceID(spaceID)).First(&space).Error; err != nil {
		return nil, err
	}
	return &space, nil
}

// CountLibraryPathsInSpace 返回指定 Space 已注册媒体库目录数量。
func (s *Service) CountLibraryPathsInSpace(spaceID string) (int64, error) {
	var count int64
	err := s.db.Model(&models.LibraryPath{}).Where("space_id = ?", normalizeSpaceID(spaceID)).Count(&count).Error
	return count, err
}

// SetSMBCredentialStore 设置 SMB 凭据存储器。
func (s *Service) SetSMBCredentialStore(store *smb.CredentialStore) {
	s.smbCredsMu.Lock()
	defer s.smbCredsMu.Unlock()
	s.smbCreds = store
}

// CreateLibraryPath 添加媒体库目录。
func (s *Service) CreateLibraryPath(path, dirType, label string) (*models.LibraryPath, error) {
	return s.CreateLibraryPathInSpace(models.DefaultSpaceID, path, dirType, label)
}

// CreateLibraryPathInSpace 添加指定 Space 的媒体库目录。
func (s *Service) CreateLibraryPathInSpace(spaceID, path, dirType, label string) (*models.LibraryPath, error) {
	return s.CreateLibraryPathWithKindInSpace(spaceID, path, dirType, label, "")
}

// CreateLibraryPathWithKindInSpace 添加指定 Space 与内容分型的媒体库目录。
func (s *Service) CreateLibraryPathWithKindInSpace(spaceID, path, dirType, label, libraryKind string) (*models.LibraryPath, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("路径不能为空")
	}
	kind, err := normalizeLibraryKind(libraryKind)
	if err != nil {
		return nil, err
	}

	if dirType == "" {
		dirType = "local"
	}
	if dirType != "local" && dirType != "smb" {
		return nil, fmt.Errorf("目录类型不支持")
	}

	var storedPath string
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
		SpaceID:            normalizeSpaceID(spaceID),
		Path:               storedPath,
		Type:               dirType,
		LibraryKind:        kind,
		LibraryProfileJSON: "{}",
		Label:              label,
		Enabled:            1,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(lp).Error; err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      lp.SpaceID,
			ActorType:    audit.ActorSystem,
			Action:       "library.created",
			ResourceType: "library",
			ResourceID:   fmt.Sprintf("%d", lp.ID),
			After:        libraryAuditPayload(lp),
		})
	}); err != nil {
		return nil, err
	}
	return lp, nil
}

// ListLibraryPaths 查询所有媒体库目录。
func (s *Service) ListLibraryPaths() ([]models.LibraryPath, error) {
	return s.ListLibraryPathsInSpace(models.DefaultSpaceID)
}

// ListAllLibraryPaths 返回所有 Space 的媒体库，仅供后台 watcher 与定时扫描枚举。
func (s *Service) ListAllLibraryPaths() ([]models.LibraryPath, error) {
	var paths []models.LibraryPath
	if err := s.db.Order("space_id ASC, id ASC").Find(&paths).Error; err != nil {
		return nil, err
	}
	return paths, nil
}

// ListLibraryPathsInSpace 查询指定 Space 的媒体库目录。
func (s *Service) ListLibraryPathsInSpace(spaceID string) ([]models.LibraryPath, error) {
	return s.mediaRepo.ListLibraryPaths(spaceID)
}

// PathView 媒体库目录视图：在目录记录基础上附带已索引媒体数量。
// 供 FR-23 存储库卡片展示，不改动 LibraryPath 数据模型。
type PathView struct {
	models.LibraryPath
	// MediaCount 该库已索引且可用的媒体文件数量
	MediaCount int64 `json:"media_count"`
}

// ListLibraryPathViews 查询所有媒体库目录并附带各库的已索引媒体数量。
// 媒体数量按 library_id 分组一次查询统计，排除已软删和 missing 记录，避免按库 N+1 计数。
func (s *Service) ListLibraryPathViews() ([]PathView, error) {
	return s.ListLibraryPathViewsInSpace(models.DefaultSpaceID)
}

// ListLibraryPathViewsInSpace 查询指定 Space 的媒体库目录并附带各库的已索引媒体数量。
func (s *Service) ListLibraryPathViewsInSpace(spaceID string) ([]PathView, error) {
	paths, err := s.ListLibraryPathsInSpace(spaceID)
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
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Group("library_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	countByLibrary := make(map[int64]int64, len(rows))
	for _, r := range rows {
		countByLibrary[r.LibraryID] = r.Count
	}

	views := make([]PathView, len(paths))
	for i, p := range paths {
		views[i] = PathView{LibraryPath: p, MediaCount: countByLibrary[p.ID]}
	}
	return views, nil
}

// GetLibraryPathByID 根据 ID 获取媒体库目录。
func (s *Service) GetLibraryPathByID(id int64) (*models.LibraryPath, error) {
	return s.GetLibraryPathByIDInSpace(models.DefaultSpaceID, id)
}

// GetLibraryPathByIDInSpace 根据 Space 与 ID 获取媒体库目录。
func (s *Service) GetLibraryPathByIDInSpace(spaceID string, id int64) (*models.LibraryPath, error) {
	return s.mediaRepo.GetLibraryPathByID(spaceID, id)
}

// UpdateLibraryPathInSpace 更新指定 Space 的媒体库展示属性。
func (s *Service) UpdateLibraryPathInSpace(spaceID string, id int64, label *string, enabled *bool) (*models.LibraryPath, error) {
	return s.UpdateLibraryPathWithKindInSpace(spaceID, id, label, enabled, nil)
}

// UpdateLibraryPathWithKindInSpace 更新指定 Space 的媒体库展示属性与内容分型。
func (s *Service) UpdateLibraryPathWithKindInSpace(spaceID string, id int64, label *string, enabled *bool, libraryKind *string) (*models.LibraryPath, error) {
	spaceID = normalizeSpaceID(spaceID)
	var after models.LibraryPath
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var before models.LibraryPath
		if err := tx.Where("space_id = ? AND id = ?", spaceID, id).First(&before).Error; err != nil {
			return err
		}
		updates, err := libraryPathUpdates(label, enabled, libraryKind)
		if err != nil {
			return err
		}
		if len(updates) == 0 {
			after = before
			return nil
		}
		if err := tx.Model(&models.LibraryPath{}).Where("space_id = ? AND id = ?", spaceID, id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id = ? AND id = ?", spaceID, id).First(&after).Error; err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "library.updated",
			ResourceType: "library",
			ResourceID:   fmt.Sprintf("%d", before.ID),
			Before:       libraryAuditPayload(&before),
			After:        libraryAuditPayload(&after),
		})
	}); err != nil {
		return nil, err
	}
	return &after, nil
}

func libraryPathUpdates(label *string, enabled *bool, libraryKind *string) (map[string]any, error) {
	updates := map[string]any{}
	if label != nil {
		updates["label"] = strings.TrimSpace(*label)
	}
	if enabled != nil {
		if *enabled {
			updates["enabled"] = 1
		} else {
			updates["enabled"] = 0
		}
	}
	if libraryKind != nil {
		kind, err := normalizeLibraryKind(*libraryKind)
		if err != nil {
			return nil, err
		}
		updates["library_kind"] = kind
	}
	return updates, nil
}

func (s *Service) spaceIDForLibrary(libraryID int64) (string, error) {
	if !s.db.Migrator().HasTable(&models.LibraryPath{}) {
		return models.DefaultSpaceID, nil
	}
	var lp models.LibraryPath
	if err := s.db.Select("space_id").First(&lp, libraryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.DefaultSpaceID, nil
		}
		return "", err
	}
	return normalizeSpaceID(lp.SpaceID), nil
}

// DeleteLibraryPath 删除媒体库目录及其关联的媒体文件记录。
func (s *Service) DeleteLibraryPath(id int64) error {
	return s.DeleteLibraryPathInSpace(models.DefaultSpaceID, id)
}

// DeleteLibraryPathInSpace 删除指定 Space 的媒体库目录及其关联媒体记录。
func (s *Service) DeleteLibraryPathInSpace(spaceID string, id int64) error {
	spaceID = normalizeSpaceID(spaceID)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var before models.LibraryPath
		if err := tx.Where("space_id = ? AND id = ?", spaceID, id).First(&before).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id = ? AND library_id = ?", spaceID, id).Delete(&models.MediaFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("library_id = ?", id).Delete(&models.MediaExtension{}).Error; err != nil {
			return err
		}
		result := tx.Where("space_id = ? AND id = ?", spaceID, id).Delete(&models.LibraryPath{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("目录不存在")
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "library.deleted",
			ResourceType: "library",
			ResourceID:   fmt.Sprintf("%d", before.ID),
			Before:       libraryAuditPayload(&before),
		})
	})
}

// CreateMediaFile 添加媒体文件记录。
func (s *Service) CreateMediaFile(libraryID int64, filePath string, fileSize int64) (*models.MediaFile, error) {
	spaceID, err := s.spaceIDForLibrary(libraryID)
	if err != nil {
		return nil, err
	}
	return s.CreateMediaFileInSpace(spaceID, libraryID, filePath, fileSize)
}

// CreateMediaFileInSpace 添加指定 Space 的媒体文件记录。
func (s *Service) CreateMediaFileInSpace(spaceID string, libraryID int64, filePath string, fileSize int64) (*models.MediaFile, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("文件路径不能为空")
	}

	// 统一存储为正斜杠，保证跨平台 LIKE 查询一致
	filePath = filepath.ToSlash(filePath)

	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")

	mf := &models.MediaFile{
		SpaceID:          normalizeSpaceID(spaceID),
		LibraryID:        libraryID,
		FilePath:         filePath,
		FileName:         filepath.Base(filePath),
		FileSize:         fileSize,
		Format:           ext,
		FileState:        models.MediaFileStateAvailable,
		ContentHashStale: true,
		AddedAt:          time.Now(),
		ModifiedAt:       time.Now(),
	}
	// 填充媒体时间与 EXIF（FR-31）：图片提取 EXIF，视频读 creation_time，
	// 再按 exif → 文件名 → 创建 → 修改时间降级链定 media_time。
	// 经可注入函数变量调用，便于测试观测扫描期富化并发。
	enrichMediaMetadataFn(mf)
	if err := s.db.Create(mf).Error; err != nil {
		return nil, err
	}
	if _, err := s.InferAndStoreMediaInSpace(mf.SpaceID, mf.ID); err != nil {
		if compensationErr := s.compensateImmediateInferenceFailure(mf, err); compensationErr != nil {
			return mf, compensationErr
		}
	}
	s.notifyScanChange(ScanChange{SpaceID: mf.SpaceID, LibraryID: mf.LibraryID, Path: mf.FilePath, Op: ScanChangeAdded, FingerprintChanged: true})
	return mf, nil
}

func (s *Service) compensateImmediateInferenceFailure(mf *models.MediaFile, inferenceErr error) error {
	if s.inferenceCompensationEnqueue == nil {
		return inferenceErr
	}
	if err := s.inferenceCompensationEnqueue(context.Background(), mf.SpaceID, mf.LibraryID, mf.ID); err != nil {
		return fmt.Errorf("即时推断失败且补偿任务入队失败: inference=%v: %w", inferenceErr, err)
	}
	log.Printf("[WARN] 媒体即时推断失败，已持久化补偿任务: mediaID=%d, err=%v", mf.ID, inferenceErr)
	if s.inferenceCompensationWake != nil {
		s.inferenceCompensationWake()
	}
	return nil
}

// ListMediaFiles 分页查询媒体文件列表。
func (s *Service) ListMediaFiles(libraryID int64, sort, search string, page, pageSize int) ([]models.MediaFile, int64, error) {
	return s.ListMediaFilesFiltered(MediaFilter{LibraryID: libraryID, Sort: sort, Search: search}, page, pageSize)
}

// ListMediaFilesPage 按筛选条件返回带 cursor 的媒体分页结果。
func (s *Service) ListMediaFilesPage(filter MediaFilter, page MediaPageRequest) (MediaPageResult, error) {
	if filter.MediaType != "" && len(filter.MediaTypeExtensions) == 0 {
		exts, err := s.EnabledExtensionsForType(filter.SpaceID, filter.LibraryID, filter.MediaType)
		if err != nil {
			return MediaPageResult{}, err
		}
		filter.MediaTypeExtensions = exts
	}
	return s.mediaRepo.ListMediaFiles(filter, page)
}

// GetMediaFileByID 根据 ID 获取媒体文件。
// 排除已软删项（deleted_at 非空），使详情/播放/raw/缩略图等正常访问路径对回收站中的媒体视为不存在（FR-25 访问隔离）。
// 还原需读取软删记录，由 RestoreMediaFile 自身的查询完成，不经此方法。
func (s *Service) GetMediaFileByID(id int64) (*models.MediaFile, error) {
	return s.GetMediaFileByIDInSpace(models.DefaultSpaceID, id)
}

// GetMediaFileByIDInSpace 根据 Space 与 ID 获取媒体文件。
func (s *Service) GetMediaFileByIDInSpace(spaceID string, id int64) (*models.MediaFile, error) {
	return s.mediaRepo.GetMediaFileByID(spaceID, id)
}

// CountThumbnailCandidates 返回当前 Space 可生成缩略图的媒体数量。
func (s *Service) CountThumbnailCandidates(spaceID string) (int64, error) {
	var count int64
	err := s.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Count(&count).Error
	return count, err
}

// ListThumbnailCandidates 按 ID 游标返回当前 Space 的缩略图批量候选。
func (s *Service) ListThumbnailCandidates(spaceID string, afterID int64, limit int) ([]models.MediaFile, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var items []models.MediaFile
	err := s.db.Where("space_id = ? AND id > ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), afterID).
		Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

// DeleteMediaFile 软删除单条媒体文件记录（FR-25）。
// 仅置 deleted_at 标记进回收站，不物理删除数据库记录、不动磁盘源文件。
func (s *Service) DeleteMediaFile(id int64) error {
	return s.DeleteMediaFileInSpace(models.DefaultSpaceID, id)
}

// DeleteMediaFileInSpace 软删除指定 Space 的媒体文件记录（FR-25）。
func (s *Service) DeleteMediaFileInSpace(spaceID string, id int64) error {
	now := time.Now()
	spaceID = normalizeSpaceID(spaceID)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var before models.MediaFile
		if err := tx.Where("space_id = ? AND id = ? AND deleted_at IS NULL", spaceID, id).First(&before).Error; err != nil {
			return fmt.Errorf("媒体文件不存在")
		}
		if err := tx.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).
			Update("deleted_at", now).Error; err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.deleted",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", before.ID),
			Before:       mediaAuditPayload(&before),
			After:        map[string]any{"deleted_at": now},
		})
	})
}

// BatchDeleteMediaFiles 批量软删媒体文件（FR-69）。
// 复用 FR-25 软删语义：在单事务内对所有有效 id 一次 UPDATE 置 deleted_at，不动磁盘源文件。
// 跳过不存在 / 已软删的 id（不计入返回值、不报错）；空列表为 no-op 返回 0。返回实际软删条数。
func (s *Service) BatchDeleteMediaFiles(ids []int64) (int64, error) {
	return s.BatchDeleteMediaFilesInSpace(models.DefaultSpaceID, ids)
}

// BatchDeleteMediaFilesInSpace 批量软删指定 Space 的媒体文件（FR-69）。
func (s *Service) BatchDeleteMediaFilesInSpace(spaceID string, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	spaceID = normalizeSpaceID(spaceID)
	now := time.Now()
	var affected int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var before []models.MediaFile
		if err := tx.Where("space_id = ? AND id IN ? AND deleted_at IS NULL", spaceID, ids).
			Order("id ASC").
			Find(&before).Error; err != nil {
			return err
		}
		if len(before) == 0 {
			return nil
		}
		validIDs := make([]int64, 0, len(before))
		for i := range before {
			validIDs = append(validIDs, before[i].ID)
		}
		result := tx.Model(&models.MediaFile{}).
			Where("space_id = ? AND id IN ?", spaceID, validIDs).
			Update("deleted_at", now)
		if result.Error != nil {
			return result.Error
		}
		affected = result.RowsAffected
		for i := range before {
			if err := s.recordAuditTx(tx, audit.EventInput{
				Scope:        audit.ScopeSpace,
				SpaceID:      spaceID,
				ActorType:    audit.ActorSystem,
				Action:       "media.deleted",
				ResourceType: "media",
				ResourceID:   fmt.Sprintf("%d", before[i].ID),
				Before:       mediaAuditPayload(&before[i]),
				After:        map[string]any{"deleted_at": now},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// GetMediaFilesByIDs 批量获取媒体文件（FR-91 批量打包下载）。
// 单次 IN 查询取回，排除已软删项（与 GetMediaFileByID 的访问隔离一致），避免 N+1。
// 不存在 / 已软删的 id 自然不在结果中；空列表为 no-op 返回空切片。
// 返回顺序不保证与入参一致，调用方如需保序自行处理。
func (s *Service) GetMediaFilesByIDs(ids []int64) ([]models.MediaFile, error) {
	return s.GetMediaFilesByIDsInSpace(models.DefaultSpaceID, ids)
}

// GetMediaFilesByIDsInSpace 批量获取指定 Space 的媒体文件（FR-91 批量打包下载）。
func (s *Service) GetMediaFilesByIDsInSpace(spaceID string, ids []int64) ([]models.MediaFile, error) {
	if len(ids) == 0 {
		return []models.MediaFile{}, nil
	}
	var items []models.MediaFile
	if err := s.db.Where("space_id = ? AND id IN ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), ids).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListDeletedMediaFiles 列出全部已软删的媒体文件（回收站，FR-25），按软删时间倒序。
func (s *Service) ListDeletedMediaFiles() ([]models.MediaFile, error) {
	return s.ListDeletedMediaFilesInSpace(models.DefaultSpaceID)
}

// ListDeletedMediaFilesInSpace 列出指定 Space 已软删的媒体文件（回收站，FR-25）。
func (s *Service) ListDeletedMediaFilesInSpace(spaceID string) ([]models.MediaFile, error) {
	var items []models.MediaFile
	if err := s.db.Where("space_id = ? AND deleted_at IS NOT NULL", normalizeSpaceID(spaceID)).
		Order("deleted_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// RestoreMediaFile 从回收站还原媒体文件（FR-25）：清空 deleted_at 回到常规列表。
func (s *Service) RestoreMediaFile(id int64) error {
	return s.RestoreMediaFileInSpace(models.DefaultSpaceID, id)
}

// RestoreMediaFileInSpace 从指定 Space 回收站还原媒体文件（FR-25）。
func (s *Service) RestoreMediaFileInSpace(spaceID string, id int64) error {
	spaceID = normalizeSpaceID(spaceID)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var before models.MediaFile
		if err := tx.Where("space_id = ? AND id = ? AND deleted_at IS NOT NULL", spaceID, id).First(&before).Error; err != nil {
			return fmt.Errorf("回收站中不存在该媒体文件")
		}
		if err := tx.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Update("deleted_at", nil).Error; err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.restored",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", before.ID),
			Before:       mediaAuditPayload(&before),
			After:        map[string]any{"deleted_at": nil},
		})
	})
}

// RenameMediaFile 重命名媒体文件：磁盘改名 + 更新数据库 + 失效旧缩略图。
// newName 仅允许单层文件名（不含路径分隔符），目标已存在时拒绝。
// 先磁盘原子改名，再更新数据库；数据库更新失败时尽力回滚磁盘改名。
func (s *Service) RenameMediaFile(id int64, newName string) (*models.MediaFile, error) {
	return s.RenameMediaFileInSpace(models.DefaultSpaceID, id, newName)
}

// RenameMediaFileInSpace 重命名指定 Space 的媒体文件。
func (s *Service) RenameMediaFileInSpace(spaceID string, id int64, newName string) (*models.MediaFile, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == "." || newName == ".." || strings.ContainsAny(newName, "/\\") {
		return nil, ErrInvalidNewName
	}

	spaceID = normalizeSpaceID(spaceID)
	mf, err := s.GetMediaFileByIDInSpace(spaceID, id)
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
	if _, dupErr := s.GetMediaFileByLibraryAndPathInSpace(spaceID, mf.LibraryID, newPathSlash); dupErr == nil {
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
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Updates(updates).Error; err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.renamed",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", mf.ID),
			Before:       mediaAuditPayload(mf),
			After:        map[string]any{"file_path": newPathSlash, "file_name": newName, "format": format},
		})
	}); err != nil {
		if rbErr := os.Rename(newDiskPath, oldDiskPath); rbErr != nil {
			log.Printf("[ERROR] 重命名回滚磁盘文件失败: %s -> %s, err=%v", newDiskPath, oldDiskPath, rbErr)
		}
		return nil, fmt.Errorf("更新媒体文件记录失败: %w", err)
	}

	// FR2-028 缩略图按 Space/media ID 寻址，重命名不改变缓存键，也无需进程内重新生成。
	mf.FilePath = newPathSlash
	mf.FileName = newName
	mf.Format = format
	return mf, nil
}

// MoveMediaFile 移动媒体文件到同媒体库内的目标目录。
func (s *Service) MoveMediaFile(id int64, targetDir string) (*models.MediaFile, error) {
	return s.MoveMediaFileInSpace(models.DefaultSpaceID, id, targetDir)
}

// MoveMediaFileInSpace 移动指定 Space 的媒体文件到同媒体库内目录。
func (s *Service) MoveMediaFileInSpace(spaceID string, id int64, targetDir string) (*models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return nil, ErrInvalidMoveTarget
	}
	mf, err := s.GetMediaFileByIDInSpace(spaceID, id)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(mf.FilePath, "smb://") {
		return nil, ErrMoveUnsupported
	}
	lp, err := s.GetLibraryPathByIDInSpace(spaceID, mf.LibraryID)
	if err != nil {
		return nil, err
	}
	targetDirAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return nil, err
	}
	targetDirSlash := filepath.ToSlash(targetDirAbs)
	libraryRoot := strings.TrimRight(filepath.ToSlash(lp.Path), "/") + "/"
	if targetDirSlash != strings.TrimRight(filepath.ToSlash(lp.Path), "/") && !strings.HasPrefix(targetDirSlash+"/", libraryRoot) {
		return nil, ErrInvalidMoveTarget
	}
	info, err := os.Stat(targetDirAbs)
	if err != nil || !info.IsDir() {
		return nil, ErrInvalidMoveTarget
	}

	oldDiskPath := filepath.FromSlash(mf.FilePath)
	newDiskPath := filepath.Join(targetDirAbs, mf.FileName)
	newPathSlash := filepath.ToSlash(newDiskPath)
	if newPathSlash == mf.FilePath {
		return mf, nil
	}
	if _, statErr := os.Stat(newDiskPath); statErr == nil {
		return nil, ErrRenameTargetExists
	}

	if err := os.Rename(oldDiskPath, newDiskPath); err != nil {
		return nil, fmt.Errorf("移动磁盘文件失败: %w", err)
	}
	updates := map[string]any{"file_path": newPathSlash, "modified_at": time.Now()}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Updates(updates).Error; err != nil {
			return err
		}
		return s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.moved",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", mf.ID),
			Before:       mediaAuditPayload(mf),
			After:        map[string]any{"file_path": newPathSlash},
		})
	}); err != nil {
		if rbErr := os.Rename(newDiskPath, oldDiskPath); rbErr != nil {
			log.Printf("[ERROR] 移动回滚磁盘文件失败: %s -> %s, err=%v", newDiskPath, oldDiskPath, rbErr)
		}
		return nil, fmt.Errorf("更新媒体文件记录失败: %w", err)
	}
	// FR2-028 缩略图按 Space/media ID 寻址，移动不改变缓存键，也无需进程内重新生成。
	mf.FilePath = newPathSlash
	return mf, nil
}

// WritebackMediaMetadata 重新提取媒体元数据并回写到库内记录。
func (s *Service) WritebackMediaMetadata(id int64) (*models.MediaFile, error) {
	return s.WritebackMediaMetadataInSpace(models.DefaultSpaceID, id)
}

// WritebackMediaMetadataInSpace 重新提取指定媒体的元数据并回写到库内记录。
func (s *Service) WritebackMediaMetadataInSpace(spaceID string, id int64) (*models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	before, err := s.GetMediaFileByIDInSpace(spaceID, id)
	if err != nil {
		return nil, err
	}
	if err := validateMetadataWritebackSource(before.FilePath); err != nil {
		if auditErr := s.recordMetadataWritebackFailure(spaceID, before, err); auditErr != nil {
			return nil, auditErr
		}
		return nil, err
	}

	after := *before
	enrichMediaMetadataFn(&after)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.recordMetadataWritebackTx(tx, spaceID, before, "metadata.writeback.started", ""); err != nil {
			return err
		}
		if err := tx.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Updates(metadataWritebackUpdates(&after)).Error; err != nil {
			return err
		}
		return s.recordMetadataWritebackTx(tx, spaceID, &after, "metadata.writeback.succeeded", "")
	}); err != nil {
		return nil, err
	}
	return s.GetMediaFileByIDInSpace(spaceID, id)
}

func (s *Service) recordAuditTx(tx *gorm.DB, input audit.EventInput) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordTx(context.Background(), tx, input)
}

func validateMetadataWritebackSource(filePath string) error {
	if strings.HasPrefix(filePath, "smb://") {
		return nil
	}
	if _, err := os.Stat(filepath.FromSlash(filePath)); err != nil {
		return fmt.Errorf("源文件不可访问: %w", err)
	}
	return nil
}

func (s *Service) recordMetadataWritebackFailure(spaceID string, mf *models.MediaFile, cause error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.recordMetadataWritebackTx(tx, spaceID, mf, "metadata.writeback.started", ""); err != nil {
			return err
		}
		return s.recordMetadataWritebackTx(tx, spaceID, mf, "metadata.writeback.failed", cause.Error())
	})
}

func (s *Service) recordMetadataWritebackTx(tx *gorm.DB, spaceID string, mf *models.MediaFile, action, errText string) error {
	metadata := map[string]any{"summary": "媒体元数据回写", "file_path": mf.FilePath}
	if errText != "" {
		metadata["error"] = errText
	}
	return s.recordAuditTx(tx, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      spaceID,
		ActorType:    audit.ActorSystem,
		Action:       action,
		ResourceType: "media",
		ResourceID:   fmt.Sprintf("%d", mf.ID),
		After:        mediaMetadataAuditPayload(mf),
		Metadata:     metadata,
	})
}

func libraryAuditPayload(lp *models.LibraryPath) map[string]any {
	return map[string]any{
		"id":                   lp.ID,
		"space_id":             lp.SpaceID,
		"path":                 lp.Path,
		"type":                 lp.Type,
		"library_kind":         lp.LibraryKind,
		"library_profile_json": lp.LibraryProfileJSON,
		"label":                lp.Label,
		"enabled":              lp.Enabled,
	}
}

func mediaAuditPayload(mf *models.MediaFile) map[string]any {
	return map[string]any{
		"id":         mf.ID,
		"space_id":   mf.SpaceID,
		"library_id": mf.LibraryID,
		"file_path":  mf.FilePath,
		"file_name":  mf.FileName,
		"deleted_at": mf.DeletedAt,
	}
}

func mediaMetadataAuditPayload(mf *models.MediaFile) map[string]any {
	return map[string]any{
		"duration":          mf.Duration,
		"video_codec":       mf.VideoCodec,
		"audio_codec":       mf.AudioCodec,
		"width":             mf.Width,
		"height":            mf.Height,
		"bitrate":           mf.Bitrate,
		"media_time":        mf.MediaTime,
		"media_time_source": mf.MediaTimeSource,
		"camera":            mf.Camera,
		"lens":              mf.Lens,
		"aperture":          mf.Aperture,
		"shutter":           mf.Shutter,
		"iso":               mf.ISO,
		"gps_lat":           mf.GPSLat,
		"gps_lon":           mf.GPSLon,
		"location":          mf.Location,
	}
}

func metadataWritebackUpdates(mf *models.MediaFile) map[string]any {
	payload := mediaMetadataAuditPayload(mf)
	payload["modified_at"] = time.Now()
	return payload
}

// UpdateDisplayName 设置或清除媒体的库内显示名（FR-30）。
// 仅更新数据库 display_name 列，不动磁盘文件名与路径；去首尾空白后落库，空串表示清除。
// 记录不存在时返回 gorm.ErrRecordNotFound。
func (s *Service) UpdateDisplayName(id int64, displayName string) (*models.MediaFile, error) {
	return s.UpdateDisplayNameInSpace(models.DefaultSpaceID, id, displayName)
}

// UpdateDisplayNameInSpace 设置或清除指定 Space 媒体的库内显示名（FR-30）。
func (s *Service) UpdateDisplayNameInSpace(spaceID string, id int64, displayName string) (*models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	displayName = strings.TrimSpace(displayName)
	result := s.db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Update("display_name", displayName)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		// 区分「媒体不存在」与「值未变化」：再查一次确认存在性
		var count int64
		if err := s.db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	return s.GetMediaFileByIDInSpace(spaceID, id)
}

// UpdateMediaNotes 设置或清除媒体的库内备注（FR-137）。
// 仅更新数据库 notes 列；去首尾空白后落库，空串表示清除备注。
// 记录不存在时返回 gorm.ErrRecordNotFound。
func (s *Service) UpdateMediaNotes(id int64, notes string) (*models.MediaFile, error) {
	return s.UpdateMediaNotesInSpace(models.DefaultSpaceID, id, notes)
}

// UpdateMediaNotesInSpace 设置或清除指定 Space 媒体的库内备注（FR-137）。
func (s *Service) UpdateMediaNotesInSpace(spaceID string, id int64, notes string) (*models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	notes = strings.TrimSpace(notes)
	result := s.db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Update("notes", notes)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		// 区分「媒体不存在」与「值未变化」：再查一次确认存在性
		var count int64
		if err := s.db.Model(&models.MediaFile{}).Where("space_id = ? AND id = ?", spaceID, id).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	return s.GetMediaFileByIDInSpace(spaceID, id)
}

// GetMediaFileByPath 根据文件路径查询媒体文件。
func (s *Service) GetMediaFileByPath(filePath string) (*models.MediaFile, error) {
	return s.GetMediaFileByPathInSpace(models.DefaultSpaceID, filePath)
}

// GetMediaFileByPathInSpace 根据 Space 与文件路径查询媒体文件。
func (s *Service) GetMediaFileByPathInSpace(spaceID, filePath string) (*models.MediaFile, error) {
	filePath = filepath.ToSlash(filePath)
	var mf models.MediaFile
	if err := s.db.Where("space_id = ? AND file_path = ?", normalizeSpaceID(spaceID), filePath).Where(activeFileStateCondition()).First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// GetMediaFileByLibraryAndPath 根据媒体库和文件路径查询媒体文件。
func (s *Service) GetMediaFileByLibraryAndPath(libraryID int64, filePath string) (*models.MediaFile, error) {
	return s.GetMediaFileByLibraryAndPathInSpace(models.DefaultSpaceID, libraryID, filePath)
}

// GetMediaFileByLibraryAndPathInSpace 根据 Space、媒体库和文件路径查询媒体文件。
func (s *Service) GetMediaFileByLibraryAndPathInSpace(spaceID string, libraryID int64, filePath string) (*models.MediaFile, error) {
	filePath = filepath.ToSlash(filePath)
	var mf models.MediaFile
	if err := s.db.Where("space_id = ? AND library_id = ? AND file_path = ?", normalizeSpaceID(spaceID), libraryID, filePath).Where(activeFileStateCondition()).First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

func (s *Service) getMediaFileAnyStateByLibraryAndPathInSpace(spaceID string, libraryID int64, filePath string) (*models.MediaFile, error) {
	filePath = filepath.ToSlash(filePath)
	var mf models.MediaFile
	if err := s.db.Where("space_id = ? AND library_id = ? AND file_path = ?", normalizeSpaceID(spaceID), libraryID, filePath).First(&mf).Error; err != nil {
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

// MarkMediaMissingByLibraryAndPath 标记源文件丢失，不进入回收站、不物理删除记录。
func (s *Service) MarkMediaMissingByLibraryAndPath(spaceID string, libraryID int64, filePath string) error {
	filePath = filepath.ToSlash(filePath)
	spaceID = normalizeSpaceID(spaceID)
	var missing *models.MediaFile
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var before models.MediaFile
		if err := tx.Where("space_id = ? AND library_id = ? AND file_path = ? AND deleted_at IS NULL", spaceID, libraryID, filePath).First(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if before.FileState == models.MediaFileStateMissing {
			return nil
		}
		if err := tx.Model(&models.MediaFile{}).Where("id = ?", before.ID).Update("file_state", models.MediaFileStateMissing).Error; err != nil {
			return err
		}
		if err := s.recordAuditTx(tx, audit.EventInput{
			Scope:        audit.ScopeSpace,
			SpaceID:      spaceID,
			ActorType:    audit.ActorSystem,
			Action:       "media.missing",
			ResourceType: "media",
			ResourceID:   fmt.Sprintf("%d", before.ID),
			Before:       mediaAuditPayload(&before),
			After:        map[string]any{"file_state": models.MediaFileStateMissing},
		}); err != nil {
			return err
		}
		missing = &before
		return nil
	})
	if err != nil || missing == nil {
		return err
	}
	s.notifyScanChange(ScanChange{SpaceID: spaceID, LibraryID: libraryID, Path: filePath, Op: ScanChangeRemoved})
	return nil
}

// ApplyScanChange 执行单路径扫描变更，供 watcher、轮询和增量任务队列统一调用。
func (s *Service) ApplyScanChange(change ScanChange) (int, error) {
	change = NormalizeScanChange(change)
	if change.LibraryID <= 0 || change.Path == "" {
		return 0, nil
	}
	switch change.Op {
	case ScanChangeRemoved:
		return 0, s.MarkMediaMissingByLibraryAndPath(change.SpaceID, change.LibraryID, change.Path)
	case ScanChangeRenamed:
		return s.applyRenameChange(change)
	default:
		return s.applyUpsertChange(change)
	}
}

func (s *Service) applyRenameChange(change ScanChange) (int, error) {
	if change.OldPath != "" {
		var mf models.MediaFile
		err := s.db.Where("space_id = ? AND library_id = ? AND file_path = ? AND deleted_at IS NULL", change.SpaceID, change.LibraryID, change.OldPath).First(&mf).Error
		if err == nil {
			return 1, s.updateExistingMediaFromPath(&mf, change.Path, change)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, err
		}
		if err := s.MarkMediaMissingByLibraryAndPath(change.SpaceID, change.LibraryID, change.OldPath); err != nil {
			return 0, err
		}
	}
	return s.applyUpsertChange(change)
}

func (s *Service) applyUpsertChange(change ScanChange) (int, error) {
	info, err := os.Stat(filepath.FromSlash(change.Path))
	if err != nil {
		return 0, s.MarkMediaMissingByLibraryAndPath(change.SpaceID, change.LibraryID, change.Path)
	}
	if info.IsDir() {
		return 0, nil
	}
	if !s.IsMediaFileForLibrary(change.LibraryID, change.Path) {
		return 0, nil
	}
	existing, err := s.getMediaFileAnyStateByLibraryAndPathInSpace(change.SpaceID, change.LibraryID, change.Path)
	if err == nil {
		return 1, s.updateExistingMediaFromInfo(existing, info, change)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if _, err := s.CreateMediaFileInSpace(change.SpaceID, change.LibraryID, change.Path, info.Size()); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *Service) updateExistingMediaFromPath(mf *models.MediaFile, newPath string, change ScanChange) error {
	info, err := os.Stat(filepath.FromSlash(newPath))
	if err != nil {
		return s.MarkMediaMissingByLibraryAndPath(change.SpaceID, change.LibraryID, mf.FilePath)
	}
	fingerprintChanged := mediaFileFingerprintChanged(mf, info.Size(), info.ModTime())
	updates := map[string]any{
		"file_path":   filepath.ToSlash(newPath),
		"file_name":   filepath.Base(newPath),
		"file_size":   info.Size(),
		"format":      strings.TrimPrefix(filepath.Ext(newPath), "."),
		"modified_at": info.ModTime(),
		"file_state":  models.MediaFileStateAvailable,
	}
	addContentHashStaleUpdate(updates, mf, info.Size(), info.ModTime())
	if err := s.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(updates).Error; err != nil {
		return err
	}
	change.FingerprintChanged = fingerprintChanged
	s.notifyScanChange(change)
	return nil
}

func (s *Service) updateExistingMediaFromInfo(mf *models.MediaFile, info os.FileInfo, change ScanChange) error {
	fingerprintChanged := mediaFileFingerprintChanged(mf, info.Size(), info.ModTime())
	updates := map[string]any{
		"file_size":   info.Size(),
		"modified_at": info.ModTime(),
		"file_state":  models.MediaFileStateAvailable,
	}
	addContentHashStaleUpdate(updates, mf, info.Size(), info.ModTime())
	if err := s.db.Model(&models.MediaFile{}).Where("id = ?", mf.ID).Updates(updates).Error; err != nil {
		return err
	}
	change.FingerprintChanged = fingerprintChanged
	s.notifyScanChange(change)
	return nil
}

func (s *Service) notifyScanChange(change ScanChange) {
	if s.changeHook != nil {
		s.changeHook(change)
	}
}

// BrowseDirectory 按真实磁盘路径跨库浏览子目录与媒体文件（FR-121，取代 ADR-0037）。
// 顶层根（parentPath==BrowseRootMarker）列出各启用库推导出的卷根/共享根；
// 其余按真实路径 P 跨所有库前缀聚合：子目录 = P 下一级目录去重、文件 = 目录恰为 P 的项。
// sortKey 控制文件排序（name/size/type/time，缺省 name）；目录恒在文件前、按名排序。
func (s *Service) BrowseDirectory(parentPath, sortKey string) (*models.BrowseResponse, error) {
	return s.BrowseDirectoryInSpace(models.DefaultSpaceID, parentPath, sortKey)
}

// BrowseDirectoryInSpace 按 Space 浏览真实磁盘路径。
func (s *Service) BrowseDirectoryInSpace(spaceID, parentPath, sortKey string) (*models.BrowseResponse, error) {
	// 顶层根分支：列出各盘符/共享根，子目录不带 library_id
	if parentPath == BrowseRootMarker {
		return s.browseVolumeRootsInSpace(spaceID)
	}

	// 统一路径分隔符为 /，防止 Windows filepath.Clean 把 / 转成 \
	parentPath = strings.ReplaceAll(parentPath, `\`, `/`)
	// 规范化路径，防止路径遍历
	if strings.Contains(parentPath, "..") {
		return nil, fmt.Errorf("非法路径：parentPath 不能包含上级目录引用")
	}

	// 规范化路径：去尾斜杠，加 / 用于前缀匹配
	trimmedPath := strings.TrimRight(parentPath, "/")
	prefix := trimmedPath + "/"

	// 一次 SQL 查询：跨所有库取 file_path 以 prefix 开头、未软删的媒体文件（不按 library_id 收窄）
	allFiles, err := s.mediaRepo.ListMediaByPathPrefix(spaceID, prefix)
	if err != nil {
		return nil, err
	}

	// 构建面包屑（保持 D:/... 正斜杠，不加前导 /）
	breadcrumbs := buildBreadcrumbs(trimmedPath)

	// Go 层聚合：按下一级目录段分组（跨库去重），目录恰为 P 的项归为直属文件
	dirSet := make(map[string]bool)
	files := make([]models.MediaFile, 0)
	for _, f := range allFiles {
		// 去掉 prefix 前缀得相对路径；Windows 路径兼容盘符大小写差异
		rel, ok := trimPathPrefix(f.FilePath, prefix)
		if !ok {
			continue
		}
		if idx := strings.Index(rel, "/"); idx != -1 {
			// 含 / 说明在子目录中，取第一级目录段
			dirSet[rel[:idx]] = true
		} else {
			files = append(files, f)
		}
	}

	// 子目录恒在前、按名升序；子目录跨库故不填 library_id
	dirs := make([]models.DirInfo, 0, len(dirSet))
	for name := range dirSet {
		dirs = append(dirs, models.DirInfo{
			Name: name,
			Path: joinSlashPath(trimmedPath, name),
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })

	// 文件按 sortKey 服务端排序
	sortBrowseFiles(files, sortKey)

	return &models.BrowseResponse{
		Breadcrumbs: breadcrumbs,
		Directories: dirs,
		Files:       files,
	}, nil
}

func (s *Service) browseVolumeRootsInSpace(spaceID string) (*models.BrowseResponse, error) {
	paths, err := s.mediaRepo.ListLibraryPaths(spaceID)
	if err != nil {
		return nil, err
	}

	// 卷根去重：同盘符/共享下的多个库只列一个根
	seen := make(map[string]bool, len(paths))
	roots := make([]string, 0, len(paths))
	for _, lp := range paths {
		if lp.Enabled != 1 {
			continue
		}
		root := volumeRoot(lp.Path)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	sort.Strings(roots)

	dirs := make([]models.DirInfo, 0, len(roots))
	for _, root := range roots {
		dirs = append(dirs, models.DirInfo{Name: root, Path: root})
	}

	return &models.BrowseResponse{
		Breadcrumbs: []models.BreadcrumbItem{{Name: "全部", Path: BrowseRootMarker}},
		Directories: dirs,
		Files:       make([]models.MediaFile, 0),
	}, nil
}

// volumeRoot 从库路径推导其卷根/共享根（FR-121），结果不含尾斜杠：
//   - UNC/共享：//host/share/... → //host/share（仅取 host+share 两段）
//   - 本地盘符：D:/媒体/... 或 D: → D:
//   - 其余（已是根或无盘符的绝对路径）：返回去尾斜杠后的原值
//
// 纯函数、无副作用，便于穷举测试。
func volumeRoot(path string) string {
	// 统一分隔符为 /，便于按段切分
	p := strings.ReplaceAll(path, `\`, "/")
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	// UNC/共享根：以 // 开头，取 host + share 两段
	if strings.HasPrefix(p, "//") {
		rest := strings.TrimLeft(p, "/")
		segs := strings.SplitN(rest, "/", 3)
		if len(segs) >= 2 && segs[0] != "" && segs[1] != "" {
			return "//" + segs[0] + "/" + segs[1]
		}
		// 不足两段（信息不全），回退去尾斜杠原值
		return strings.TrimRight(p, "/")
	}

	// 本地盘符根：首段形如 D:，取盘符段
	first := p
	if idx := strings.Index(p, "/"); idx != -1 {
		first = p[:idx]
	}
	if isWindowsDrivePart(first) {
		return first
	}

	// 其余：去尾斜杠（保留根 /）后返回
	if trimmed := strings.TrimRight(p, "/"); trimmed != "" {
		return trimmed
	}
	return p
}

// sortBrowseFiles 按 sortKey 就地排序目录浏览的文件列表（FR-121）。
// name/size/type/time 升序，缺省/未知键按 name；用 sort.SliceStable 保证同键稳定。
func sortBrowseFiles(files []models.MediaFile, sortKey string) {
	less := func(i, j int) bool { return files[i].FileName < files[j].FileName }
	switch sortKey {
	case BrowseSortSize:
		less = func(i, j int) bool { return files[i].FileSize < files[j].FileSize }
	case BrowseSortType:
		less = func(i, j int) bool {
			// 同类型再按名，保证类型内有序
			if files[i].Format == files[j].Format {
				return files[i].FileName < files[j].FileName
			}
			return files[i].Format < files[j].Format
		}
	case BrowseSortTime:
		less = func(i, j int) bool { return files[i].ModifiedAt.Before(files[j].ModifiedAt) }
	}
	sort.SliceStable(files, less)
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
	p = strings.TrimLeft(p, `\`)
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.Trim(p, "/")
}

// ScanLibrary 异步扫描指定本地目录，立即返回。默认增量模式（FR-27）。
func (s *Service) ScanLibrary(libraryID int64, dirPath string) {
	s.StartAsyncScan(libraryID, dirPath, "local", ScanModeIncremental)
}

// ScanLibraryWithType 按类型同步扫描指定目录，索引所有媒体文件。
// mode 为增量（ScanModeIncremental）或全量（ScanModeFull）：全量模式在本地扫描后对账，
// 库内 active 记录源文件已不存在时标记 missing。供 watcher 轮询等内部同步调用使用。
func (s *Service) ScanLibraryWithType(libraryID int64, dirPath, dirType, mode string) (int, error) {
	spaceID, err := s.spaceIDForLibrary(libraryID)
	if err != nil {
		return 0, err
	}
	return s.ScanLibraryWithTypeInSpace(spaceID, libraryID, dirPath, dirType, mode)
}

// ScanLibraryWithTypeInSpace 按 Space 同步扫描指定目录。
func (s *Service) ScanLibraryWithTypeInSpace(spaceID string, libraryID int64, dirPath, dirType, mode string) (int, error) {
	scanCtx, err := s.ScanContextForLibraryInSpace(spaceID, libraryID)
	if err != nil {
		return 0, err
	}
	switch dirType {
	case "smb":
		// SMB 远程列举不保证完整，不做对账以免误删（FR-27 设计）
		return s.scanSMBLibrary(scanCtx, dirPath)
	default:
		return s.scanLocalLibrary(scanCtx, dirPath, mode)
	}
}

// ScanContextForLibraryInSpace 读取扫描上下文；历史测试库缺少目录记录时回退 mixed。
func (s *Service) ScanContextForLibraryInSpace(spaceID string, libraryID int64) (ScanContext, error) {
	scanCtx := ScanContext{
		SpaceID:     normalizeSpaceID(spaceID),
		LibraryID:   libraryID,
		LibraryKind: models.LibraryKindMixed,
	}
	if !s.db.Migrator().HasTable(&models.LibraryPath{}) {
		return scanCtx, nil
	}
	var lp models.LibraryPath
	err := s.db.Select("library_kind").Where("space_id = ? AND id = ?", scanCtx.SpaceID, libraryID).First(&lp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return scanCtx, nil
	}
	if err != nil {
		return ScanContext{}, err
	}
	kind, err := normalizeLibraryKind(lp.LibraryKind)
	if err != nil {
		return ScanContext{}, err
	}
	scanCtx.LibraryKind = kind
	return scanCtx, nil
}

// StartAsyncScan 按类型启动异步扫描，立即返回。
// 实际扫描在后台 goroutine 中执行，进度通过 GetScanStatus 查询。
func (s *Service) StartAsyncScan(libraryID int64, dirPath, dirType, mode string) {
	spaceID, err := s.spaceIDForLibrary(libraryID)
	if err != nil {
		log.Printf("[ERROR] 异步扫描启动失败：媒体库 Space 不存在: libraryID=%d, err=%v", libraryID, err)
		return
	}
	s.StartAsyncScanInSpace(spaceID, libraryID, dirPath, dirType, mode)
}

// StartAsyncScanInSpace 按 Space 启动异步扫描，立即返回。
func (s *Service) StartAsyncScanInSpace(spaceID string, libraryID int64, dirPath, dirType, mode string) {
	s.StartAsyncScanInSpaceWithSuccess(spaceID, libraryID, dirPath, dirType, mode, nil)
}

// StartAsyncScanInSpaceWithSuccess 按 Space 启动异步扫描，并在扫描成功后执行回调。
func (s *Service) StartAsyncScanInSpaceWithSuccess(spaceID string, libraryID int64, dirPath, dirType, mode string, onSuccess func()) {
	updateScanStatus(func(ss *ScanStatus) {
		*ss = ScanStatus{
			Status:       "scanning",
			SpaceID:      normalizeSpaceID(spaceID),
			LibraryID:    libraryID,
			CurrentPath:  "",
			TotalFiles:   0,
			ScannedFiles: 0,
			StartedAt:    time.Now(),
		}
	})

	go func() {
		count, err := s.ScanLibraryWithTypeInSpace(spaceID, libraryID, dirPath, dirType, mode)

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
		if onSuccess != nil {
			onSuccess()
		}
	}()
}

// scanLocalLibrary 扫描本地目录。
// mode 为全量（ScanModeFull）时在入库后对账：库内 active 记录源文件不存在时标记 missing。
func (s *Service) scanLocalLibrary(scanCtx ScanContext, dirPath, mode string) (int, error) {
	policy, err := s.mediaExtensionPolicyInSpace(scanCtx.SpaceID, scanCtx.LibraryID)
	if err != nil {
		return 0, err
	}

	const scanBatchSize = 500
	var batch []string
	count := 0
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := s.indexMediaFiles(scanCtx, batch)
		count += n
		batch = batch[:0]
		return err
	}
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
		batch = append(batch, path)
		total++
		updateScanStatus(func(ss *ScanStatus) {
			ss.TotalFiles = total
		})
		if len(batch) >= scanBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		// 遍历整体失败则现存集合不完整，放弃对账以免误删，直接上报错误
		return 0, err
	}
	if err := flush(); err != nil {
		return count, err
	}

	if mode == ScanModeFull {
		if err := s.reconcileMissingInSpace(scanCtx.SpaceID, scanCtx.LibraryID); err != nil {
			return count, err
		}
	}

	return count, nil
}

func (s *Service) reconcileMissingInSpace(spaceID string, libraryID int64) error {
	const reconcileBatchSize = 500
	spaceID = normalizeSpaceID(spaceID)
	lastID := int64(0)
	missingCount := 0
	for {
		var rows []models.MediaFile
		if err := s.db.Select("id", "space_id", "library_id", "file_path", "file_name", "file_state").
			Where("space_id = ? AND library_id = ? AND deleted_at IS NULL AND id > ? AND "+activeFileStateCondition(), spaceID, libraryID, lastID).
			Order("id ASC").
			Limit(reconcileBatchSize).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for i := range rows {
			lastID = rows[i].ID
			if _, err := os.Stat(filepath.FromSlash(rows[i].FilePath)); err == nil {
				continue
			}
			if err := s.MarkMediaMissingByLibraryAndPath(spaceID, libraryID, rows[i].FilePath); err != nil {
				return err
			}
			missingCount++
		}
	}
	if missingCount > 0 {
		log.Printf("[INFO] 全量扫描对账：库 %d 标记 %d 条源文件缺失记录", libraryID, missingCount)
	}
	return nil
}

// scanSMBLibrary 扫描 SMB 共享目录。
func (s *Service) scanSMBLibrary(scanCtx ScanContext, smbPath string) (int, error) {
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

	policy, err := s.mediaExtensionPolicyInSpace(scanCtx.SpaceID, scanCtx.LibraryID)
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
	defer func() { _ = client.Disconnect() }()

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

	return s.indexSMBMediaFiles(scanCtx.SpaceID, scanCtx.LibraryID, paths, smbFS)
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
func (s *Service) indexMediaFiles(scanCtx ScanContext, paths []string) (int, error) {
	// 统一所有路径为正斜杠，保证跨平台查询和去重一致
	normalizedPaths := make([]string, len(paths))
	for i, p := range paths {
		normalizedPaths[i] = filepath.ToSlash(p)
	}

	// 批量查询已有记录，避免 N+1 查询
	var existingFiles []models.MediaFile
	if err := s.db.Where("space_id = ? AND library_id = ? AND file_path IN ?", scanCtx.SpaceID, scanCtx.LibraryID, normalizedPaths).Find(&existingFiles).Error; err != nil {
		return 0, err
	}
	existingSet := make(map[string]models.MediaFile, len(existingFiles))
	for _, f := range existingFiles {
		existingSet[f.FilePath] = f
	}

	// 先确定待入库的新文件固定列表（去重后），再有界并发处理。
	type pendingFile struct {
		fullPath   string
		normalized string
	}
	pending := make([]pendingFile, 0, len(paths))
	for i, fullPath := range paths {
		normalized := normalizedPaths[i]
		if existing, ok := existingSet[normalized]; ok {
			if info, err := os.Stat(fullPath); err == nil && (existing.FileState != models.MediaFileStateAvailable || existing.FileSize != info.Size() || !existing.ModifiedAt.Equal(info.ModTime())) {
				change := NormalizeScanChange(ScanChange{
					SpaceID:   scanCtx.SpaceID,
					LibraryID: scanCtx.LibraryID,
					Path:      normalized,
					Op:        ScanChangeModified,
				})
				if err := s.updateExistingMediaFromInfo(&existing, info, change); err != nil {
					log.Printf("[WARN] 媒体文件刷新失败: %s, err=%v", normalized, err)
				}
			}
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
			if _, err := s.CreateMediaFileInSpace(scanCtx.SpaceID, scanCtx.LibraryID, pf.normalized, info.Size()); err != nil {
				log.Printf("[WARN] 媒体文件入库失败: %s, err=%v", pf.normalized, err)
				return
			}
			// SQLite WAL 串行写、计数走原子，进度状态经 updateScanStatus 互斥更新，均并发安全
			done := atomic.AddInt64(&count, 1)

			// 缩略图按需或由 thumbnail.backfill 批量生成，扫描只负责索引入库。

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
func (s *Service) indexSMBMediaFiles(spaceID string, libraryID int64, paths []string, smbFS *smb.FS) (int, error) {
	var existingFiles []models.MediaFile
	if err := s.db.Where("space_id = ? AND library_id = ? AND file_path IN ?", normalizeSpaceID(spaceID), libraryID, paths).Find(&existingFiles).Error; err != nil {
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

		if _, err := s.CreateMediaFileInSpace(spaceID, libraryID, smbPath, info.Size()); err != nil {
			log.Printf("[WARN] SMB 媒体文件入库失败: %s, err=%v", smbPath, err)
			continue
		}
		count++
	}
	return count, nil
}

// ListMediaExtensions 查询指定媒体库的媒体后缀。
func (s *Service) ListMediaExtensions(libraryID int64) ([]models.MediaExtension, error) {
	return s.ListMediaExtensionsInSpace(models.DefaultSpaceID, libraryID)
}

// ListMediaExtensionsInSpace 查询指定 Space 媒体库的媒体后缀。
func (s *Service) ListMediaExtensionsInSpace(spaceID string, libraryID int64) ([]models.MediaExtension, error) {
	if _, err := s.GetLibraryPathByIDInSpace(spaceID, libraryID); err != nil {
		return nil, err
	}
	if s.mediaTypeRuleTableExists() {
		rules, err := s.resolveMediaTypeRules(spaceID, libraryID, false)
		if err != nil {
			return nil, err
		}
		items := make([]models.MediaExtension, 0, len(rules))
		for _, rule := range rules {
			items = append(items, mediaExtensionFromRule(libraryID, rule))
		}
		return sortedMediaExtensions(items), nil
	}
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
	return s.AddMediaExtensionInSpace(models.DefaultSpaceID, libraryID, extension, mediaType)
}

// AddMediaExtensionInSpace 添加指定 Space 媒体库的自定义后缀。
func (s *Service) AddMediaExtensionInSpace(spaceID string, libraryID int64, extension, mediaType string) error {
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
	if _, err := s.GetLibraryPathByIDInSpace(spaceID, libraryID); err != nil {
		return err
	}
	if _, ok := builtInMediaExtensions[ext]; ok {
		return nil
	}
	if s.mediaTypeRuleTableExists() {
		_, err := s.CreateMediaTypeRuleInSpace(spaceID, MediaTypeRuleInput{
			LibraryID: &libraryID,
			Type:      mediaType,
			Extension: ext,
			Label:     strings.ToUpper(ext) + " 自定义",
			Enabled:   boolPtr(true),
		})
		return err
	}

	item := models.MediaExtension{LibraryID: libraryID, Extension: ext, Type: mediaType, IsBuiltIn: 0}
	return s.db.Where(models.MediaExtension{LibraryID: libraryID, Extension: ext}).Attrs(item).FirstOrCreate(&item).Error
}

// DeleteMediaExtension 删除媒体库自定义后缀（FR-64）：仅删自定义后缀，内置后缀不可删，
// 删除不存在的自定义后缀返回错误。
func (s *Service) DeleteMediaExtension(libraryID int64, extension string) error {
	return s.DeleteMediaExtensionInSpace(models.DefaultSpaceID, libraryID, extension)
}

// DeleteMediaExtensionInSpace 删除指定 Space 媒体库的自定义后缀。
func (s *Service) DeleteMediaExtensionInSpace(spaceID string, libraryID int64, extension string) error {
	ext := normalizeExtension(extension)
	if libraryID <= 0 {
		return fmt.Errorf("媒体库 ID 无效")
	}
	if ext == "" {
		return fmt.Errorf("后缀格式不支持")
	}
	if _, ok := builtInMediaExtensions[ext]; ok {
		return fmt.Errorf("内置后缀不可删除")
	}
	if _, err := s.GetLibraryPathByIDInSpace(spaceID, libraryID); err != nil {
		return err
	}
	if s.mediaTypeRuleTableExists() {
		rules, err := s.resolveMediaTypeRules(spaceID, libraryID, true)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if rule.LibraryID != nil && *rule.LibraryID == libraryID && rule.Extension == ext && !rule.Builtin {
				return s.DeleteMediaTypeRuleInSpace(spaceID, rule.ID)
			}
		}
		return fmt.Errorf("自定义后缀不存在")
	}

	result := s.db.Where("library_id = ? AND extension = ?", libraryID, ext).Delete(&models.MediaExtension{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("自定义后缀不存在")
	}
	return nil
}

// IsMediaFile 判断文件是否为内置支持的媒体文件。
func (s *Service) IsMediaFile(path string) bool {
	_, ok := mediaTypeByPathFromBuiltins(path)
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
	return s.MediaTypeByPathInSpace(models.DefaultSpaceID, libraryID, path)
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
