// Package thumbnail 提供分档缩略图任务、路径隔离与缓存资产登记。
package thumbnail

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

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeGenerate 表示单媒体单尺寸或多尺寸缩略图生成任务。
	TaskTypeGenerate = "thumbnail.generate"
	// TaskTypeBackfill 表示当前 Space 的批量缩略图预生成任务。
	TaskTypeBackfill = "thumbnail.backfill"

	generatePriority = 100
	backfillPriority = 10
	maxAttempts      = 3
	backfillBatch    = 100
)

// Generator 执行单个媒体单尺寸的真实缩略图生成。
type Generator func(context.Context, models.MediaFile, int, string) error

// Service 管理缩略图按需与批量任务。
type Service struct {
	library   *library.Service
	tasks     *tasksvc.Service
	cache     *storage.Service
	dataDir   string
	generator Generator
}

// Result 表示缩略图就绪状态或异步任务信息。
type Result struct {
	Ready  bool   `json:"ready"`
	Path   string `json:"-"`
	TaskID int64  `json:"task_id,omitempty"`
	Sizes  []int  `json:"sizes"`
}

type generatePayload struct {
	SpaceID string `json:"space_id"`
	MediaID int64  `json:"media_id"`
	Sizes   []int  `json:"sizes"`
}

type backfillPayload struct {
	SpaceID string `json:"space_id"`
	Sizes   []int  `json:"sizes"`
}

// NewService 创建缩略图任务服务。
func NewService(lib *library.Service, tasks *tasksvc.Service, cache *storage.Service, dataDir string) *Service {
	service := &Service{library: lib, tasks: tasks, cache: cache, dataDir: filepath.Clean(dataDir)}
	service.generator = service.generateFile
	return service
}

// DataDir 返回缓存数据根目录，供测试和装配诊断使用。
func (s *Service) DataDir() string { return s.dataDir }

// Cache 返回缓存登记服务。
func (s *Service) Cache() *storage.Service { return s.cache }

// SetGeneratorForTest 替换真实生成器，仅供测试使用。
func (s *Service) SetGeneratorForTest(generator Generator) {
	if generator != nil {
		s.generator = generator
	}
}

// RegisterWorkers 注册缩略图按需与批量任务处理器。
func (s *Service) RegisterWorkers(registry *tasksvc.WorkerRegistry, concurrency int) error {
	if s.library == nil || s.tasks == nil || s.cache == nil {
		return errors.New("缩略图服务依赖未配置")
	}
	if registry == nil {
		return errors.New("缩略图 worker 注册表不能为空")
	}
	if concurrency <= 0 {
		concurrency = tasksvc.DefaultConcurrency(TaskTypeGenerate)
	}
	if err := registry.Register(TaskTypeGenerate, concurrency, s.handleGenerate); err != nil {
		return err
	}
	return registry.Register(TaskTypeBackfill, 1, s.handleBackfill)
}

// Ensure 确保指定媒体的尺寸缓存存在；缺失尺寸按幂等键入队。
func (s *Service) Ensure(ctx context.Context, spaceID string, mediaID int64, sizes []int) (Result, error) {
	media, err := s.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return Result{}, err
	}
	normalized, err := normalizeSizes(sizes, false)
	if err != nil {
		return Result{}, err
	}
	missing := make([]int, 0, len(normalized))
	for _, size := range normalized {
		path, pathErr := PathFor(s.dataDir, media.SpaceID, media.ID, size)
		if pathErr != nil {
			return Result{}, pathErr
		}
		if _, statErr := os.Stat(path); statErr == nil {
			if err := s.register(ctx, *media, size, path); err != nil {
				return Result{}, err
			}
			continue
		} else if !os.IsNotExist(statErr) {
			return Result{}, statErr
		}
		missing = append(missing, size)
	}
	firstPath, err := PathFor(s.dataDir, media.SpaceID, media.ID, normalized[0])
	if err != nil {
		return Result{}, err
	}
	if len(missing) == 0 {
		return Result{Ready: true, Path: firstPath, Sizes: normalized}, nil
	}
	task, err := s.enqueue(ctx, TaskTypeGenerate, media.SpaceID, generatePayload{
		SpaceID: media.SpaceID,
		MediaID: media.ID,
		Sizes:   missing,
	}, generatePriority, "media", strconv.FormatInt(media.ID, 10), generateKey(media.SpaceID, media.ID, missing))
	if err != nil {
		return Result{}, err
	}
	return Result{TaskID: task.ID, Sizes: missing}, nil
}

