package api

import (
	"errors"
	"net/http"
	"strconv"

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

// CreateTranscodeTask POST /api/transcode/tasks
// 请求体 {media_id, preset_id}：按预设快照编码/分辨率入预生成队列，返回 {"status":"queued","task_id":N}。
func (h *Handler) CreateTranscodeTask(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.presets == nil || h.pregenQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PREGEN_UNAVAILABLE", "message": "转码预生成服务未启用"})
		return
	}
	var req struct {
		MediaID  int64 `json:"media_id"`
		PresetID int64 `json:"preset_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	if req.MediaID <= 0 || req.PresetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "media_id 与 preset_id 必填且为正"})
		return
	}

	// 校验媒体存在
	if _, err := h.library.GetMediaFileByIDInSpace(spaceID, req.MediaID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "MEDIA_NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	// 取预设快照编码/分辨率
	preset, err := h.presets.Get(req.PresetID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "PRESET_NOT_FOUND", "message": "预设不存在"})
		return
	}

	taskID, err := h.pregenQueue.EnqueueInSpace(spaceID, req.MediaID, preset.ID, preset.Codec, preset.Width, preset.Height)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ENQUEUE_FAILED", "message": "预生成入队失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "queued", "task_id": taskID})
}

// ListTranscodeTasks GET /api/transcode/tasks?status=
// 返回预生成任务列表（按入队倒序），status 非空时仅返回该状态的任务。
func (h *Handler) ListTranscodeTasks(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.pregenQueue == nil {
		c.JSON(http.StatusOK, gin.H{"tasks": []models.TranscodeTask{}})
		return
	}
	tasks, err := h.pregenQueue.ListTasksInSpace(spaceID, c.Query("status"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询任务失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}
