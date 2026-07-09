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
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "不存在") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"code": "UPDATE_FAILED", "message": "保存推断失败"})
		return
	}
	c.JSON(http.StatusOK, inf)
}

// BackfillMediaInferences 处理媒体影视信息批量回填请求。
func (h *Handler) BackfillMediaInferences(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		LibraryID int64 `json:"library_id"`
	}
	_ = c.ShouldBindJSON(&req)
	if h.tasks == nil {
		updated, err := h.library.BackfillMediaInferencesInSpace(spaceID, req.LibraryID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "BACKFILL_FAILED", "message": "回填推断失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "succeeded", "updated": updated})
		return
	}
	h.enqueueAndRunInferenceBackfill(c, spaceID, req.LibraryID)
}

func (h *Handler) enqueueAndRunInferenceBackfill(c *gin.Context, spaceID string, libraryID int64) {
	task, err := h.enqueueInferenceBackfillTask(c.Request.Context(), spaceID, libraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ENQUEUE_FAILED", "message": "回填任务入队失败"})
		return
	}
	claimed, err := h.tasks.ClaimNext(c.Request.Context(), tasksvc.ClaimQuery{Type: "library.inference.backfill"})
	if err != nil || claimed.ID != task.ID {
		c.JSON(http.StatusOK, gin.H{"status": task.Status, "task_id": task.ID, "updated": 0})
		return
	}
	h.runClaimedInferenceBackfill(c, claimed.ID, spaceID, libraryID)
}

func (h *Handler) enqueueInferenceBackfillTask(ctx context.Context, spaceID string, libraryID int64) (*models.Task, error) {
	payload, _ := json.Marshal(map[string]any{"library_id": libraryID})
	return h.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        spaceID,
		Type:           "library.inference.backfill",
		Priority:       0,
		MaxAttempts:    1,
		IdempotencyKey: fmt.Sprintf("inference-backfill:%s:%d", spaceID, libraryID),
		PayloadJSON:    string(payload),
		ResourceType:   "library",
		ResourceID:     fmt.Sprintf("%d", libraryID),
	})
}

func (h *Handler) runClaimedInferenceBackfill(c *gin.Context, taskID int64, spaceID string, libraryID int64) {
	updated, runErr := h.library.BackfillMediaInferencesInSpace(spaceID, libraryID)
	if runErr != nil {
		_ = h.tasks.MarkFailed(context.Background(), taskID, runErr.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"code": "BACKFILL_FAILED", "message": "回填推断失败", "task_id": taskID})
		return
	}
	_ = h.tasks.MarkSucceeded(context.Background(), taskID)
	c.JSON(http.StatusOK, gin.H{"status": models.TaskStatusSucceeded, "task_id": taskID, "updated": updated})
}
