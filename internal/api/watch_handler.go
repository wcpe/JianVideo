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

// OnThisDay GET /api/library/on-this-day?limit=N
// 返回「往年同一天」拍摄的媒体列表（FR-72「那年今日」，按 media_time 倒序），供首页回忆区块展示。
func (h *Handler) OnThisDay(c *gin.Context) {
	limit := 12
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	items, err := h.library.ListOnThisDay(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// MarkMediaViewed PUT /api/library/media/:id/viewed
// 记录媒体被打开（详情面板/播放页）的时刻（FR-120）：把 last_viewed_at 置为当前时间。
// 成功 200 {"ok":true}；非法 id → 400，媒体不存在 → 404。
func (h *Handler) MarkMediaViewed(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	if err := h.library.SetMediaViewed(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "记录最近查看失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RecentlyViewed GET /api/library/recently-viewed?limit=N
// 返回最近打开过的媒体列表（FR-120，按 last_viewed_at 倒序、排除软删），供时间轴「最近查看」回忆区块展示。
func (h *Handler) RecentlyViewed(c *gin.Context) {
	limit := 12
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	items, err := h.library.RecentlyViewed(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
