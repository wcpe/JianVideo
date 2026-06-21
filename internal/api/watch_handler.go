package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UpdateWatchPosition PUT /api/play/:id/position
// 请求体：{"position": 12.5}，持久化媒体上次播放位置（秒），返回更新后的媒体对象。
// 注意：此为「用户观看位置」，区别于 playback 的转码/缓冲进度（/api/play/:id/progress）。
func (h *Handler) UpdateWatchPosition(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	var req struct {
		Position float64 `json:"position"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	mf, err := h.library.UpdateWatchPosition(id, req.Position)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新播放位置失败"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// MarkWatched PUT /api/play/:id/watched
// 标记媒体已看完（清零续播位置），返回更新后的媒体对象。
func (h *Handler) MarkWatched(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	mf, err := h.library.MarkWatched(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "MARK_FAILED", "message": "标记已看失败"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// ContinueWatching GET /api/library/continue-watching?limit=N
// 返回有进度且未看完的媒体列表（按最近观看倒序），供首页「继续观看」区块展示。
func (h *Handler) ContinueWatching(c *gin.Context) {
	limit := 12
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	items, err := h.library.ListContinueWatching(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
