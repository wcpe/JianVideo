package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

// HLSStatus GET /api/play/:id/hls-status，返回指定 profile 的可用性与最近任务。
func (h *Handler) HLSStatus(c *gin.Context) {
	profileID := strings.TrimSpace(c.Query("profile_id"))
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
	if _, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	var status transcoder.HLSPreviewStatus
	var err error
	if profileID == transcoder.ABRProfileID {
		status, err = h.hlsABR.Status(c.Request.Context(), spaceID, mediaID)
	} else {
		status, err = h.hlsPreview.Status(c.Request.Context(), spaceID, mediaID, profileID)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_HLS_PROFILE", "message": err.Error()})
		return
	}
	var task any
	if status.Task != nil {
		task = toTaskResponse(status.Task)
	}
	c.JSON(http.StatusOK, gin.H{
		"available":  status.Available,
		"profile_id": status.ProfileID,
		"url":        status.URL,
		"task":       task,
	})
}
