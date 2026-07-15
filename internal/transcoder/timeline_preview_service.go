package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

const (
	// TaskTypeTimelinePreviewGenerate 是时间轴预览生成任务类型。
	TaskTypeTimelinePreviewGenerate = "preview.timeline.generate"
	// TimelinePreviewAvailable 表示当前 generation 可读取。
	TimelinePreviewAvailable = "available"
	// TimelinePreviewPending 表示预览缺失或正在生成。
	TimelinePreviewPending = "pending"
	timelineWriteAttempts  = 5
)

var (
	// ErrTimelinePreviewInvalid 表示时间轴预览身份或资源名无效。
	ErrTimelinePreviewInvalid = errors.New("时间轴预览身份无效")
	// ErrTimelinePreviewNotFound 表示完整身份未命中当前已发布资源。
	ErrTimelinePreviewNotFound   = errors.New("时间轴预览资源不存在")
	errTimelinePreviewSuperseded = errors.New("时间轴预览任务已被取代")
)

// TimelinePreviewIdentity 标识 Space 内媒体及其预览 profile。
type TimelinePreviewIdentity struct {
	MediaID   int64
	ProfileID string
	SpaceID   string
}

// TimelinePreviewResourceIdentity 标识一个已发布 generation 内的资源。
type TimelinePreviewResourceIdentity struct {
	TimelinePreviewIdentity
	GenerationID      string
	ResourceName      string
	SourceFingerprint string
}

// TimelinePreviewStatus 描述当前预览或未完成任务。
type TimelinePreviewStatus struct {
	GenerationID      string   `json:"generation_id,omitempty"`
	ProfileID         string   `json:"profile_id"`
	SourceFingerprint string   `json:"source_fingerprint,omitempty"`
	State             string   `json:"status"`
	Duration          float64  `json:"duration"`
	Version           int      `json:"version"`
	SpriteNames       []string `json:"-"`
	TaskID            int64    `json:"task_id,omitempty"`
}

// TimelinePreviewResource 是受控读取的 VTT 或 sprite。
type TimelinePreviewResource struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// TimelinePreviewService 协调任务、生成器、缓存登记和当前指针。
type TimelinePreviewService struct {
	db                *gorm.DB
	tasks             *tasksvc.Service
	workers           *tasksvc.WorkerRegistry
	cache             *storage.Service
	dataDir           string
	generator         TimelinePreviewGenerator
	cleanCompensation func(context.Context, TimelinePreviewPayload, string) error
}

// NewTimelinePreviewService 创建时间轴预览服务。
func NewTimelinePreviewService(db *gorm.DB, tasks *tasksvc.Service, workers *tasksvc.WorkerRegistry, cache *storage.Service, dataDir string, generator TimelinePreviewGenerator) *TimelinePreviewService {
	service := &TimelinePreviewService{
		db: db, tasks: tasks, workers: workers, cache: cache,
		dataDir: filepath.Clean(dataDir), generator: generator,
	}
	service.cleanCompensation = service.cleanTimelineCompensation
	return service
}

// RegisterWorker 注册单并发时间轴预览 worker。
func (s *TimelinePreviewService) RegisterWorker() error {
	if s.db == nil || s.tasks == nil || s.workers == nil || s.cache == nil || s.generator == nil {
		return errors.New("时间轴预览服务未完整配置")
	}
	return s.workers.Register(TaskTypeTimelinePreviewGenerate, 1, s.handleTask)
}

// Status 返回当前可用 generation，缺失时返回未完成任务。
func (s *TimelinePreviewService) Status(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	identity, media, fingerprint, err := s.resolveIdentity(ctx, identity)
	if err != nil {
		return TimelinePreviewStatus{}, err
	}
	if status, ok := s.currentStatus(ctx, identity, fingerprint); ok {
		return withTimelineMetadata(status, media), nil
	}
	pending, err := s.pendingStatus(ctx, identity, fingerprint)
	if err != nil {
		return TimelinePreviewStatus{}, err
	}
	return withTimelineMetadata(pending, media), nil
}

// Enqueue 幂等创建或复用普通 generation。
func (s *TimelinePreviewService) Enqueue(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	return s.enqueue(ctx, identity, false)
}

