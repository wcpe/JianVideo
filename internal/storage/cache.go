// Package storage 提供缓存资产登记、盘点与清理能力。
package storage

import (
	"context"
	"errors"
	"fmt"
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
)

// ErrUnsafeCachePath 表示缓存路径不在允许管理的目录内。
var (
	ErrUnsafeCachePath = errors.New("缓存路径不在白名单内")
	ErrInvalidKind     = errors.New("缓存类型无效")
)

// Service 管理缓存资产元数据、盘点和清理任务。
type Service struct {
	db    *gorm.DB
	data  string
	audit audit.Recorder
	tasks *tasksvc.Service
	now   func() time.Time
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

// InventoryResult 表示缓存盘点结果。
type InventoryResult struct {
	Discovered int64 `json:"discovered"`
	Missing    int64 `json:"missing"`
	TaskID     int64 `json:"task_id,omitempty"`
}

// CleanInput 描述缓存清理输入。
type CleanInput struct {
	SpaceID string   `json:"space_id"`
	Kinds   []string `json:"kinds"`
	DryRun  bool     `json:"dry_run"`
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
	return &Service{db: db, data: cleanAbs(dataDir), now: time.Now}
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

// Inventory 扫描缓存目录并同步资产状态。
func (s *Service) Inventory(ctx context.Context, input InventoryInput) (InventoryResult, error) {
	taskID, err := s.createTask(ctx, TaskTypeCacheInventory, normalizeSpace(input.SpaceID), `{"operation":"inventory"}`)
	if err != nil {
		return InventoryResult{}, err
	}
	result := InventoryResult{TaskID: taskID}
	discovered, err := s.scanInventoryKinds(ctx, input.SpaceID)
	if err != nil {
		return result, s.finishTask(ctx, taskID, err)
	}
	result.Discovered = discovered
	missing, err := s.markMissing(ctx, input.SpaceID)
	if err != nil {
		return result, s.finishTask(ctx, taskID, err)
	}
	result.Missing = missing
	if err := s.recordAudit(ctx, normalizeSpace(input.SpaceID), "cache.inventory", result); err != nil {
		return result, s.finishTask(ctx, taskID, err)
	}
	return result, s.finishTask(ctx, taskID, nil)
}

func (s *Service) scanInventoryKinds(ctx context.Context, spaceID string) (int64, error) {
	var discovered int64
	for kind, dir := range kindDirs() {
		root := filepath.Join(s.data, dir)
		if kind == CacheKindHLS {
			n, err := s.inventoryHLS(ctx, spaceID, root)
			if err != nil {
				return discovered, err
			}
			discovered += n
			continue
		}
		n, err := s.inventoryFiles(ctx, spaceID, kind, root)
		if err != nil {
			return discovered, err
		}
		discovered += n
	}
	return discovered, nil
}

// Clean 清理或预览可重建缓存资产。
func (s *Service) Clean(ctx context.Context, input CleanInput) (CleanResult, error) {
	kinds, err := normalizeKinds(input.Kinds)
	if err != nil {
		return CleanResult{}, err
	}
	result := CleanResult{DryRun: input.DryRun}
	assets, err := s.cleanCandidates(ctx, input.SpaceID, kinds)
	if err != nil {
		return result, err
	}
	result.addCandidates(assets)
	if input.DryRun {
		return s.previewClean(ctx, input.SpaceID, result)
	}
	return s.executeClean(ctx, input.SpaceID, kinds, assets, result)
}

func (s *Service) cleanCandidates(ctx context.Context, spaceID string, kinds []string) ([]models.CacheAsset, error) {
	var assets []models.CacheAsset
	err := s.db.WithContext(ctx).
		Where("space_id = ? AND kind IN ? AND missing_at IS NULL", normalizeSpace(spaceID), kinds).
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

func (s *Service) executeClean(ctx context.Context, spaceID string, kinds []string, assets []models.CacheAsset, result CleanResult) (CleanResult, error) {
	taskID, err := s.createTask(ctx, TaskTypeCacheClean, normalizeSpace(spaceID), fmt.Sprintf(`{"kinds":%q}`, strings.Join(kinds, ",")))
	if err != nil {
		return result, err
	}
	result.TaskID = taskID
	for _, asset := range assets {
		size, deleted, failed := s.deleteRegisteredAsset(ctx, asset)
		if deleted {
			result.DeletedCount++
			result.DeletedSizeBytes += size
		}
		if failed {
			result.FailedCount++
		}
	}
	if result.FailedCount > 0 {
		result.Error = "部分缓存删除失败"
	}
	if err := s.recordAudit(ctx, normalizeSpace(spaceID), "cache.clean.executed", result); err != nil {
		return result, s.finishTask(ctx, taskID, err)
	}
	if result.FailedCount > 0 {
		return result, s.finishTask(ctx, taskID, errors.New(result.Error))
	}
	return result, s.finishTask(ctx, taskID, nil)
}

func (s *Service) deleteRegisteredAsset(ctx context.Context, asset models.CacheAsset) (int64, bool, bool) {
	absPath, err := s.safeAbsFromRelative(asset.RelativePath, asset.Kind)
	if err != nil {
		return 0, false, true
	}
	if err := deleteAssetPath(absPath, asset.AssetLevel); err != nil && !os.IsNotExist(err) {
		return 0, false, true
	}
	err = s.db.WithContext(ctx).Delete(&models.CacheAsset{}, asset.ID).Error
	return asset.SizeBytes, true, err != nil
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
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var count int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		mediaID := parseInt64(entry.Name())
		if _, err := s.RegisterDirectory(ctx, RegisterInput{SpaceID: spaceID, MediaID: mediaID, Kind: CacheKindHLS, Path: path}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Service) markMissing(ctx context.Context, spaceID string) (int64, error) {
	var assets []models.CacheAsset
	if err := s.db.WithContext(ctx).Where("space_id = ?", normalizeSpace(spaceID)).Find(&assets).Error; err != nil {
		return 0, err
	}
	now := s.now().UTC()
	var count int64
	for _, asset := range assets {
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
	if isProtectedName(filepath.Base(absPath)) {
		return "", "", ErrUnsafeCachePath
	}
	return absPath, filepath.ToSlash(rel), nil
}

func (s *Service) safeAbsFromRelative(relPath, kind string) (string, error) {
	returnValue, _, err := s.safePath(filepath.Join(s.data, filepath.FromSlash(relPath)), kind)
	return returnValue, err
}

func (s *Service) createTask(ctx context.Context, taskType, spaceID, payload string) (int64, error) {
	if s.tasks == nil {
		return 0, nil
	}
	now := s.now().UTC()
	task := models.Task{
		Scope:        models.TaskScopeSpace,
		SpaceID:      &spaceID,
		Type:         taskType,
		Status:       models.TaskStatusRunning,
		Priority:     0,
		Attempts:     1,
		MaxAttempts:  1,
		Progress:     0,
		PayloadJSON:  payload,
		ResourceType: "cache",
		ResourceID:   strings.TrimPrefix(taskType, "cache."),
		CreatedAt:    now,
		UpdatedAt:    now,
		StartedAt:    &now,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		return 0, err
	}
	return task.ID, nil
}

func (s *Service) finishTask(ctx context.Context, taskID int64, err error) error {
	if s.tasks == nil || taskID == 0 {
		return err
	}
	now := s.now().UTC()
	if err != nil {
		_ = s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":      models.TaskStatusFailed,
			"error":       err.Error(),
			"progress":    100,
			"updated_at":  now,
			"finished_at": now,
		}).Error
		return err
	}
	return s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ?", taskID).Updates(map[string]any{
		"status":      models.TaskStatusSucceeded,
		"progress":    100,
		"updated_at":  now,
		"finished_at": now,
	}).Error
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
