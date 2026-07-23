package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/transcoder"
)

// CreateHLSABR POST /api/play/:id/hls-abr，显式创建多码率 HLS 任务。
func (h *Handler) CreateHLSABR(c *gin.Context) {
	if h.hlsABR == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HLS_ABR_UNAVAILABLE", "message": "自适应版本生成服务未启用"})
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
	media, err := h.loadMediaForViewer(c, spaceID, mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	var request struct {
		Priority     int  `json:"priority"`
		ForceRebuild bool `json:"force_rebuild"`
	}
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	ladder := transcoder.DefaultABRLadderNames()
	hwPreference := "auto"
	if h.settings != nil {
		ladder = h.settings.TranscodeABRLadder()
		hwPreference = h.settings.TranscodeHWAccelMode()
	}
	task, err := h.hlsABR.Enqueue(c.Request.Context(), transcoder.ABRRequest{
		SpaceID: spaceID, MediaID: mediaID, SourceWidth: media.Width, SourceHeight: media.Height,
		Ladder: ladder, Priority: request.Priority, ForceRebuild: request.ForceRebuild,
		HWAccelPreference: hwPreference,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "HLS_ABR_ENQUEUE_FAILED", "message": err.Error()})
		return
	}
	if h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	c.JSON(http.StatusAccepted, gin.H{
		"task_id": task.ID, "profile_id": transcoder.ABRProfileID,
		"url": transcoder.HLSProfileURL(mediaID, transcoder.ABRProfileID, "h264"),
	})
}