// Rebuild 每次创建新的强制重建 generation。
func (s *TimelinePreviewService) Rebuild(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewStatus, error) {
	return s.enqueue(ctx, identity, true)
}

func (s *TimelinePreviewService) enqueue(ctx context.Context, identity TimelinePreviewIdentity, force bool) (TimelinePreviewStatus, error) {
	identity, media, fingerprint, err := s.resolveIdentity(ctx, identity)
	if err != nil {
		return TimelinePreviewStatus{}, err
	}
	var status TimelinePreviewStatus
	err = retryTimelineWrite(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var enqueueErr error
			status, enqueueErr = s.enqueueTx(ctx, tx, identity, fingerprint, force)
			return enqueueErr
		})
	})
	return withTimelineMetadata(status, media), err
}

func (s *TimelinePreviewService) enqueueTx(ctx context.Context, tx *gorm.DB, identity TimelinePreviewIdentity, fingerprint string, force bool) (TimelinePreviewStatus, error) {
	pointer, err := lockTimelinePointer(ctx, tx, identity)
	if err != nil {
		return TimelinePreviewStatus{}, err
	}
	if !force {
		if status, ok, err := pendingPointerStatus(ctx, tx, pointer, fingerprint); err != nil || ok {
			return status, err
		}
	}
	payload, task, err := s.createGenerationTaskTx(ctx, tx, identity, fingerprint, force)
	if err != nil {
		return TimelinePreviewStatus{}, err
	}
	if err := setPendingPointer(ctx, tx, pointer.ID, payload, task.ID); err != nil {
		return TimelinePreviewStatus{}, err
	}
	return pendingTimelineStatus(identity.ProfileID, task, payload), nil
}

func (s *TimelinePreviewService) createGenerationTaskTx(ctx context.Context, tx *gorm.DB, identity TimelinePreviewIdentity, fingerprint string, force bool) (TimelinePreviewPayload, *models.Task, error) {
	generation, err := NewTimelinePreviewGenerationID()
	if err != nil {
		return TimelinePreviewPayload{}, nil, err
	}
	payload := TimelinePreviewPayload{SpaceID: identity.SpaceID, MediaID: identity.MediaID, ProfileID: identity.ProfileID,
		SourceFingerprint: fingerprint, GenerationID: generation, ForceRebuild: force}
	data, err := json.Marshal(payload)
	if err != nil {
		return payload, nil, fmt.Errorf("编码时间轴预览任务失败: %w", err)
	}
	task, err := s.tasks.EnqueueTx(ctx, tx, timelineTaskInput(identity.MediaID, payload, string(data)))
	return payload, task, err
}

func timelineTaskInput(mediaID int64, payload TimelinePreviewPayload, data string) tasksvc.EnqueueInput {
	return tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: payload.SpaceID, Type: TaskTypeTimelinePreviewGenerate,
		MaxAttempts: 3, IdempotencyKey: TimelinePreviewTaskKey(payload), PayloadJSON: data,
		ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10),
	}
}

func lockTimelinePointer(ctx context.Context, tx *gorm.DB, identity TimelinePreviewIdentity) (models.MediaTimelinePreview, error) {
	now := time.Now().UTC()
	pointer := models.MediaTimelinePreview{
		SpaceID: identity.SpaceID, MediaID: identity.MediaID, ProfileID: identity.ProfileID, UpdatedAt: now,
	}
	err := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "space_id"}, {Name: "media_id"}, {Name: "profile_id"}},
		DoUpdates: clause.Assignments(map[string]any{"updated_at": gorm.Expr("media_timeline_previews.updated_at")}),
	}).Create(&pointer).Error
	if err != nil {
		return pointer, err
	}
	err = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("space_id = ? AND media_id = ? AND profile_id = ?", identity.SpaceID, identity.MediaID, identity.ProfileID).
		First(&pointer).Error
	return pointer, err
}

