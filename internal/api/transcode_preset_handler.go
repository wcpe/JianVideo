package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// presetRequest 预设创建/更新请求体（FR-77）。
type presetRequest struct {
	Name   string `json:"name"`
	Codec  string `json:"codec"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// presetErrorStatus 把预设校验错误映射为 HTTP 状态码与错误码。
// 不存在 → 404；校验类（空名/编码/分辨率）→ 400；其余 → 500。
func presetErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, transcoder.ErrPresetNotFound):
		return http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, transcoder.ErrPresetNameEmpty),
		errors.Is(err, transcoder.ErrPresetCodecInvalid),
		errors.Is(err, transcoder.ErrPresetDimensionNegative):
		return http.StatusBadRequest, "INVALID_PRESET"
	default:
		return http.StatusInternalServerError, "INTERNAL"
	}
}

// ListTranscodePresets GET /api/transcode/presets
func (h *Handler) ListTranscodePresets(c *gin.Context) {
	if h.presets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PRESETS_UNAVAILABLE", "message": "转码预设服务未启用"})
		return
	}
	items, err := h.presets.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询预设失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateTranscodePreset POST /api/transcode/presets
func (h *Handler) CreateTranscodePreset(c *gin.Context) {
	if h.presets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PRESETS_UNAVAILABLE", "message": "转码预设服务未启用"})
		return
	}
	var req presetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	preset, err := h.presets.Create(req.Name, req.Codec, req.Width, req.Height)
	if err != nil {
		status, code := presetErrorStatus(err)
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, preset)
}

// UpdateTranscodePreset PUT /api/transcode/presets/:id
func (h *Handler) UpdateTranscodePreset(c *gin.Context) {
	if h.presets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PRESETS_UNAVAILABLE", "message": "转码预设服务未启用"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	var req presetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	preset, err := h.presets.Update(id, req.Name, req.Codec, req.Width, req.Height)
	if err != nil {
		status, code := presetErrorStatus(err)
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preset)
}

// DeleteTranscodePreset DELETE /api/transcode/presets/:id
func (h *Handler) DeleteTranscodePreset(c *gin.Context) {
	if h.presets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PRESETS_UNAVAILABLE", "message": "转码预设服务未启用"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}
	if err := h.presets.Delete(id); err != nil {
		status, code := presetErrorStatus(err)
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

type transcodeTaskRequest struct {
	MediaID      int64  `json:"media_id"`
	PresetID     int64  `json:"preset_id"`
	ProfileID    string `json:"profile_id"`
	Priority     int    `json:"priority"`
	ForceRebuild bool   `json:"force_rebuild"`
}

type legacyTranscodeTaskResponse struct {
	ID          int64      `json:"id"`
	SpaceID     string     `json:"space_id"`
	MediaID     int64      `json:"media_id"`
	PresetID    int64      `json:"preset_id"`
	ProfileID   string     `json:"profile_id"`
	Codec       string     `json:"codec"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	Progress    float64    `json:"progress"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// CreateTranscodeTask POST /api/transcode/tasks，将旧请求映射到统一 HLS preview 任务。
func (h *Handler) CreateTranscodeTask(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req transcodeTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	preset, ok := h.resolveTranscodeTaskPreset(c, spaceID, req)
	if !ok {
		return
	}
	if h.hlsPreview == nil {
		h.enqueueLegacyTranscodeTask(c, spaceID, req, preset)
		return
	}
	profileID := req.ProfileID
	if profileID == "" {
		profileID = preset.Codec
	}
	task, err := h.hlsPreview.Enqueue(c.Request.Context(), transcoder.HLSPreviewRequest{
		SpaceID: spaceID, MediaID: req.MediaID, PresetID: preset.ID, ProfileID: profileID,
		Codec: preset.Codec, Width: preset.Width, Height: preset.Height,
		Priority: req.Priority, ForceRebuild: req.ForceRebuild,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ENQUEUE_FAILED", "message": err.Error()})
		return
	}
	if h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	c.JSON(http.StatusOK, gin.H{"status": "queued", "task_id": task.ID})
}

func (h *Handler) resolveTranscodeTaskPreset(c *gin.Context, spaceID string, req transcodeTaskRequest) (*models.TranscodePreset, bool) {
	if h.presets == nil || req.MediaID <= 0 || req.PresetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "media_id 与 preset_id 必填且为正"})
		return nil, false
	}
	if _, err := h.library.GetMediaFileByIDInSpace(spaceID, req.MediaID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "MEDIA_NOT_FOUND", "message": "媒体文件不存在"})
		return nil, false
	}
	preset, err := h.presets.Get(req.PresetID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "PRESET_NOT_FOUND", "message": "预设不存在"})
		return nil, false
	}
	return preset, true
}

func (h *Handler) enqueueLegacyTranscodeTask(c *gin.Context, spaceID string, req transcodeTaskRequest, preset *models.TranscodePreset) {
	if h.pregenQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PREGEN_UNAVAILABLE", "message": "转码预生成服务未启用"})
		return
	}
	taskID, err := h.pregenQueue.EnqueueInSpace(spaceID, req.MediaID, preset.ID, preset.Codec, preset.Width, preset.Height)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ENQUEUE_FAILED", "message": "预生成入队失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "queued", "task_id": taskID})
}

// ListTranscodeTasks GET /api/transcode/tasks?status=，返回旧形状的统一 HLS preview 任务。
func (h *Handler) ListTranscodeTasks(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.hlsPreview == nil {
		h.listLegacyTranscodeTasks(c, spaceID)
		return
	}
	page, err := h.hlsPreview.List(c.Request.Context(), spaceID, c.Query("status"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TASK_QUERY", "message": err.Error()})
		return
	}
	items := make([]legacyTranscodeTaskResponse, 0, len(page.Items))
	for i := range page.Items {
		if item, ok := toLegacyTranscodeTask(page.Items[i]); ok {
			items = append(items, item)
		}
	}
	c.JSON(http.StatusOK, gin.H{"tasks": items})
}

func (h *Handler) listLegacyTranscodeTasks(c *gin.Context, spaceID string) {
	if h.pregenQueue == nil {
		c.JSON(http.StatusOK, gin.H{"tasks": []models.TranscodeTask{}})
		return
	}
	items, err := h.pregenQueue.ListTasksInSpace(spaceID, c.Query("status"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询任务失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": items})
}

func toLegacyTranscodeTask(task models.Task) (legacyTranscodeTaskResponse, bool) {
	var payload transcoder.HLSPreviewPayload
	if json.Unmarshal([]byte(task.PayloadJSON), &payload) != nil {
		return legacyTranscodeTaskResponse{}, false
	}
	status := task.Status
	if status == models.TaskStatusSucceeded {
		status = models.TranscodeTaskStatusCompleted
	} else if status == models.TaskStatusFailed {
		status = models.TranscodeTaskStatusError
	}
	spaceID := models.DefaultSpaceID
	if task.SpaceID != nil {
		spaceID = *task.SpaceID
	}
	return legacyTranscodeTaskResponse{
		ID: task.ID, SpaceID: spaceID, MediaID: payload.MediaID, PresetID: payload.PresetID,
		ProfileID: payload.ProfileID, Codec: payload.Codec, Width: payload.Width, Height: payload.Height,
		Status: status, Priority: task.Priority, Progress: float64(task.Progress) / 100,
		Error: task.Error, CreatedAt: task.CreatedAt, StartedAt: task.StartedAt, CompletedAt: task.FinishedAt,
	}, true
}