// Backfill 为当前 Space 批量预生成指定尺寸；空尺寸使用三档默认值。
func (s *Service) Backfill(ctx context.Context, spaceID string, sizes []int) (Result, error) {
	normalized, err := normalizeSizes(sizes, true)
	if err != nil {
		return Result{}, err
	}
	if exists, err := s.library.SpaceExists(spaceID); err != nil {
		return Result{}, err
	} else if !exists {
		return Result{}, fmt.Errorf("Space 不存在: %s", spaceID)
	}
	payload := backfillPayload{SpaceID: normalizeSpace(spaceID), Sizes: normalized}
	task, err := s.enqueue(ctx, TaskTypeBackfill, payload.SpaceID, payload, backfillPriority, "thumbnail", "backfill", backfillKey(payload.SpaceID, normalized))
	if err != nil {
		return Result{}, err
	}
	return Result{TaskID: task.ID, Sizes: normalized}, nil
}

// PathFor 按 Space/media/size 构造隔离路径。
func PathFor(dataDir, spaceID string, mediaID int64, size int) (string, error) {
	spaceID = strings.TrimSpace(spaceID)
	if !safeSegment(spaceID) {
		return "", errors.New("Space ID 不能用于缩略图路径")
	}
	if mediaID <= 0 {
		return "", errors.New("媒体 ID 必须大于 0")
	}
	if !slices.Contains(library.SupportedThumbnailSizes(), size) {
		return "", fmt.Errorf("缩略图尺寸无效: %d", size)
	}
	return filepath.Join(filepath.Clean(dataDir), "thumbnails", spaceID, strconv.FormatInt(mediaID, 10), strconv.Itoa(size)+".jpg"), nil
}

func safeSegment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return false
	}
	return filepath.Base(value) == value
}

func (s *Service) handleGenerate(ctx context.Context, task models.Task) error {
	var payload generatePayload
	if err := decodePayload(task.PayloadJSON, &payload); err != nil {
		return err
	}
	if err := validateTask(task, TaskTypeGenerate, payload.SpaceID, "media", strconv.FormatInt(payload.MediaID, 10)); err != nil {
		return err
	}
	sizes, err := validatePayloadSizes(payload.Sizes)
	if err != nil {
		return err
	}
	payload.Sizes = sizes
	media, err := s.library.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
	if err != nil {
		return err
	}
	for index, size := range payload.Sizes {
		if err := s.generateOne(ctx, *media, size); err != nil {
			return err
		}
		progress := 10 + (index+1)*80/len(payload.Sizes)
		if err := s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: progress, Checkpoint: strconv.Itoa(size)}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleBackfill(ctx context.Context, task models.Task) error {
	var payload backfillPayload
	if err := decodePayload(task.PayloadJSON, &payload); err != nil {
		return err
	}
	if err := validateTask(task, TaskTypeBackfill, payload.SpaceID, "thumbnail", "backfill"); err != nil {
		return err
	}
	sizes, err := validatePayloadSizes(payload.Sizes)
	if err != nil {
		return err
	}
	payload.Sizes = sizes
	afterID, err := parseCheckpoint(task.Checkpoint)
	if err != nil {
		return err
	}
	total, err := s.library.CountThumbnailCandidates(payload.SpaceID)
	if err != nil {
		return err
	}
	processed, lastID := int64(0), afterID
	for {
		items, listErr := s.library.ListThumbnailCandidates(payload.SpaceID, lastID, backfillBatch)
		if listErr != nil {
			return listErr
		}
		if len(items) == 0 {
			break
		}
		for _, media := range items {
			if s.supportsThumbnail(media) {
				for _, size := range payload.Sizes {
					if err := s.generateOne(ctx, media, size); err != nil {
						return err
					}
				}
			}
			processed++
			lastID = media.ID
			progress := backfillProgress(processed, total)
			if err := s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: progress, Checkpoint: strconv.FormatInt(lastID, 10)}); err != nil {
				return err
			}
		}
	}
	return s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 95, Checkpoint: strconv.FormatInt(lastID, 10)})
}

func (s *Service) supportsThumbnail(media models.MediaFile) bool {
	mediaType, ok := s.library.MediaTypeByPathInSpace(media.SpaceID, media.LibraryID, media.FilePath)
	return ok && (mediaType == library.MediaTypeImage || mediaType == library.MediaTypeVideo)
}