func pendingPointerStatus(ctx context.Context, tx *gorm.DB, pointer models.MediaTimelinePreview, fingerprint string) (TimelinePreviewStatus, bool, error) {
	if pointer.PendingSourceFingerprint != fingerprint || pointer.PendingGenerationID == "" || pointer.PendingTaskID <= 0 {
		return TimelinePreviewStatus{}, false, nil
	}
	var task models.Task
	err := tx.WithContext(ctx).Where("id = ? AND status IN ?", pointer.PendingTaskID,
		[]string{models.TaskStatusPending, models.TaskStatusRunning}).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TimelinePreviewStatus{}, false, nil
	}
	if err != nil {
		return TimelinePreviewStatus{}, false, err
	}
	payload, err := DecodeTimelinePreviewPayload(task.PayloadJSON)
	if err != nil || payload.GenerationID != pointer.PendingGenerationID {
		return TimelinePreviewStatus{}, false, err
	}
	return pendingTimelineStatus(pointer.ProfileID, &task, payload), true, nil
}

func setPendingPointer(ctx context.Context, tx *gorm.DB, pointerID int64, payload TimelinePreviewPayload, taskID int64) error {
	return tx.WithContext(ctx).Model(&models.MediaTimelinePreview{}).Where("id = ?", pointerID).Updates(map[string]any{
		"pending_source_fingerprint": payload.SourceFingerprint,
		"pending_generation_id":      payload.GenerationID,
		"pending_task_id":            taskID,
		"updated_at":                 time.Now().UTC(),
	}).Error
}

func retryTimelineWrite(ctx context.Context, operation func() error) error {
	for attempt := 0; attempt < timelineWriteAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation()
		if err == nil || !isSQLiteBusy(err) || attempt == timelineWriteAttempts-1 {
			return err
		}
		if err := waitTimelineRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return nil
}

func waitTimelineRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isSQLiteBusy(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

func pendingTimelineStatus(profileID string, task *models.Task, payload TimelinePreviewPayload) TimelinePreviewStatus {
	status := TimelinePreviewStatus{ProfileID: profileID, State: TimelinePreviewPending}
	if task != nil {
		status.TaskID = task.ID
		status.GenerationID = payload.GenerationID
		status.SourceFingerprint = payload.SourceFingerprint
	}
	return status
}

func withTimelineMetadata(status TimelinePreviewStatus, media models.MediaFile) TimelinePreviewStatus {
	status.Duration = media.Duration
	status.Version = DefaultTimelinePreviewProfile().Version
	return status
}

func (s *TimelinePreviewService) resolveIdentity(ctx context.Context, identity TimelinePreviewIdentity) (TimelinePreviewIdentity, models.MediaFile, string, error) {
	identity.SpaceID = normalizeTimelineSpace(identity.SpaceID)
	if identity.ProfileID == "" {
		identity.ProfileID = DefaultTimelinePreviewProfile().ID
	}
	if identity.MediaID <= 0 || identity.ProfileID != DefaultTimelinePreviewProfile().ID || !validTimelinePreviewToken(identity.SpaceID) {
		return identity, models.MediaFile{}, "", ErrTimelinePreviewInvalid
	}
	media, err := s.loadMedia(ctx, identity.SpaceID, identity.MediaID)
	if err != nil {
		return identity, media, "", err
	}
	fingerprint, err := fingerprintMedia(media)
	return identity, media, fingerprint, err
}

func (s *TimelinePreviewService) loadMedia(ctx context.Context, spaceID string, mediaID int64) (models.MediaFile, error) {
	var media models.MediaFile
	err := s.db.WithContext(ctx).Where("space_id = ? AND id = ? AND deleted_at IS NULL", spaceID, mediaID).First(&media).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return media, ErrTimelinePreviewNotFound
	}
	if err != nil {
		return media, err
	}
	if media.FileState == models.MediaFileStateMissing || media.FilePath == "" {
		return media, ErrTimelinePreviewNotFound
	}
	return media, nil
}

func fingerprintMedia(media models.MediaFile) (string, error) {
	return TimelineSourceFingerprint(media.FilePath, media.ContentHash, media.ContentHashAlgo, media.ContentHashStale)
}

