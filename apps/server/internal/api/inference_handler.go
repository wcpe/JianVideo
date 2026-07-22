package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// GetMediaInference 处理媒体影视信息推断查询请求。
func (h *Handler) GetMediaInference(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	mf, err := h.library.GetMediaFileByIDInSpace(spaceID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	inf, err := h.library.GetMediaInferenceInSpace(spaceID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"inference": nil, "display_name": library.ResolveInferenceDisplayName(*mf, nil)})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询推断失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"inference": inf, "display_name": library.ResolveInferenceDisplayName(*mf, inf)})
}

// UpdateMediaInference 处理媒体影视信息人工纠正请求。
func (h *Handler) UpdateMediaInference(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	var req library.InferenceManualInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	inf, err := h.library.UpsertManualInferenceInSpace(spaceID, id, req)
	if err != nil {
		if errors.Is(err, library.ErrInvalidLibraryKind) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_KIND", "message": "推断 kind 无效"})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"code": "UPDATE_FAILED", "message": "保存推断失败"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "保存推断失败"})
		return
	}
	c.JSON(http.StatusOK, inf)
}

const (
	inferenceBackfillTaskType    = "library.inference.backfill"
	inferenceBackfillModeFull    = "full"
	inferenceBackfillModeMissing = "missing"
	inferenceBackfillModeMedia   = "media"
)

type inferenceBackfillPayload struct {
	SpaceID           string  `json:"space_id"`
	LibraryID         int64   `json:"library_id"`
	MediaID           int64   `json:"media_id,omitempty"`
	Mode              string  `json:"mode"`
	Generation        int64   `json:"generation,omitempty"`
	Enabled           bool    `json:"enabled,omitempty"`
	DisabledLibraries []int64 `json:"disabled_libraries,omitempty"`
}

// BackfillMediaInferences 处理媒体影视信息批量回填请求。
func (h *Handler) BackfillMediaInferences(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.tasks == nil || h.taskWorkers == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "通用任务服务未启用"})
		return
	}
	var req struct {
		LibraryID int64 `json:"library_id"`
	}
	_ = c.ShouldBindJSON(&req)
	task, err := h.enqueueInferenceBackfillTask(c.Request.Context(), spaceID, req.LibraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ENQUEUE_FAILED", "message": "回填任务入队失败"})
		return
	}
	h.taskWorkers.Wake()
	c.JSON(http.StatusAccepted, gin.H{"status": task.Status, "task_id": task.ID})
}

func (h *Handler) enqueueInferenceBackfillTask(ctx context.Context, spaceID string, libraryID int64) (*models.Task, error) {
	return h.enqueueInferenceBackfillTaskWithMode(ctx, spaceID, libraryID, inferenceBackfillModeFull)
}

func (h *Handler) enqueueInferenceBackfillTaskWithMode(ctx context.Context, spaceID string, libraryID int64, mode string) (*models.Task, error) {
	payload := inferenceBackfillPayload{SpaceID: spaceID, LibraryID: libraryID, Mode: mode}
	return h.enqueueInferenceTask(ctx, nil, payload)
}

func (h *Handler) enqueueInferenceTask(ctx context.Context, tx tasksvc.Tx, payload inferenceBackfillPayload) (*models.Task, error) {
	return enqueueInferenceTask(ctx, h.tasks, tx, payload)
}

// NewInferenceCompensationEnqueuer 创建媒体即时推断失败后的持久化补偿回调。
func NewInferenceCompensationEnqueuer(tasks *tasksvc.Service) library.InferenceCompensationEnqueuer {
	return func(ctx context.Context, spaceID string, libraryID, mediaID int64) error {
		payload := inferenceBackfillPayload{
			SpaceID: spaceID, LibraryID: libraryID, MediaID: mediaID, Mode: inferenceBackfillModeMedia,
		}
		_, err := enqueueInferenceTask(ctx, tasks, nil, payload)
		return err
	}
}