func (s *Service) generateOne(ctx context.Context, media models.MediaFile, size int) error {
	path, err := PathFor(s.dataDir, media.SpaceID, media.ID, size)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := s.generator(ctx, media, size, path); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return s.register(ctx, media, size, path)
}

func (s *Service) generateFile(ctx context.Context, media models.MediaFile, size int, outputPath string) error {
	mediaType, ok := s.library.MediaTypeByPathInSpace(media.SpaceID, media.LibraryID, media.FilePath)
	if !ok {
		return fmt.Errorf("媒体类型不支持缩略图: %s", media.FilePath)
	}
	return library.GenerateThumbnailFile(ctx, media.FilePath, mediaType, size, outputPath)
}

func (s *Service) register(ctx context.Context, media models.MediaFile, size int, path string) error {
	_, err := s.cache.RegisterFile(ctx, storage.RegisterInput{
		SpaceID:   media.SpaceID,
		LibraryID: media.LibraryID,
		MediaID:   media.ID,
		Kind:      storage.CacheKindThumbnail,
		Variant:   strconv.Itoa(size),
		CacheKey:  fmt.Sprintf("%s/%d/%d", media.SpaceID, media.ID, size),
		Path:      path,
	})
	return err
}

func (s *Service) enqueue(ctx context.Context, taskType, spaceID string, payload any, priority int, resourceType, resourceID, key string) (*models.Task, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码缩略图任务失败: %w", err)
	}
	return s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        normalizeSpace(spaceID),
		Type:           taskType,
		Priority:       priority,
		MaxAttempts:    maxAttempts,
		IdempotencyKey: key,
		PayloadJSON:    string(data),
		ResourceType:   resourceType,
		ResourceID:     resourceID,
	})
}

func validatePayloadSizes(sizes []int) ([]int, error) {
	if len(sizes) == 0 {
		return nil, errors.New("缩略图任务尺寸不能为空")
	}
	return normalizeSizes(sizes, false)
}

func normalizeSizes(sizes []int, allByDefault bool) ([]int, error) {
	if len(sizes) == 0 {
		if allByDefault {
			return library.SupportedThumbnailSizes(), nil
		}
		return []int{320}, nil
	}
	allowed := library.SupportedThumbnailSizes()
	result := make([]int, 0, len(sizes))
	for _, size := range sizes {
		if !slices.Contains(allowed, size) {
			return nil, fmt.Errorf("缩略图尺寸无效: %d", size)
		}
		if !slices.Contains(result, size) {
			result = append(result, size)
		}
	}
	slices.Sort(result)
	return result, nil
}

func generateKey(spaceID string, mediaID int64, sizes []int) string {
	return fmt.Sprintf("thumbnail:generate:%s:%d:%s", normalizeSpace(spaceID), mediaID, joinSizes(sizes))
}

func backfillKey(spaceID string, sizes []int) string {
	return "thumbnail:backfill:" + normalizeSpace(spaceID) + ":" + joinSizes(sizes)
}

func joinSizes(sizes []int) string {
	parts := make([]string, len(sizes))
	for index, size := range sizes {
		parts[index] = strconv.Itoa(size)
	}
	return strings.Join(parts, ",")
}

func normalizeSpace(spaceID string) string {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return models.DefaultSpaceID
	}
	return spaceID
}

func validateTask(task models.Task, taskType, spaceID, resourceType, resourceID string) error {
	if task.Type != taskType || task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return errors.New("缩略图任务信封无效")
	}
	if *task.SpaceID != normalizeSpace(spaceID) {
		return errors.New("缩略图任务 Space 不匹配")
	}
	if task.ResourceType != resourceType || task.ResourceID != resourceID {
		return errors.New("缩略图任务资源不匹配")
	}
	return nil
}

func decodePayload(raw string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("缩略图任务 payload 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("缩略图任务 payload 包含多余内容")
	}
	return nil
}

func parseCheckpoint(checkpoint string) (int64, error) {
	checkpoint = strings.TrimSpace(checkpoint)
	if checkpoint == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(checkpoint, 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("缩略图批量任务检查点无效")
	}
	return value, nil
}

func backfillProgress(processed, total int64) int {
	if total <= 0 {
		return 90
	}
	progress := 5 + int(processed*85/total)
	if progress > 90 {
		return 90
	}
	return progress
}