func (s *TimelinePreviewService) currentStatus(ctx context.Context, identity TimelinePreviewIdentity, fingerprint string) (TimelinePreviewStatus, bool) {
	var pointer models.MediaTimelinePreview
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND profile_id = ? AND source_fingerprint = ? AND asset_id > 0",
		identity.SpaceID, identity.MediaID, identity.ProfileID, fingerprint).First(&pointer).Error
	if err != nil {
		return TimelinePreviewStatus{}, false
	}
	sprites, ok := s.availableSpriteNames(ctx, pointer)
	if !ok {
		return TimelinePreviewStatus{}, false
	}
	return TimelinePreviewStatus{
		GenerationID: pointer.GenerationID, ProfileID: pointer.ProfileID,
		SourceFingerprint: pointer.SourceFingerprint, State: TimelinePreviewAvailable, SpriteNames: sprites,
	}, true
}

func (s *TimelinePreviewService) availableSpriteNames(ctx context.Context, pointer models.MediaTimelinePreview) ([]string, bool) {
	asset, ok := s.timelineAsset(ctx, pointer)
	if !ok {
		return nil, false
	}
	dir := TimelinePreviewGenerationPath(s.dataDir, pointerPayload(pointer))
	if filepath.Clean(filepath.Join(s.dataDir, filepath.FromSlash(asset.RelativePath))) != filepath.Clean(dir) {
		return nil, false
	}
	if _, err := os.Stat(filepath.Join(dir, "index.vtt")); err != nil {
		return nil, false
	}
	return controlledSpriteNames(dir)
}

func (s *TimelinePreviewService) timelineAsset(ctx context.Context, pointer models.MediaTimelinePreview) (models.CacheAsset, bool) {
	var asset models.CacheAsset
	err := s.db.WithContext(ctx).Where("id = ? AND space_id = ? AND media_id = ? AND kind = ? AND profile_id = ? AND missing_at IS NULL",
		pointer.AssetID, pointer.SpaceID, pointer.MediaID, storage.CacheKindTimelinePreview, pointer.ProfileID).First(&asset).Error
	return asset, err == nil && asset.CacheKey == TimelinePreviewCacheKey(pointerPayload(pointer))
}

func controlledSpriteNames(dir string) ([]string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && validTimelineResourceName(entry.Name()) && entry.Name() != "index.vtt" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, len(names) > 0
}

func pointerPayload(pointer models.MediaTimelinePreview) TimelinePreviewPayload {
	return TimelinePreviewPayload{
		SpaceID: pointer.SpaceID, MediaID: pointer.MediaID, ProfileID: pointer.ProfileID,
		SourceFingerprint: pointer.SourceFingerprint, GenerationID: pointer.GenerationID,
	}
}

func (s *TimelinePreviewService) pendingStatus(ctx context.Context, identity TimelinePreviewIdentity, fingerprint string) (TimelinePreviewStatus, error) {
	var pointer models.MediaTimelinePreview
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND profile_id = ?", identity.SpaceID, identity.MediaID, identity.ProfileID).First(&pointer).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TimelinePreviewStatus{ProfileID: identity.ProfileID, State: TimelinePreviewPending}, nil
	}
	if err != nil {
		return TimelinePreviewStatus{}, err
	}
	status, ok, err := pendingPointerStatus(ctx, s.db, pointer, fingerprint)
	if err != nil || ok {
		return status, err
	}
	return TimelinePreviewStatus{ProfileID: identity.ProfileID, State: TimelinePreviewPending}, nil
}

func (s *TimelinePreviewService) handleTask(ctx context.Context, task models.Task) error {
	payload, err := s.decodeTaskEnvelope(task)
	if err != nil {
		return err
	}
	media, err := s.verifyTaskSource(ctx, payload)
	if err != nil {
		return err
	}
	if err := s.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 5, Checkpoint: "已校验时间轴预览任务"}); err != nil {
		return err
	}
	request := TimelinePreviewGenerateRequest{
		SourcePath: media.FilePath, OutputDir: TimelinePreviewGenerationPath(s.dataDir, payload),
		Duration: durationFromMedia(media), Profile: DefaultTimelinePreviewProfile(),
	}
	if err := s.generator.Generate(ctx, request); err != nil {
		return err
	}
	return s.registerAndActivate(ctx, task.ID, media, payload, request.OutputDir)
}

