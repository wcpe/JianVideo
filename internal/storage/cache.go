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
	CacheKindThumbnail    = "thumbnail"
	CacheKindHLS          = "hls"
	CacheKindImageProxy   = "image_proxy"
	CacheKindCover        = "cover"
	CacheKindMetadataTemp = "metadata_temp"

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
		return fmt.Errorf("Space 不存在: %s", spaceID)
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
	if kind == CacheKindHLS {
		return s.inventoryHLS(ctx, spaceID, root)
	}
	return s.inventoryFiles(ctx, spaceID, kind, root)
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
	if _, err := s.validateDeleteTarget(asset, libraryRoots); err != nil {
		return err
	}
	if err := deleteAssetPath(absPath, CacheAssetLevelDirectory); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND kind = ? AND profile_id = ? AND relative_path = ?",
		normalizeSpace(spaceID), mediaID, CacheKindHLS, strings.TrimSpace(profileID), relPath).Delete(&models.CacheAsset{}).Error
}

func (s *Service) deleteRegisteredAssets(ctx context.Context, assets []models.CacheAsset, libraryRoots []string, result *CleanResult) error {
	deletedIDs := make([]int64, 0, len(assets))
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return err
		}
		absPath, err := s.validateDeleteTarget(asset, libraryRoots)
		if err != nil {
			result.FailedCount++
			continue
		}
		if err := deleteAssetPath(absPath, asset.AssetLevel); err != nil && !os.IsNotExist(err) {
			result.FailedCount++
			continue
		}
		deletedIDs = append(deletedIDs, asset.ID)
		result.DeletedCount++
		result.DeletedSizeBytes += asset.SizeBytes
	}
	if len(deletedIDs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Where("id IN ?", deletedIDs).Delete(&models.CacheAsset{}).Error
}

func (s *Service) register(ctx context.Context, input RegisterInput, level string) (*models.CacheAsset, error) {
	kind := strings.TrimSpace(input.Kind)
	if !isKnownKind(kind) {
		return nil, ErrInvalidKind
	}
	absPath, relPath, err := s.safePath(input.Path, kind)
	if err != nil {
		return nil, err
	}
	size, count, err := measurePath(absPath, level)
	if err != nil {
		return nil, err
	}
	asset := s.buildAsset(input, kind, level, relPath, size, count)
	if err := s.upsertAsset(ctx, &asset); err != nil {
		return nil, err
	}
	return &asset, nil
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

func (s *Service) upsertAsset(ctx context.Context, asset *models.CacheAsset) error {
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "relative_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"space_id", "library_id", "media_id", "kind", "asset_level", "profile_id",
			"variant", "cache_key", "size_bytes", "file_count", "rebuildable", "missing_at", "updated_at",
		}),
	}).Create(asset).Error
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Where("relative_path = ?", asset.RelativePath).First(asset).Error
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
	if len(parts) != 3 || parts[0] != requestedSpace {
		return RegisterInput{}, false
	}
	mediaID := parseInt64(parts[1])
	return RegisterInput{SpaceID: parts[0], MediaID: mediaID, Kind: CacheKindHLS, ProfileID: parts[2], Path: dir}, mediaID > 0
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
	absPath := cleanAbs(path)
	rel, err := filepath.Rel(s.data, absPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", ErrUnsafeCachePath
	}
	rel = filepath.Clean(rel)
	first := rel
	if idx := strings.IndexAny(rel, `/\`); idx >= 0 {
		first = rel[:idx]
	}
	if first != kindDirs()[kind] {
		return "", "", ErrUnsafeCachePath
	}
	if hasProtectedPath(rel) {
		return "", "", ErrUnsafeCachePath
	}
	return absPath, filepath.ToSlash(rel), nil
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
		CacheKindThumbnail:    "thumbnails",
		CacheKindHLS:          "hls",
		CacheKindImageProxy:   "image_cache",
		CacheKindCover:        "covers",
		CacheKindMetadataTemp: "metadata_temp",
	}
}

func orderedKinds() []string {
	return []string{CacheKindThumbnail, CacheKindHLS, CacheKindImageProxy, CacheKindCover, CacheKindMetadataTemp}
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
