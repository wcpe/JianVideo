package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type thumbnailBackfillRequest struct {
	Sizes []int `json:"sizes"`
}

// BackfillThumbnails POST /api/library/thumbnails/backfill。
func (h *Handler) BackfillThumbnails(c *gin.Context) {
	if h.thumbnail == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "THUMBNAIL_UNAVAILABLE", "message": "缩略图任务服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var request thumbnailBackfillRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": "请求格式无效"})
			return
		}
	}
	result, err := h.thumbnail.Backfill(c.Request.Context(), spaceID, request.Sizes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "THUMBNAIL_BACKFILL_ERROR", "message": err.Error()})
		return
	}
	if h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "task_id": result.TaskID, "sizes": result.Sizes})
}