func (s *TimelinePreviewService) decodeTaskEnvelope(task models.Task) (TimelinePreviewPayload, error) {
	payload, err := DecodeTimelinePreviewPayload(task.PayloadJSON)
	if err != nil {
		return payload, err
	}
	if task.Type != TaskTypeTimelinePreviewGenerate || task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return payload, errors.New("时间轴预览任务信封无效")
	}
	if payload.SpaceID != *task.SpaceID || task.ResourceType != "media" || task.ResourceID != strconv.FormatInt(payload.MediaID, 10) || task.IdempotencyKey != TimelinePreviewTaskKey(payload) {
		return payload, errors.New("时间轴预览任务 payload 与信封不匹配")
	}
	if payload.ProfileID != DefaultTimelinePreviewProfile().ID {
		return payload, errors.New("时间轴预览任务 profile 不受支持")
	}
	return payload, nil
}

func (s *TimelinePreviewService) verifyTaskSource(ctx context.Context, payload TimelinePreviewPayload) (models.MediaFile, error) {
	media, err := s.loadMedia(ctx, payload.SpaceID, payload.MediaID)
	if err != nil {
		return media, err
	}
	fingerprint, err := fingerprintMedia(media)
	if err != nil {
		return media, err
	}
	if fingerprint != payload.SourceFingerprint {
		return media, errors.New("媒体源指纹已变化")
	}
	return media, nil
}

func durationFromMedia(media models.MediaFile) time.Duration {
	return time.Duration(media.Duration * float64(time.Second))
}

func (s *TimelinePreviewService) registerAndActivate(ctx context.Context, taskID int64, media models.MediaFile, payload TimelinePreviewPayload, outputDir string) error {
	if err := s.ensureLatestPending(ctx, taskID, payload); err != nil {
		return s.handleRegistrationFailure(ctx, payload, outputDir, err)
	}
	prepared, err := s.cache.PrepareDirectoryAsset(timelineRegisterInput(media, payload, outputDir))
	if err != nil {
		return s.handleRegistrationFailure(ctx, payload, outputDir, err)
	}
	if err := s.tasks.UpdateProgress(ctx, taskID, tasksvc.ProgressInput{Progress: 90, Checkpoint: "已校验时间轴预览产物"}); err != nil {
		return s.handleRegistrationFailure(ctx, payload, outputDir, err)
	}
	if err := ctx.Err(); err != nil {
		return s.handleRegistrationFailure(ctx, payload, outputDir, err)
	}
	activated, err := s.commitTimelineAsset(ctx, taskID, payload, prepared)
	if err != nil || !activated {
		return s.handleRegistrationFailure(ctx, payload, outputDir, err)
	}
	return nil
}

func (s *TimelinePreviewService) handleRegistrationFailure(ctx context.Context, payload TimelinePreviewPayload, outputDir string, cause error) error {
	cleanupErr := s.removeTimelineGeneration(context.WithoutCancel(ctx), payload, outputDir)
	if cleanupErr != nil {
		return errors.Join(cause, cleanupErr)
	}
	if errors.Is(cause, errTimelinePreviewSuperseded) || cause == nil {
		return nil
	}
	return cause
}

func (s *TimelinePreviewService) ensureLatestPending(ctx context.Context, taskID int64, payload TimelinePreviewPayload) error {
	var pointer models.MediaTimelinePreview
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND profile_id = ?", payload.SpaceID, payload.MediaID, payload.ProfileID).First(&pointer).Error
	if err != nil {
		return err
	}
	if !timelinePendingMatches(pointer, taskID, payload) {
		return errTimelinePreviewSuperseded
	}
	return nil
}

func timelinePendingMatches(pointer models.MediaTimelinePreview, taskID int64, payload TimelinePreviewPayload) bool {
	return pointer.PendingGenerationID == payload.GenerationID && pointer.PendingTaskID == taskID &&
		pointer.PendingSourceFingerprint == payload.SourceFingerprint
}

func (s *TimelinePreviewService) removeTimelineGeneration(ctx context.Context, payload TimelinePreviewPayload, outputDir string) error {
	if err := validateTimelinePreviewPayload(payload); err != nil {
		return err
	}
	expected := TimelinePreviewGenerationPath(s.dataDir, payload)
	if filepath.Clean(outputDir) != filepath.Clean(expected) {
		return storage.ErrUnsafeCachePath
	}
	if s.cleanCompensation == nil {
		return errors.New("时间轴预览补偿清理未配置")
	}
	if err := s.cleanCompensation(ctx, payload, expected); err != nil {
		return fmt.Errorf("删除时间轴预览 generation 失败: %w", err)
	}
	return nil
}

