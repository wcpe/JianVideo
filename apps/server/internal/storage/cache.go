// Package storage 提供缓存资产登记、盘点与清理能力。
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// CacheKindThumbnail 等常量定义缓存类型、资产层级和缓存任务类型。
const (
	CacheKindThumbnail       = "thumbnail"
	CacheKindHLS             = "hls"
	CacheKindImageProxy      = "image_proxy"
	CacheKindCover           = "cover"
	CacheKindMetadataTemp    = "metadata_temp"
	CacheKindTimelinePreview = "timeline_preview"

	CacheAssetLevelFile      = "file"
	CacheAssetLevelDirectory = "directory"

	TaskTypeCacheInventory = "cache.inventory"
	TaskTypeCacheClean     = "cache.clean"

	cacheTaskMaxAttempts = 3
)

// ErrUnsafeCachePath 表示缓存路径不在允许管理的目录内。
var (
	ErrUnsafeCachePath = errors.New("缓存路径不在白名单内")
	ErrInvalidKind     = errors.New("缓存类型无效")
)

// Service 管理缓存资产元数据、盘点和清理任务。
type Service struct {
	db         *gorm.DB
	data       string
	audit      audit.Recorder
	tasks      *tasksvc.Service
	now        func() time.Time
	removeAll  func(string) error
	workerGate chan struct{}
}

// RegisterInput 描述缓存文件或目录登记所需的字段。
type RegisterInput struct {
	SpaceID   string
	LibraryID int64
	MediaID   int64
	Kind      string
	ProfileID string
	Variant   string
	CacheKey  string
	Path      string
}

// PreparedDirectoryAsset 保存事务外完成安全校验和目录测量后的缓存资产。
type PreparedDirectoryAsset struct {
	asset    models.CacheAsset
	dataRoot string
}

// SummaryQuery 描述缓存汇总筛选条件。
type SummaryQuery struct {
	SpaceID   string
	LibraryID int64
	MediaID   int64
	Kind      string
}

// Summary 汇总缓存总体占用与类型分布。
type Summary struct {
	TotalSizeBytes int64                    `json:"total_size_bytes"`
	TotalFileCount int64                    `json:"total_file_count"`
	TotalAssets    int64                    `json:"total_assets"`
	ByKind         map[string]SummaryByKind `json:"by_kind"`
}

// SummaryByKind 汇总单一缓存类型的占用情况。
type SummaryByKind struct {
	Kind          string `json:"kind"`
	SizeBytes     int64  `json:"size_bytes"`
	FileCount     int64  `json:"file_count"`
	AssetCount    int64  `json:"asset_count"`
	Rebuildable   bool   `json:"rebuildable"`
	LastAccessed  string `json:"last_accessed,omitempty"`
	LastCreatedAt string `json:"last_created_at,omitempty"`
}

type summaryRow struct {
	Kind       string
	SizeBytes  int64
	FileCount  int64
	AssetCount int64
}

// AssetQuery 描述缓存资产分页查询条件。
type AssetQuery struct {
	SpaceID   string
	Kind      string
	LibraryID int64
	MediaID   int64
	Page      int
	PageSize  int
}

