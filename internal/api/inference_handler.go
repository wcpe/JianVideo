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

const inferenceBackfillTaskType = "library.inference.backfill"

type inferenceBackfillPayload struct {
	SpaceID   string `json:"space_id"`
	LibraryID int64  `json:"library_id"`
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
	payload, err := json.Marshal(inferenceBackfillPayload{SpaceID: spaceID, LibraryID: libraryID})
	if err != nil {
		return nil, err
	}
	return h.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        spaceID,
		Type:           inferenceBackfillTaskType,
		Priority:       0,
		MaxAttempts:    1,
		IdempotencyKey: fmt.Sprintf("inference-backfill:%s:%d", spaceID, libraryID),
		PayloadJSON:    string(payload),
		ResourceType:   "library",
		ResourceID:     fmt.Sprintf("%d", libraryID),
	})
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
		_, err = lib.BackfillMediaInferencesWithProgressInSpace(ctx, payload.SpaceID, payload.LibraryID, progress)
		return err
	}
}

func parseInferenceBackfillTask(task models.Task) (inferenceBackfillPayload, error) {
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return inferenceBackfillPayload{}, errors.New("推断回填任务必须归属 Space")
	}
	var payload inferenceBackfillPayload
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
	return payload, nil
}