func (s *TimelinePreviewService) cleanTimelineCompensation(ctx context.Context, payload TimelinePreviewPayload, path string) error {
	return s.cache.CleanUnregisteredTimelinePreviewGeneration(
		ctx, payload.SpaceID, payload.MediaID, payload.ProfileID,
		payload.SourceFingerprint, payload.GenerationID, path,
	)
}

func timelineRegisterInput(media models.MediaFile, payload TimelinePreviewPayload, outputDir string) storage.RegisterInput {
	return storage.RegisterInput{
		SpaceID: payload.SpaceID, LibraryID: media.LibraryID, MediaID: payload.MediaID,
		Kind: storage.CacheKindTimelinePreview, ProfileID: payload.ProfileID,
		Variant:  payload.SourceFingerprint + ":" + payload.GenerationID,
		CacheKey: TimelinePreviewCacheKey(payload), Path: outputDir,
	}
}

func (s *TimelinePreviewService) commitTimelineAsset(ctx context.Context, taskID int64, payload TimelinePreviewPayload, prepared storage.PreparedDirectoryAsset) (bool, error) {
	activated := false
	err := retryTimelineWrite(ctx, func() error {
		activated = false
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			activated, err = s.commitTimelineAssetTx(ctx, tx, taskID, payload, prepared)
			return err
		})
	})
	return activated, err
}

func (s *TimelinePreviewService) commitTimelineAssetTx(ctx context.Context, tx *gorm.DB, taskID int64, payload TimelinePreviewPayload, prepared storage.PreparedDirectoryAsset) (bool, error) {
	pointer, err := lockTimelinePointer(ctx, tx, TimelinePreviewIdentity{
		SpaceID: payload.SpaceID, MediaID: payload.MediaID, ProfileID: payload.ProfileID,
	})
	if err != nil {
		return false, err
	}
	if !timelinePendingMatches(pointer, taskID, payload) {
		return false, nil
	}
	if err := verifyTimelineSourceTx(ctx, tx, payload); err != nil {
		return false, err
	}
	asset, err := s.cache.RegisterDirectoryTx(ctx, tx, prepared)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	activated, err := activateLatestTimelineRequest(ctx, tx, pointer, taskID, payload, asset.ID)
	if err != nil {
		return false, err
	}
	if !activated {
		return false, errTimelinePreviewSuperseded
	}
	return true, nil
}

func verifyTimelineSourceTx(ctx context.Context, tx *gorm.DB, payload TimelinePreviewPayload) error {
	query := "space_id = ? AND id = ? AND deleted_at IS NULL"
	locked := tx.WithContext(ctx).Model(&models.MediaFile{}).Where(query, payload.SpaceID, payload.MediaID).
		UpdateColumn("file_size", gorm.Expr("file_size"))
	if locked.Error != nil || locked.RowsAffected != 1 {
		return firstTimelineDBError(locked.Error)
	}
	var media models.MediaFile
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(query, payload.SpaceID, payload.MediaID).First(&media).Error; err != nil {
		return err
	}
	fingerprint, err := fingerprintMedia(media)
	if err != nil || fingerprint != payload.SourceFingerprint {
		return errors.New("媒体源指纹已变化，拒绝切换当前预览")
	}
	return nil
}

func firstTimelineDBError(err error) error {
	if err != nil {
		return err
	}
	return gorm.ErrRecordNotFound
}