// enqueueInferenceTask 入队推断回填；tx 非 nil 时在调用方事务内创建（FR2-070：签名用 tasksvc.Tx，不暴露 *gorm.DB）。
func enqueueInferenceTask(ctx context.Context, tasks *tasksvc.Service, tx tasksvc.Tx, payload inferenceBackfillPayload) (*models.Task, error) {
	if tasks == nil {
		return nil, errors.New("推断回填任务服务未启用")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	input := inferenceTaskInput(payload, string(encoded))
	if tx != nil {
		return tasks.EnqueueTx(ctx, tx, input)
	}
	return tasks.Enqueue(ctx, input)
}

func inferenceTaskInput(payload inferenceBackfillPayload, encoded string) tasksvc.EnqueueInput {
	key := fmt.Sprintf("inference-backfill:%s:%d:%s", payload.SpaceID, payload.LibraryID, payload.Mode)
	resourceType, resourceID, maxAttempts := "library", fmt.Sprintf("%d", payload.LibraryID), 1
	switch {
	case payload.Mode == inferenceBackfillModeMedia:
		key = fmt.Sprintf("inference-media:%s:%d", payload.SpaceID, payload.MediaID)
		resourceType, resourceID, maxAttempts = "media", fmt.Sprintf("%d", payload.MediaID), 3
	case payload.Generation > 0:
		key = fmt.Sprintf("%s:%d", key, payload.Generation)
	}
	return tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: payload.SpaceID, Type: inferenceBackfillTaskType,
		Priority: 0, MaxAttempts: maxAttempts, IdempotencyKey: key, PayloadJSON: encoded,
		ResourceType: resourceType, ResourceID: resourceID,
	}
}

// RegisterInferenceBackfillWorker 注册离线推断回填处理器。
func RegisterInferenceBackfillWorker(registry *tasksvc.WorkerRegistry, lib *library.Service) error {
	if registry == nil || lib == nil {
		return errors.New("推断回填 worker 依赖不能为空")
	}
	handler := inferenceBackfillHandler(lib, registry)
	return registry.Register(inferenceBackfillTaskType, tasksvc.DefaultConcurrency(inferenceBackfillTaskType), handler)
}

func inferenceBackfillHandler(lib *library.Service, registry *tasksvc.WorkerRegistry) tasksvc.Handler {
	return func(ctx context.Context, task models.Task) error {
		payload, err := parseInferenceBackfillTask(task)
		if err != nil {
			return err
		}
		progress := func(completed, total int, mediaID int64) error {
			percent := 100
			if total > 0 {
				percent = completed * 100 / total
			}
			return registry.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{
				Progress:   percent,
				Checkpoint: fmt.Sprintf("media:%d", mediaID),
			})
		}
		switch payload.Mode {
		case inferenceBackfillModeMedia:
			_, err = lib.InferAndStoreMediaInSpace(payload.SpaceID, payload.MediaID)
		case inferenceBackfillModeMissing:
			cfg := library.InferenceConfig{
				Enabled: payload.Enabled, Generation: payload.Generation,
				DisabledLibraries: inferenceDisabledLibrarySet(payload.DisabledLibraries),
			}
			_, err = lib.BackfillMissingMediaInferencesWithConfigInSpace(ctx, payload.SpaceID, payload.LibraryID, cfg, progress)
		default:
			_, err = lib.BackfillMediaInferencesWithProgressInSpace(ctx, payload.SpaceID, payload.LibraryID, progress)
		}
		return err
	}
}

func inferenceDisabledLibrarySet(ids []int64) map[int64]bool {
	result := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id > 0 {
			result[id] = true
		}
	}
	return result
}

func parseInferenceBackfillTask(task models.Task) (inferenceBackfillPayload, error) {
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return inferenceBackfillPayload{}, errors.New("推断回填任务必须归属 Space")
	}
	payload := inferenceBackfillPayload{Enabled: true}
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return payload, fmt.Errorf("解析推断回填任务参数失败: %w", err)
	}
	taskSpaceID := strings.TrimSpace(*task.SpaceID)
	if strings.TrimSpace(payload.SpaceID) == "" {
		return payload, errors.New("推断回填任务参数缺少 Space")
	}
	if payload.SpaceID != taskSpaceID {
		return payload, errors.New("推断回填任务 Space 与参数不一致")
	}
	if payload.Mode == "" {
		payload.Mode = inferenceBackfillModeFull
	}
	if payload.Mode != inferenceBackfillModeFull && payload.Mode != inferenceBackfillModeMissing && payload.Mode != inferenceBackfillModeMedia {
		return payload, errors.New("推断回填任务模式无效")
	}
	if payload.Generation < 0 {
		payload.Generation = 0
	}
	if payload.Mode == inferenceBackfillModeMedia && payload.MediaID <= 0 {
		return payload, errors.New("推断补偿任务参数缺少媒体 ID")
	}
	return payload, nil
}