// AssetPage 表示缓存资产分页结果。
type AssetPage struct {
	Items    []models.CacheAsset `json:"items"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int64               `json:"total"`
}

// InventoryInput 描述缓存盘点输入。
type InventoryInput struct {
	SpaceID string
}

type inventoryTaskPayload struct {
	SpaceID string `json:"space_id"`
}

// InventoryResult 表示缓存盘点结果。
type InventoryResult struct {
	Discovered int64 `json:"discovered,omitempty"`
	Missing    int64 `json:"missing,omitempty"`
	TaskID     int64 `json:"task_id,omitempty"`
}

// CleanInput 描述缓存清理输入。
type CleanInput struct {
	SpaceID string   `json:"space_id"`
	Kinds   []string `json:"kinds"`
	DryRun  bool     `json:"dry_run"`
}

type cleanTaskPayload struct {
	SpaceID string   `json:"space_id"`
	Kinds   []string `json:"kinds"`
}

// CleanResult 表示缓存清理或预览结果。
type CleanResult struct {
	DryRun           bool   `json:"dry_run"`
	TaskID           int64  `json:"task_id,omitempty"`
	CandidateCount   int64  `json:"candidate_count"`
	TotalSizeBytes   int64  `json:"total_size_bytes"`
	TotalFileCount   int64  `json:"total_file_count"`
	DeletedCount     int64  `json:"deleted_count"`
	DeletedSizeBytes int64  `json:"deleted_size_bytes"`
	FailedCount      int64  `json:"failed_count"`
	Error            string `json:"error,omitempty"`
}

// NewService 创建缓存资产服务。
func NewService(db *gorm.DB, dataDir string) *Service {
	return &Service{
		db:         db,
		data:       cleanAbs(dataDir),
		now:        time.Now,
		removeAll:  os.RemoveAll,
		workerGate: make(chan struct{}, 1),
	}
}

// WithAudit 配置缓存操作审计记录器。
func (s *Service) WithAudit(rec audit.Recorder) *Service {
	s.audit = rec
	return s
}

// WithTasks 配置缓存盘点与清理任务记录服务。
func (s *Service) WithTasks(tasks *tasksvc.Service) *Service {
	s.tasks = tasks
	return s
}

// RegisterWorkers 注册缓存盘点与清理任务处理器。
func (s *Service) RegisterWorkers(registry *tasksvc.WorkerRegistry) error {
	if s.tasks == nil {
		return errors.New("缓存任务服务未配置")
	}
	if registry == nil {
		return errors.New("缓存 worker 注册表不能为空")
	}
	if err := registry.Register(TaskTypeCacheInventory, 1, s.handleInventoryTask); err != nil {
		return err
	}
	return registry.Register(TaskTypeCacheClean, 1, s.handleCleanTask)
}

func (s *Service) handleInventoryTask(ctx context.Context, task models.Task) error {
	release, err := s.enterWorker(ctx)
	if err != nil {
		return err
	}
	defer release()

	var payload inventoryTaskPayload
	if err := decodeTaskPayload(task.PayloadJSON, &payload); err != nil {
		return err
	}
	spaceID, err := s.validateTaskEnvelope(ctx, task, TaskTypeCacheInventory, "inventory", payload.SpaceID)
	if err != nil {
		return err
	}
	if err := s.updateTaskProgress(ctx, task.ID, 5, "已校验盘点任务"); err != nil {
		return err
	}
	discovered, err := s.scanInventoryKinds(ctx, spaceID, task.ID)
	if err != nil {
		return err
	}
	missing, err := s.markMissing(ctx, spaceID)
	if err != nil {
		return err
	}
	if err := s.updateTaskProgress(ctx, task.ID, 90, "已同步缺失状态"); err != nil {
		return err
	}
	result := InventoryResult{Discovered: discovered, Missing: missing, TaskID: task.ID}
	if err := s.recordAudit(ctx, spaceID, "cache.inventory", result); err != nil {
		return err
	}
	return s.updateTaskProgress(ctx, task.ID, 95, "已写入盘点审计")
}

func (s *Service) handleCleanTask(ctx context.Context, task models.Task) error {
	release, err := s.enterWorker(ctx)
	if err != nil {
		return err
	}
	defer release()

	var payload cleanTaskPayload
	if err := decodeTaskPayload(task.PayloadJSON, &payload); err != nil {
		return err
	}
	spaceID, err := s.validateTaskEnvelope(ctx, task, TaskTypeCacheClean, "clean", payload.SpaceID)
	if err != nil {
		return err
	}
	kinds, err := normalizeKinds(payload.Kinds)
	if err != nil {
		return err
	}
	slices.Sort(kinds)
	if err := s.updateTaskProgress(ctx, task.ID, 5, "已校验清理任务"); err != nil {
		return err
	}
	return s.executeCleanTask(ctx, task.ID, spaceID, kinds)
}

func (s *Service) enterWorker(ctx context.Context) (func(), error) {
	select {
	case s.workerGate <- struct{}{}:
		return func() { <-s.workerGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) validateTaskEnvelope(ctx context.Context, task models.Task, taskType, resourceID, payloadSpaceID string) (string, error) {
	if task.Type != taskType {
		return "", fmt.Errorf("缓存任务类型不匹配: %s", task.Type)
	}
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return "", errors.New("缓存任务必须归属 Space")
	}
	spaceID := strings.TrimSpace(*task.SpaceID)
	if strings.TrimSpace(payloadSpaceID) == "" || payloadSpaceID != spaceID {
		return "", errors.New("缓存任务 payload 与 Space 不匹配")
	}
	if task.ResourceType != "cache" || task.ResourceID != resourceID {
		return "", errors.New("缓存任务资源信息无效")
	}
	if err := s.validateSpace(ctx, spaceID); err != nil {
		return "", err
	}
	return spaceID, nil
}

func (s *Service) enqueueCacheTask(ctx context.Context, taskType, spaceID string, payload any, resourceID, key string) (*models.Task, error) {
	if s.tasks == nil {
		return nil, errors.New("缓存任务服务未配置")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码缓存任务 payload 失败: %w", err)
	}
	return s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        spaceID,
		Type:           taskType,
		MaxAttempts:    cacheTaskMaxAttempts,
		IdempotencyKey: key,
		PayloadJSON:    string(data),
		ResourceType:   "cache",
		ResourceID:     resourceID,
	})
}

func (s *Service) updateTaskProgress(ctx context.Context, taskID int64, progress int, checkpoint string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.tasks.UpdateProgress(ctx, taskID, tasksvc.ProgressInput{Progress: progress, Checkpoint: checkpoint})
}

func (s *Service) validateSpace(ctx context.Context, spaceID string) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Space{}).Where("id = ?", spaceID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("指定 Space 不存在: %s", spaceID)
	}
	return nil
}

func decodeTaskPayload(raw string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("缓存任务 payload 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("缓存任务 payload 包含多余内容")
	}
	return nil
}

// RegisterFile 登记单个缓存文件。
func (s *Service) RegisterFile(ctx context.Context, input RegisterInput) (*models.CacheAsset, error) {
	return s.register(ctx, input, CacheAssetLevelFile)
}

// RegisterDirectory 登记缓存目录。
func (s *Service) RegisterDirectory(ctx context.Context, input RegisterInput) (*models.CacheAsset, error) {
	return s.register(ctx, input, CacheAssetLevelDirectory)
}

// PrepareDirectoryAsset 在事务外完成目录安全校验与测量。
func (s *Service) PrepareDirectoryAsset(input RegisterInput) (PreparedDirectoryAsset, error) {
	asset, err := s.prepareAsset(input, CacheAssetLevelDirectory)
	return PreparedDirectoryAsset{asset: asset, dataRoot: s.data}, err
}

// RegisterDirectoryTx 在调用方事务内登记已准备的目录资产。
func (s *Service) RegisterDirectoryTx(ctx context.Context, tx *gorm.DB, prepared PreparedDirectoryAsset) (*models.CacheAsset, error) {
	asset := prepared.asset
	if prepared.dataRoot != s.data || asset.AssetLevel != CacheAssetLevelDirectory || asset.RelativePath == "" {
		return nil, ErrUnsafeCachePath
	}
	if err := s.upsertAssetDB(ctx, tx, &asset); err != nil {
		return nil, err
	}
	return &asset, nil
}

// Summary 统计缓存资产占用。
func (s *Service) Summary(ctx context.Context, query SummaryQuery) (Summary, error) {
	db := s.applySummaryQuery(s.db.WithContext(ctx).Model(&models.CacheAsset{}), query).
		Where("missing_at IS NULL")
	rows, err := scanSummaryRows(db)
	if err != nil {
		return Summary{}, err
	}
	return buildSummary(rows), nil
}

func scanSummaryRows(db *gorm.DB) ([]summaryRow, error) {
	var rows []summaryRow
	err := db.Select("kind, SUM(size_bytes) AS size_bytes, SUM(file_count) AS file_count, COUNT(*) AS asset_count").
		Group("kind").
		Scan(&rows).Error
	return rows, err
}

func buildSummary(rows []summaryRow) Summary {
	out := Summary{ByKind: map[string]SummaryByKind{}}
	for _, kind := range orderedKinds() {
		out.ByKind[kind] = SummaryByKind{Kind: kind, Rebuildable: true}
	}
	for _, row := range rows {
		item := SummaryByKind{
			Kind:        row.Kind,
			SizeBytes:   row.SizeBytes,
			FileCount:   row.FileCount,
			AssetCount:  row.AssetCount,
			Rebuildable: true,
		}
		out.TotalSizeBytes += row.SizeBytes
		out.TotalFileCount += row.FileCount
		out.TotalAssets += row.AssetCount
		out.ByKind[row.Kind] = item
	}
	return out
}

// ListAssets 分页列出缓存资产。
func (s *Service) ListAssets(ctx context.Context, query AssetQuery) (AssetPage, error) {
	page, size := normalizePage(query.Page, query.PageSize)
	db := s.db.WithContext(ctx).Model(&models.CacheAsset{}).Where("space_id = ?", normalizeSpace(query.SpaceID))
	if query.Kind != "" {
		db = db.Where("kind = ?", strings.TrimSpace(query.Kind))
	}
	if query.LibraryID > 0 {
		db = db.Where("library_id = ?", query.LibraryID)
	}
	if query.MediaID > 0 {
		db = db.Where("media_id = ?", query.MediaID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return AssetPage{}, err
	}
	var items []models.CacheAsset
	if err := db.Order("updated_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&items).Error; err != nil {
		return AssetPage{}, err
	}
	return AssetPage{Items: items, Page: page, PageSize: size, Total: total}, nil
}

// Inventory 将缓存盘点任务写入通用任务队列。
func (s *Service) Inventory(ctx context.Context, input InventoryInput) (InventoryResult, error) {
	spaceID := normalizeSpace(input.SpaceID)
	if err := s.validateSpace(ctx, spaceID); err != nil {
		return InventoryResult{}, err
	}
	task, err := s.enqueueCacheTask(ctx, TaskTypeCacheInventory, spaceID, inventoryTaskPayload{SpaceID: spaceID}, "inventory", "cache:inventory:"+spaceID)
	if err != nil {
		return InventoryResult{}, err
	}
	return InventoryResult{TaskID: task.ID}, nil
}

func (s *Service) scanInventoryKinds(ctx context.Context, spaceID string, taskID int64) (int64, error) {
	var discovered int64
	for index, kind := range orderedKinds() {
		if err := ctx.Err(); err != nil {
			return discovered, err
		}
		root := filepath.Join(s.data, kindDirs()[kind])
		n, err := s.inventoryKind(ctx, spaceID, kind, root)
		if err != nil {
			return discovered, err
		}
		discovered += n
		progress := 10 + (index+1)*10
		if err := s.updateTaskProgress(ctx, taskID, progress, "已盘点 "+kind); err != nil {
			return discovered, err
		}
	}
	return discovered, nil
}

func (s *Service) inventoryKind(ctx context.Context, spaceID, kind, root string) (int64, error) {
	switch kind {
	case CacheKindHLS:
		return s.inventoryHLS(ctx, spaceID, root)
	case CacheKindThumbnail:
		return s.inventoryThumbnails(ctx, spaceID, root)
	case CacheKindTimelinePreview:
		return s.inventoryTimelinePreviews(ctx, spaceID, root)
	default:
		return s.inventoryFiles(ctx, spaceID, kind, root)
	}
}

// Clean 同步预览清理范围，真实清理则写入通用任务队列。
func (s *Service) Clean(ctx context.Context, input CleanInput) (CleanResult, error) {
	spaceID := normalizeSpace(input.SpaceID)
	if err := s.validateSpace(ctx, spaceID); err != nil {
		return CleanResult{}, err
	}
	kinds, err := normalizeKinds(input.Kinds)
	if err != nil {
		return CleanResult{}, err
	}
	slices.Sort(kinds)
	if input.DryRun {
		return s.cleanPreview(ctx, spaceID, kinds)
	}
	payload := cleanTaskPayload{SpaceID: spaceID, Kinds: kinds}
	key := "cache:clean:" + spaceID + ":" + strings.Join(kinds, ",")
	task, err := s.enqueueCacheTask(ctx, TaskTypeCacheClean, spaceID, payload, "clean", key)
	if err != nil {
		return CleanResult{}, err
	}
	return CleanResult{TaskID: task.ID}, nil
}

func (s *Service) cleanPreview(ctx context.Context, spaceID string, kinds []string) (CleanResult, error) {
	result := CleanResult{DryRun: true}
	assets, err := s.cleanCandidates(ctx, spaceID, kinds)
	if err != nil {
		return result, err
	}
	result.addCandidates(assets)
	return s.previewClean(ctx, spaceID, result)
}

func (s *Service) cleanCandidates(ctx context.Context, spaceID string, kinds []string) ([]models.CacheAsset, error) {
	var assets []models.CacheAsset
	err := s.db.WithContext(ctx).
		Where("space_id = ? AND kind IN ? AND missing_at IS NULL AND rebuildable = ?", normalizeSpace(spaceID), kinds, true).
		Order("kind ASC, relative_path ASC").
		Find(&assets).Error
	return assets, err
}

func (r *CleanResult) addCandidates(assets []models.CacheAsset) {
	r.CandidateCount = int64(len(assets))
	for _, asset := range assets {
		r.TotalSizeBytes += asset.SizeBytes
		r.TotalFileCount += asset.FileCount
	}
}

func (s *Service) previewClean(ctx context.Context, spaceID string, result CleanResult) (CleanResult, error) {
	err := s.recordAudit(ctx, normalizeSpace(spaceID), "cache.clean.preview", result)
	return result, err
}

func (s *Service) executeCleanTask(ctx context.Context, taskID int64, spaceID string, kinds []string) error {
	assets, err := s.cleanCandidates(ctx, spaceID, kinds)
	if err != nil {
		return err
	}
	result := CleanResult{TaskID: taskID}
	result.addCandidates(assets)
	if err := s.updateTaskProgress(ctx, taskID, 10, "已确认清理候选"); err != nil {
		return err
	}
	libraryRoots, err := s.libraryRoots(ctx)
	if err != nil {
		return err
	}
	if err := s.deleteRegisteredAssets(ctx, assets, libraryRoots, &result); err != nil {
		return err
	}
	if err := s.updateTaskProgress(ctx, taskID, 90, "已删除缓存文件"); err != nil {
		return err
	}
	if result.FailedCount > 0 {
		result.Error = "部分缓存删除失败"
	}
	if err := s.recordAudit(ctx, spaceID, "cache.clean.executed", result); err != nil {
		return err
	}
	if result.FailedCount > 0 {
		return errors.New(result.Error)
	}
	return s.updateTaskProgress(ctx, taskID, 95, "已写入清理审计")
}

// DeleteMediaKindAssetsExcept 只删除指定媒体与缓存类型中不在保留集的白名单资产。
func (s *Service) DeleteMediaKindAssetsExcept(ctx context.Context, spaceID string, mediaID int64, kind string, keepPaths []string) error {
	if !isKnownKind(kind) || mediaID <= 0 {
		return ErrInvalidKind
	}
	keep := make(map[string]struct{}, len(keepPaths))
	for _, path := range keepPaths {
		_, relative, err := s.safePath(path, kind)
		if err != nil {
			return err
		}
		keep[relative] = struct{}{}
	}
	var assets []models.CacheAsset
	if err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND kind = ?", normalizeSpace(spaceID), mediaID, kind).Find(&assets).Error; err != nil {
		return err
	}
	libraryRoots, err := s.libraryRoots(ctx)
	if err != nil {
		return err
	}
	deletedIDs := make([]int64, 0, len(assets))
	for _, asset := range assets {
		if _, ok := keep[asset.RelativePath]; ok {
			continue
		}
		path, err := s.validateDeleteTarget(asset, libraryRoots)
		if err != nil {
			return err
		}
		if err := deleteAssetPath(path, asset.AssetLevel); err != nil && !os.IsNotExist(err) {
			return err
		}
		deletedIDs = append(deletedIDs, asset.ID)
	}
	if len(deletedIDs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Where("id IN ?", deletedIDs).Delete(&models.CacheAsset{}).Error
}

// PrepareHLSRebuild 通过缓存资产安全边界清理单个 HLS profile，绝不删除同媒体其他 profile。
func (s *Service) PrepareHLSRebuild(ctx context.Context, spaceID string, mediaID int64, profileID, path string) error {
	absPath, relPath, err := s.safePath(path, CacheKindHLS)
	if err != nil {
		return err
	}
	libraryRoots, err := s.libraryRoots(ctx)
	if err != nil {
		return err
	}
	asset := models.CacheAsset{
		SpaceID: normalizeSpace(spaceID), MediaID: mediaID, Kind: CacheKindHLS,
		AssetLevel: CacheAssetLevelDirectory, ProfileID: strings.TrimSpace(profileID), RelativePath: relPath,
	}
	if identity, usesAudioNamespace := s.audioHLSIdentity(absPath, asset.SpaceID); usesAudioNamespace {
		if identity.TaskID == "" || identity.MediaID != mediaID || identity.ProfileID != asset.ProfileID {
			return ErrUnsafeCachePath
		}
		asset.Variant = identity.TaskID
	}
	if _, err := s.validateDeleteTarget(asset, libraryRoots); err != nil {
		return err
	}
	if err := deleteAssetPath(absPath, CacheAssetLevelDirectory); err != nil && !os.IsNotExist(err) {
		return err
	}
	var assets []models.CacheAsset
	if err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND kind = ? AND profile_id = ?",
		normalizeSpace(spaceID), mediaID, CacheKindHLS, strings.TrimSpace(profileID)).Find(&assets).Error; err != nil {
		return err
	}
	ids := make([]int64, 0, len(assets))
	prefix := strings.TrimSuffix(filepath.ToSlash(relPath), "/") + "/"
	for _, item := range assets {
		itemPath := filepath.ToSlash(item.RelativePath)
		if itemPath == filepath.ToSlash(relPath) || strings.HasPrefix(itemPath, prefix) {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.CacheAsset{}).Error
}

func (s *Service) deleteRegisteredAssets(ctx context.Context, assets []models.CacheAsset, libraryRoots []string, result *CleanResult) error {
	deletedIDs := make([]int64, 0, len(assets))
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if asset.Kind == CacheKindTimelinePreview {
			if err := s.deleteTimelinePreviewAsset(ctx, asset, libraryRoots); err != nil {
				result.FailedCount++
				continue
			}
			result.recordDeleted(asset)
			continue
		}
		if !s.deleteOrdinaryAsset(asset, libraryRoots) {
			result.FailedCount++
			continue
		}
		deletedIDs = append(deletedIDs, asset.ID)
		result.recordDeleted(asset)
	}
	return s.deleteAssetRows(ctx, deletedIDs)
}

func (r *CleanResult) recordDeleted(asset models.CacheAsset) {
	r.DeletedCount++
	r.DeletedSizeBytes += asset.SizeBytes
}

func (s *Service) deleteOrdinaryAsset(asset models.CacheAsset, libraryRoots []string) bool {
	path, err := s.validateDeleteTarget(asset, libraryRoots)
	if err != nil {
		return false
	}
	err = deleteAssetPath(path, asset.AssetLevel)
	return err == nil || os.IsNotExist(err)
}

func (s *Service) deleteAssetRows(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.CacheAsset{}).Error
}

// CleanUnregisteredTimelinePreviewGeneration 受控清理生成失败后遗留的 generation。
func (s *Service) CleanUnregisteredTimelinePreviewGeneration(ctx context.Context, spaceID string, mediaID int64, profileID, fingerprint, generationID, path string) error {
	input, target, err := s.validateTimelineCompensationTarget(spaceID, mediaID, profileID, fingerprint, generationID, path)
	if err != nil {
		return err
	}
	if err := s.rejectCurrentTimelineGeneration(ctx, input); err != nil {
		return err
	}
	asset, err := s.findTimelineCompensationAsset(ctx, input, target)
	if err == nil {
		err = s.DeleteTimelinePreviewGeneration(ctx, input.SpaceID, asset.ID, generationID)
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		err = s.removeAll(target)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.recordTimelineCompensationAudit(ctx, input)
}

func (s *Service) validateTimelineCompensationTarget(spaceID string, mediaID int64, profileID, fingerprint, generationID, path string) (RegisterInput, string, error) {
	spaceID = normalizeSpace(spaceID)
	target, _, err := s.safePath(path, CacheKindTimelinePreview)
	if err != nil {
		return RegisterInput{}, "", err
	}
	root := filepath.Join(s.data, kindDirs()[CacheKindTimelinePreview])
	input, err := parseTimelinePreviewPath(root, target, spaceID)
	if err != nil || !timelineCompensationMatches(input, mediaID, profileID, fingerprint, generationID) {
		return RegisterInput{}, "", ErrUnsafeCachePath
	}
	return input, target, nil
}

func timelineCompensationMatches(input RegisterInput, mediaID int64, profileID, fingerprint, generationID string) bool {
	return input.MediaID == mediaID && input.ProfileID == profileID &&
		input.Variant == fingerprint+":"+generationID && safeCacheToken(profileID) &&
		safeCacheToken(fingerprint) && safeCacheToken(generationID)
}

func (s *Service) rejectCurrentTimelineGeneration(ctx context.Context, input RegisterInput) error {
	parts := strings.Split(input.Variant, ":")
	var count int64
	err := s.db.WithContext(ctx).Model(&models.MediaTimelinePreview{}).
		Where("space_id = ? AND media_id = ? AND profile_id = ? AND source_fingerprint = ? AND generation_id = ?",
			input.SpaceID, input.MediaID, input.ProfileID, parts[0], parts[1]).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("拒绝清理当前时间轴预览 generation")
	}
	return nil
}

func (s *Service) findTimelineCompensationAsset(ctx context.Context, input RegisterInput, target string) (models.CacheAsset, error) {
	relative, err := filepath.Rel(s.data, target)
	if err != nil || !safeRelativePath(relative) {
		return models.CacheAsset{}, ErrUnsafeCachePath
	}
	var asset models.CacheAsset
	if err := s.db.WithContext(ctx).Where("relative_path = ?", filepath.ToSlash(relative)).First(&asset).Error; err != nil {
		return models.CacheAsset{}, err
	}
	identity, err := s.timelineAssetIdentity(asset)
	if err != nil || !timelineCompensationAssetMatches(identity, input) {
		return models.CacheAsset{}, ErrUnsafeCachePath
	}
	return asset, nil
}

func timelineCompensationAssetMatches(identity timelineAssetIdentity, input RegisterInput) bool {
	parts := strings.Split(input.Variant, ":")
	return len(parts) == 2 && identity.SpaceID == input.SpaceID && identity.MediaID == input.MediaID &&
		identity.ProfileID == input.ProfileID && identity.Fingerprint == parts[0] && identity.GenerationID == parts[1]
}

func (s *Service) recordTimelineCompensationAudit(ctx context.Context, input RegisterInput) error {
	return s.recordAudit(ctx, input.SpaceID, "cache.timeline_preview.compensation_cleanup", map[string]any{
		"media_id": input.MediaID, "profile_id": input.ProfileID, "variant": input.Variant,
	})
}

// DeleteTimelinePreviewGeneration 按 Space、资产与 generation 条件清空当前指针并删除登记和目录。
func (s *Service) DeleteTimelinePreviewGeneration(ctx context.Context, spaceID string, assetID int64, generationID string) error {
	spaceID = normalizeSpace(spaceID)
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return ErrUnsafeCachePath
	}
	var asset models.CacheAsset
	suffix := ":" + generationID
	err := s.db.WithContext(ctx).
		Where("space_id = ? AND id = ? AND kind = ? AND substr(variant, ?) = ?",
			spaceID, assetID, CacheKindTimelinePreview, -len(suffix), suffix).
		First(&asset).Error
	if err != nil {
		return err
	}
	identity, err := s.timelineAssetIdentity(asset)
	if err != nil || identity.GenerationID != generationID {
		return ErrUnsafeCachePath
	}
	roots, err := s.libraryRoots(ctx)
	if err != nil {
		return err
	}
	return s.deleteTimelinePreviewAsset(ctx, asset, roots)
}

type timelineAssetIdentity struct {
	SpaceID, ProfileID, Fingerprint, GenerationID string
	MediaID                                       int64
}

func (s *Service) timelineAssetIdentity(asset models.CacheAsset) (timelineAssetIdentity, error) {
	root := filepath.Join(s.data, kindDirs()[CacheKindTimelinePreview])
	path := filepath.Join(s.data, filepath.FromSlash(asset.RelativePath))
	input, err := parseTimelinePreviewPath(root, path, asset.SpaceID)
	if err != nil {
		return timelineAssetIdentity{}, err
	}
	parts := strings.Split(input.Variant, ":")
	if len(parts) != 2 || !timelineAssetMatches(asset, input) {
		return timelineAssetIdentity{}, ErrUnsafeCachePath
	}
	return timelineAssetIdentity{
		SpaceID: input.SpaceID, MediaID: input.MediaID, ProfileID: input.ProfileID,
		Fingerprint: parts[0], GenerationID: parts[1],
	}, nil
}

func timelineAssetMatches(asset models.CacheAsset, input RegisterInput) bool {
	return asset.Kind == CacheKindTimelinePreview && asset.AssetLevel == CacheAssetLevelDirectory &&
		asset.SpaceID == input.SpaceID && asset.MediaID == input.MediaID &&
		asset.ProfileID == input.ProfileID && asset.Variant == input.Variant && asset.CacheKey == input.CacheKey
}

func (s *Service) deleteTimelinePreviewAsset(ctx context.Context, asset models.CacheAsset, roots []string) error {
	identity, err := s.timelineAssetIdentity(asset)
	if err != nil {
		return err
	}
	path, err := s.validateDeleteTarget(asset, roots)
	if err != nil {
		return err
	}
	if err := s.deleteTimelineAssetRows(ctx, asset.ID, identity.SpaceID, identity.GenerationID); err != nil {
		return err
	}
	if err := s.removeAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Service) deleteTimelineAssetRows(ctx context.Context, assetID int64, spaceID, generationID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clearTimelinePointer(tx, spaceID, assetID, generationID); err != nil {
			return err
		}
		suffix := ":" + generationID
		result := tx.Where("space_id = ? AND id = ? AND kind = ? AND substr(variant, ?) = ?",
			spaceID, assetID, CacheKindTimelinePreview, -len(suffix), suffix).
			Delete(&models.CacheAsset{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func clearTimelinePointer(tx *gorm.DB, spaceID string, assetID int64, generationID string) error {
	return tx.Model(&models.MediaTimelinePreview{}).
		Where("space_id = ? AND asset_id = ? AND generation_id = ?", spaceID, assetID, generationID).
		Updates(map[string]any{
			"asset_id": 0, "source_fingerprint": "", "generation_id": "", "updated_at": time.Now().UTC(),
		}).Error
}

func (s *Service) register(ctx context.Context, input RegisterInput, level string) (*models.CacheAsset, error) {
	asset, err := s.prepareAsset(input, level)
	if err != nil {
		return nil, err
	}
	if err := s.upsertAssetDB(ctx, s.db, &asset); err != nil {
		return nil, err
	}
	return &asset, nil
}

func (s *Service) prepareAsset(input RegisterInput, level string) (models.CacheAsset, error) {
	kind := strings.TrimSpace(input.Kind)
	if !isKnownKind(kind) {
		return models.CacheAsset{}, ErrInvalidKind
	}
	absPath, relPath, err := s.safePath(input.Path, kind)
	if err != nil {
		return models.CacheAsset{}, err
	}
	if err := s.validateHLSInput(input, kind, level, absPath); err != nil {
		return models.CacheAsset{}, err
	}
	if err := s.validateTimelineInput(input, kind, level, absPath); err != nil {
		return models.CacheAsset{}, err
	}
	size, count, err := measurePath(absPath, level)
	if err != nil {
		return models.CacheAsset{}, err
	}
	return s.buildAsset(input, kind, level, relPath, size, count), nil
}

type audioHLSIdentity struct {
	SpaceID   string
	MediaID   int64
	ProfileID string
	TaskID    string
}

func (s *Service) validateHLSInput(input RegisterInput, kind, level, path string) error {
	if kind != CacheKindHLS {
		return nil
	}
	identity, pathUsesAudioNamespace := s.audioHLSIdentity(path, normalizeSpace(input.SpaceID))
	if !isAudioHLSNamespace(input.ProfileID) && !pathUsesAudioNamespace {
		return nil
	}
	if level != CacheAssetLevelDirectory || identity.TaskID == "" {
		return ErrUnsafeCachePath
	}
	if identity.SpaceID != normalizeSpace(input.SpaceID) || identity.MediaID != input.MediaID || identity.ProfileID != strings.TrimSpace(input.ProfileID) {
		return ErrUnsafeCachePath
	}
	if identity.TaskID != strings.TrimSpace(input.Variant) {
		return ErrUnsafeCachePath
	}
	return nil
}

func (s *Service) audioHLSIdentity(path, requestedSpace string) (audioHLSIdentity, bool) {
	root := filepath.Join(s.data, kindDirs()[CacheKindHLS])
	relative, err := filepath.Rel(cleanAbs(root), cleanAbs(path))
	if err != nil || filepath.IsAbs(relative) {
		return audioHLSIdentity{}, false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	pathUsesAudioNamespace := len(parts) >= 3 && isAudioHLSNamespace(parts[2])
	if !pathUsesAudioNamespace || len(parts) != 5 || parts[3] != "tasks" {
		return audioHLSIdentity{}, pathUsesAudioNamespace
	}
	mediaID := parsePositiveInt64(parts[1])
	taskID := parsePositiveInt64(parts[4])
	if parts[0] != requestedSpace || mediaID == 0 || taskID == 0 || !isCanonicalAudioHLSProfile(parts[2]) {
		return audioHLSIdentity{}, true
	}
	return audioHLSIdentity{
		SpaceID: parts[0], MediaID: mediaID, ProfileID: parts[2], TaskID: parts[4],
	}, true
}

func isAudioHLSNamespace(profileID string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(profileID)), "audio-h264-aac-")
}

func isCanonicalAudioHLSProfile(profileID string) bool {
	const prefix = "audio-h264-aac-"
	if len(profileID) != len(prefix)+24 || !strings.HasPrefix(profileID, prefix) {
		return false
	}
	for _, char := range profileID[len(prefix):] {
		if char < '0' || char > '9' && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func (s *Service) validateTimelineInput(input RegisterInput, kind, level, path string) error {
	if kind != CacheKindTimelinePreview {
		return nil
	}
	if level != CacheAssetLevelDirectory {
		return ErrUnsafeCachePath
	}
	parsed, err := parseTimelinePreviewPath(filepath.Join(s.data, kindDirs()[kind]), path, normalizeSpace(input.SpaceID))
	if err != nil {
		return err
	}
	if input.MediaID != parsed.MediaID || strings.TrimSpace(input.ProfileID) != parsed.ProfileID {
		return ErrUnsafeCachePath
	}
	if strings.TrimSpace(input.Variant) != parsed.Variant || strings.TrimSpace(input.CacheKey) != parsed.CacheKey {
		return ErrUnsafeCachePath
	}
	return rejectTimelineSymlinks(path)
}

func rejectTimelineSymlinks(path string) error {
	return filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrUnsafeCachePath
		}
		return nil
	})
}

func (s *Service) buildAsset(input RegisterInput, kind, level, relPath string, size, count int64) models.CacheAsset {
	now := s.now().UTC()
	return models.CacheAsset{
		SpaceID:      normalizeSpace(input.SpaceID),
		LibraryID:    input.LibraryID,
		MediaID:      input.MediaID,
		Kind:         kind,
		AssetLevel:   level,
		ProfileID:    strings.TrimSpace(input.ProfileID),
		Variant:      strings.TrimSpace(input.Variant),
		CacheKey:     strings.TrimSpace(input.CacheKey),
		RelativePath: relPath,
		SizeBytes:    size,
		FileCount:    count,
		Rebuildable:  true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (s *Service) upsertAssetDB(ctx context.Context, db *gorm.DB, asset *models.CacheAsset) error {
	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "relative_path"}},
		DoUpdates: clause.Assignments(map[string]any{
			"space_id":    gorm.Expr("excluded.space_id"),
			"library_id":  preservePositive("library_id"),
			"media_id":    preservePositive("media_id"),
			"kind":        gorm.Expr("excluded.kind"),
			"asset_level": gorm.Expr("excluded.asset_level"),
			"profile_id":  preserveText("profile_id"),
			"variant":     preserveText("variant"),
			"cache_key":   preserveText("cache_key"),
			"size_bytes":  gorm.Expr("excluded.size_bytes"),
			"file_count":  gorm.Expr("excluded.file_count"),
			"rebuildable": gorm.Expr("excluded.rebuildable"),
			"missing_at":  nil,
			"updated_at":  gorm.Expr("excluded.updated_at"),
		}),
	}).Create(asset).Error
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Where("relative_path = ?", asset.RelativePath).First(asset).Error
}

func preservePositive(column string) clause.Expr {
	return gorm.Expr("CASE WHEN excluded." + column + " > 0 THEN excluded." + column + " ELSE cache_assets." + column + " END")
}

func preserveText(column string) clause.Expr {
	return gorm.Expr("CASE WHEN excluded." + column + " <> '' THEN excluded." + column + " ELSE cache_assets." + column + " END")
}

func (s *Service) inventoryThumbnails(ctx context.Context, spaceID, root string) (int64, error) {
	var count int64
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		allowed, skip := thumbnailInventoryPath(root, path, entry, spaceID)
		if skip {
			return filepath.SkipDir
		}
		if !allowed || entry.IsDir() {
			return nil
		}
		if _, err := s.RegisterFile(ctx, RegisterInput{SpaceID: spaceID, Kind: CacheKindThumbnail, Path: path}); err != nil {
			return err
		}
		count++
		return ctx.Err()
	})
	return count, err
}

func thumbnailInventoryPath(root, path string, entry os.DirEntry, spaceID string) (bool, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return false, false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) == 1 {
		return !entry.IsDir() && spaceID == models.DefaultSpaceID, entry.IsDir() && parts[0] != spaceID
	}
	return parts[0] == spaceID, entry.IsDir() && len(parts) == 1 && parts[0] != spaceID
}

func (s *Service) inventoryFiles(ctx context.Context, spaceID, kind, root string) (int64, error) {
	var count int64
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, err := s.RegisterFile(ctx, RegisterInput{SpaceID: spaceID, Kind: kind, Path: path}); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func (s *Service) inventoryHLS(ctx context.Context, spaceID, root string) (int64, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}
	profiles := map[string]RegisterInput{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Name() != "master.m3u8" && entry.Name() != "index.m3u8" {
			return nil
		}
		input, ok := parseHLSInventoryPath(root, path, spaceID)
		if ok {
			profiles[input.Path] = input
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, input := range profiles {
		if _, err := s.RegisterDirectory(ctx, input); err != nil {
			return 0, err
		}
	}
	return int64(len(profiles)), nil
}

func parseHLSInventoryPath(root, manifestPath, requestedSpace string) (RegisterInput, bool) {
	dir := filepath.Dir(manifestPath)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return RegisterInput{}, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 1 {
		mediaID := parseInt64(parts[0])
		return RegisterInput{SpaceID: requestedSpace, MediaID: mediaID, Kind: CacheKindHLS, ProfileID: "h264", Path: dir}, mediaID > 0
	}
	if len(parts) == 5 && parts[0] == requestedSpace && parts[3] == "tasks" && isCanonicalAudioHLSProfile(parts[2]) {
		mediaID := parsePositiveInt64(parts[1])
		taskID := parsePositiveInt64(parts[4])
		return RegisterInput{
			SpaceID: parts[0], MediaID: mediaID, Kind: CacheKindHLS,
			ProfileID: parts[2], Variant: parts[4], Path: dir,
		}, mediaID > 0 && taskID > 0
	}
	if len(parts) != 3 || parts[0] != requestedSpace || isAudioHLSNamespace(parts[2]) {
		return RegisterInput{}, false
	}
	mediaID := parseInt64(parts[1])
	return RegisterInput{SpaceID: parts[0], MediaID: mediaID, Kind: CacheKindHLS, ProfileID: parts[2], Path: dir}, mediaID > 0
}

func (s *Service) inventoryTimelinePreviews(ctx context.Context, spaceID, root string) (int64, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}
	var count int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return walkErr
		}
		input, depth, err := timelineInventoryEntry(root, path, spaceID)
		if err != nil {
			return filepath.SkipDir
		}
		if depth != 5 {
			return ctx.Err()
		}
		if _, err := s.RegisterDirectory(ctx, input); err != nil {
			return err
		}
		count++
		return filepath.SkipDir
	})
	return count, err
}

func timelineInventoryEntry(root, path, spaceID string) (RegisterInput, int, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return RegisterInput{}, 0, err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if !validTimelinePrefix(parts, spaceID) {
		return RegisterInput{}, len(parts), ErrUnsafeCachePath
	}
	if len(parts) != 5 {
		return RegisterInput{}, len(parts), nil
	}
	input, err := parseTimelinePreviewPath(root, path, spaceID)
	return input, len(parts), err
}

func validTimelinePrefix(parts []string, requestedSpace string) bool {
	if len(parts) > 5 || len(parts) == 0 || !safeCacheToken(parts[0]) {
		return false
	}
	if requestedSpace != "" && parts[0] != requestedSpace {
		return false
	}
	if len(parts) > 1 && parsePositiveInt64(parts[1]) == 0 {
		return false
	}
	for index := 2; index < len(parts); index++ {
		if !safeCacheToken(parts[index]) {
			return false
		}
	}
	return true
}

func parseTimelinePreviewPath(root, path, requestedSpace string) (RegisterInput, error) {
	relative, err := filepath.Rel(cleanAbs(root), cleanAbs(path))
	if err != nil || filepath.IsAbs(relative) {
		return RegisterInput{}, ErrUnsafeCachePath
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 5 || !validTimelinePrefix(parts, requestedSpace) {
		return RegisterInput{}, ErrUnsafeCachePath
	}
	variant := parts[3] + ":" + parts[4]
	cacheKey := strings.Join([]string{
		CacheKindTimelinePreview, parts[0], parts[1], parts[2], parts[3], parts[4],
	}, ":")
	return RegisterInput{
		SpaceID: parts[0], MediaID: parsePositiveInt64(parts[1]), Kind: CacheKindTimelinePreview,
		ProfileID: parts[2], Variant: variant, CacheKey: cacheKey, Path: cleanAbs(path),
	}, nil
}

func safeCacheToken(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func parsePositiveInt64(raw string) int64 {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 || strconv.FormatInt(value, 10) != raw {
		return 0
	}
	return value
}

func (s *Service) markMissing(ctx context.Context, spaceID string) (int64, error) {
	var assets []models.CacheAsset
	if err := s.db.WithContext(ctx).Where("space_id = ?", normalizeSpace(spaceID)).Find(&assets).Error; err != nil {
		return 0, err
	}
	now := s.now().UTC()
	var count int64
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		absPath, err := s.safeAbsFromRelative(asset.RelativePath, asset.Kind)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) && asset.MissingAt == nil {
			if err := s.db.WithContext(ctx).Model(&models.CacheAsset{}).Where("id = ?", asset.ID).Updates(map[string]any{"missing_at": now, "updated_at": now}).Error; err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

func (s *Service) applySummaryQuery(db *gorm.DB, query SummaryQuery) *gorm.DB {
	db = db.Where("space_id = ?", normalizeSpace(query.SpaceID))
	if query.LibraryID > 0 {
		db = db.Where("library_id = ?", query.LibraryID)
	}
	if query.MediaID > 0 {
		db = db.Where("media_id = ?", query.MediaID)
	}
	if query.Kind != "" {
		db = db.Where("kind = ?", strings.TrimSpace(query.Kind))
	}
	return db
}

func (s *Service) libraryRoots(ctx context.Context) ([]string, error) {
	var paths []models.LibraryPath
	if err := s.db.WithContext(ctx).Select("path", "type").Find(&paths).Error; err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.Contains(path.Path, "://") {
			continue
		}
		roots = append(roots, cleanAbs(path.Path))
	}
	return roots, nil
}

func (s *Service) validateDeleteTarget(asset models.CacheAsset, libraryRoots []string) (string, error) {
	if !isKnownKind(asset.Kind) {
		return "", ErrInvalidKind
	}
	if asset.AssetLevel != CacheAssetLevelFile && asset.AssetLevel != CacheAssetLevelDirectory {
		return "", ErrUnsafeCachePath
	}
	absPath, err := s.safeAbsFromRelative(asset.RelativePath, asset.Kind)
	if err != nil {
		return "", err
	}
	if err := s.validateHLSInput(RegisterInput{
		SpaceID: asset.SpaceID, MediaID: asset.MediaID, Kind: asset.Kind,
		ProfileID: asset.ProfileID, Variant: asset.Variant, Path: absPath,
	}, asset.Kind, asset.AssetLevel, absPath); err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		if _, _, err := s.safePath(resolved, asset.Kind); err != nil {
			return "", err
		}
	}
	for _, root := range libraryRoots {
		if pathWithin(root, absPath) || asset.AssetLevel == CacheAssetLevelDirectory && pathWithin(absPath, root) {
			return "", ErrUnsafeCachePath
		}
	}
	return absPath, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(cleanAbs(root), cleanAbs(path))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Service) safePath(path, kind string) (string, string, error) {
	rootName, ok := kindDirs()[kind]
	if !ok {
		return "", "", ErrInvalidKind
	}
	absPath := cleanAbs(path)
	rel, err := filepath.Rel(s.data, absPath)
	if err != nil || !safeRelativePath(rel) {
		return "", "", ErrUnsafeCachePath
	}
	rel = filepath.Clean(rel)
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] != rootName || hasProtectedPath(rel) {
		return "", "", ErrUnsafeCachePath
	}
	if kind == CacheKindTimelinePreview {
		if _, err := parseTimelinePreviewPath(filepath.Join(s.data, rootName), absPath, ""); err != nil {
			return "", "", err
		}
		if err := rejectTimelineSymlinks(absPath); err != nil && !os.IsNotExist(err) {
			return "", "", err
		}
		if err := s.rejectSymlinkEscape(absPath, filepath.Join(s.data, rootName)); err != nil {
			return "", "", err
		}
	}
	return absPath, filepath.ToSlash(rel), nil
}

func safeRelativePath(relative string) bool {
	if filepath.IsAbs(relative) || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *Service) rejectSymlinkEscape(path, root string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = cleanAbs(root)
	}
	if !pathWithin(resolvedRoot, resolved) {
		return ErrUnsafeCachePath
	}
	return nil
}

func (s *Service) safeAbsFromRelative(relPath, kind string) (string, error) {
	returnValue, _, err := s.safePath(filepath.Join(s.data, filepath.FromSlash(relPath)), kind)
	return returnValue, err
}

func (s *Service) recordAudit(ctx context.Context, spaceID string, action string, metadata any) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      spaceID,
		ActorType:    audit.ActorSystem,
		Action:       action,
		ResourceType: "cache",
		ResourceID:   "storage-cache",
		Metadata:     metadata,
	})
}

func measurePath(path, level string) (int64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if level == CacheAssetLevelFile {
		if info.IsDir() {
			return 0, 0, fmt.Errorf("缓存文件登记不能指向目录")
		}
		return info.Size(), 1, nil
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("缓存目录登记不能指向文件")
	}
	return measureDirectory(path)
}

func measureDirectory(path string) (int64, int64, error) {
	var size, count int64
	err := filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		count++
		return nil
	})
	return size, count, err
}

func deleteAssetPath(path, level string) error {
	if level == CacheAssetLevelDirectory {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func normalizeKinds(kinds []string) ([]string, error) {
	if len(kinds) == 0 {
		return orderedKinds(), nil
	}
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		kind = strings.TrimSpace(kind)
		if !isKnownKind(kind) {
			return nil, ErrInvalidKind
		}
		if !slices.Contains(out, kind) {
			out = append(out, kind)
		}
	}
	return out, nil
}

func kindDirs() map[string]string {
	return map[string]string{
		CacheKindThumbnail:       "thumbnails",
		CacheKindHLS:             "hls",
		CacheKindImageProxy:      "image_cache",
		CacheKindCover:           "covers",
		CacheKindMetadataTemp:    "metadata_temp",
		CacheKindTimelinePreview: "timeline_previews",
	}
}

func orderedKinds() []string {
	return []string{
		CacheKindThumbnail, CacheKindHLS, CacheKindImageProxy,
		CacheKindCover, CacheKindMetadataTemp, CacheKindTimelinePreview,
	}
}

func isKnownKind(kind string) bool {
	_, ok := kindDirs()[kind]
	return ok
}

func hasProtectedPath(relPath string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(relPath), func(char rune) bool { return char == '/' }) {
		name := strings.ToLower(strings.TrimSpace(part))
		if isProtectedName(name) || name == "audit" || name == "audits" || name == "backup" || name == "backups" {
			return true
		}
	}
	return false
}

func isProtectedName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "jianvideo.db" || strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db-wal") || strings.HasSuffix(name, ".db-shm")
}

func normalizeSpace(spaceID string) string {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return models.DefaultSpaceID
	}
	return spaceID
}

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func cleanAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func parseInt64(raw string) int64 {
	var value int64
	_, _ = fmt.Sscanf(raw, "%d", &value)
	return value
}