func activateLatestTimelineRequest(ctx context.Context, tx *gorm.DB, pointer models.MediaTimelinePreview, taskID int64, payload TimelinePreviewPayload, assetID int64) (bool, error) {
	if !timelinePendingMatches(pointer, taskID, payload) {
		return false, nil
	}
	result := tx.WithContext(ctx).Model(&models.MediaTimelinePreview{}).
		Where("id = ? AND pending_generation_id = ? AND pending_task_id = ?", pointer.ID, payload.GenerationID, taskID).
		Updates(map[string]any{
			"source_fingerprint": payload.SourceFingerprint, "generation_id": payload.GenerationID,
			"asset_id": assetID, "pending_source_fingerprint": "", "pending_generation_id": "",
			"pending_task_id": 0, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// OpenResource 只打开当前指针完整身份下的 VTT 或 JPEG sprite。
func (s *TimelinePreviewService) OpenResource(ctx context.Context, identity TimelinePreviewResourceIdentity) (TimelinePreviewResource, error) {
	base, _, _, err := s.resolveIdentity(ctx, identity.TimelinePreviewIdentity)
	if err != nil {
		return TimelinePreviewResource{}, err
	}
	if !validTimelineResourceName(identity.ResourceName) || !validTimelinePreviewToken(identity.GenerationID) || !validTimelinePreviewToken(identity.SourceFingerprint) {
		return TimelinePreviewResource{}, ErrTimelinePreviewInvalid
	}
	pointer, asset, err := s.resourcePointer(ctx, base, identity)
	if err != nil {
		return TimelinePreviewResource{}, err
	}
	path, err := s.safeResourcePath(asset, pointer, identity.ResourceName)
	if err != nil {
		return TimelinePreviewResource{}, err
	}
	return openTimelineResource(path, identity.ResourceName)
}

func (s *TimelinePreviewService) resourcePointer(ctx context.Context, base TimelinePreviewIdentity, identity TimelinePreviewResourceIdentity) (models.MediaTimelinePreview, models.CacheAsset, error) {
	var pointer models.MediaTimelinePreview
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ? AND profile_id = ? AND source_fingerprint = ? AND generation_id = ? AND asset_id > 0",
		base.SpaceID, base.MediaID, base.ProfileID, identity.SourceFingerprint, identity.GenerationID).First(&pointer).Error
	if err != nil {
		return pointer, models.CacheAsset{}, ErrTimelinePreviewNotFound
	}
	var asset models.CacheAsset
	err = s.db.WithContext(ctx).Where("id = ? AND kind = ? AND missing_at IS NULL", pointer.AssetID, storage.CacheKindTimelinePreview).First(&asset).Error
	if err != nil || asset.CacheKey != TimelinePreviewCacheKey(pointerPayload(pointer)) {
		return pointer, asset, ErrTimelinePreviewNotFound
	}
	return pointer, asset, nil
}

func (s *TimelinePreviewService) safeResourcePath(asset models.CacheAsset, pointer models.MediaTimelinePreview, name string) (string, error) {
	expected := TimelinePreviewGenerationPath(s.dataDir, pointerPayload(pointer))
	actual := filepath.Join(s.dataDir, filepath.FromSlash(asset.RelativePath))
	if filepath.Clean(actual) != filepath.Clean(expected) {
		return "", ErrTimelinePreviewNotFound
	}
	path := filepath.Join(actual, name)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Dir(resolved) != filepath.Clean(actual) {
		return "", ErrTimelinePreviewNotFound
	}
	return resolved, nil
}

func validTimelineResourceName(name string) bool {
	if name == "index.vtt" {
		return true
	}
	if filepath.Base(name) != name || !strings.HasPrefix(name, "sprite-") || !strings.HasSuffix(name, ".jpg") {
		return false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(name, "sprite-"), ".jpg")
	_, err := strconv.ParseUint(digits, 10, 32)
	return err == nil && len(digits) == 3
}

func openTimelineResource(path, name string) (TimelinePreviewResource, error) {
	// #nosec G304 -- path 已由 safeResourcePath 完成受控目录与符号链接校验。
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return TimelinePreviewResource{}, ErrTimelinePreviewNotFound
	}
	if err != nil {
		return TimelinePreviewResource{}, err
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		_ = file.Close()
		return TimelinePreviewResource{}, ErrTimelinePreviewNotFound
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if name == "index.vtt" {
		contentType = "text/vtt; charset=utf-8"
	} else if contentType == "" {
		contentType = "image/jpeg"
	}
	return TimelinePreviewResource{Body: file, ContentType: contentType, Size: info.Size()}, nil
}

func normalizeTimelineSpace(spaceID string) string {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return models.DefaultSpaceID
	}
	return spaceID
}
