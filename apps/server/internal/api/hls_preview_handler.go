package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// HLSStatus GET /api/play/:id/hls-status，返回指定 profile 的可用性与最近任务。
func (h *Handler) HLSStatus(c *gin.Context) {
	profileID := strings.TrimSpace(c.Query("profile_id"))
	taskID, err := parseOptionalTaskID(c.Query("task_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_HLS_TASK_ID", "message": "无效的 HLS 任务 ID"})
		return
	}
	if taskID == 0 && transcoder.IsAudioReloadProfileNamespace(profileID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "HLS_TASK_ID_REQUIRED", "message": "音轨 HLS 状态必须提供 task_id"})
		return
	}
	if profileID == transcoder.ABRProfileID && h.hlsABR == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HLS_ABR_UNAVAILABLE", "message": "自适应版本生成服务未启用"})
		return
	}
	if profileID != transcoder.ABRProfileID && h.hlsPreview == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HLS_PREVIEW_UNAVAILABLE", "message": "HLS 预览服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	mediaID, ok := parseMediaID(c)
	if !ok {
		return
	}
	if _, err := h.loadMediaForViewer(c, spaceID, mediaID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	var status transcoder.HLSPreviewStatus
	switch {
	case profileID == transcoder.ABRProfileID:
		if taskID > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_HLS_TASK_ID", "message": "ABR 状态不支持 task_id"})
			return
		}
		status, err = h.hlsABR.Status(c.Request.Context(), spaceID, mediaID)
	case taskID > 0:
		status, err = h.hlsPreview.StatusTask(c.Request.Context(), spaceID, mediaID, profileID, taskID)
	default:
		status, err = h.hlsPreview.Status(c.Request.Context(), spaceID, mediaID, profileID)
	}
	if errors.Is(err, tasksvc.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "HLS_TASK_NOT_FOUND", "message": "HLS 任务不存在"})
		return
	}
	if err != nil {
		log.Printf("[ERROR] 查询 HLS 状态失败: media_id=%d profile_id=%q task_id=%d err=%v", mediaID, profileID, taskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "HLS_STATUS_FAILED", "message": "查询 HLS 状态失败"})
		return
	}
	var task any
	if status.Task != nil {
		task = toTaskResponse(status.Task)
	}
	response := gin.H{
		"available":  status.Available,
		"profile_id": status.ProfileID,
		"url":        status.URL,
		"task":       task,
	}
	if status.EffectiveTrackID != "" {
		response["effective_track_id"] = status.EffectiveTrackID
	}
	c.JSON(http.StatusOK, response)
}

func parseOptionalTaskID(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("任务 ID 无效")
	}
	return id, nil
}
