package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/library"
)

type watchStateUpdateRequest struct {
	PositionSeconds  *float64 `json:"position_seconds" binding:"required"`
	DurationSeconds  *float64 `json:"duration_seconds"`
	ExpectedRevision *int64   `json:"expected_revision" binding:"required"`
	SessionID        string   `json:"session_id" binding:"required"`
	EventSeq         *int64   `json:"event_seq" binding:"required"`
	EventType        string   `json:"event_type" binding:"required"`
	Reason           string   `json:"reason" binding:"required"`
}

// GetWatchState GET /api/play/:id/watch-state，返回当前 Space 的观看状态真源。
func (h *Handler) GetWatchState(c *gin.Context) {
	spaceID, mediaID, ok := h.watchStateTarget(c)
	if !ok {
		return
	}
	state, err := h.library.GetWatchStateInSpace(spaceID, mediaID)
	if err != nil {
		h.writeWatchStateError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

// UpdateWatchState PUT /api/play/:id/watch-state，应用带 revision 的观看事件。
func (h *Handler) UpdateWatchState(c *gin.Context) {
	spaceID, mediaID, ok := h.watchStateTarget(c)
	if !ok {
		return
	}
	var request watchStateUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_WATCH_STATE", "message": "观看状态请求体无效"})
		return
	}
	result, err := h.library.ApplyWatchEventInSpace(spaceID, mediaID, request.toInput())
	if err != nil {
		h.writeWatchStateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"applied": result.Applied, "current": result.State})
}

// WatchHistory GET /api/library/watch-history，返回当前 Space 的稳定游标观看历史。
func (h *Handler) WatchHistory(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	page, err := h.library.ListWatchHistoryInSpace(spaceID, c.Query("cursor"), parseWatchLimit(c, 20))
	if err != nil {
		var cursorError *library.WatchCursorError
		if errors.As(err, &cursorError) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_CURSOR", "message": cursorError.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询观看历史失败"})
		return
	}
	// 家长控制（FR2-051）：观看历史按访客 max 过滤。
	page.Items = filterWatchMediaByMaxRating(page.Items, h.viewerMaxContentRating(c, spaceID))
	c.JSON(http.StatusOK, page)
}

func (request watchStateUpdateRequest) toInput() library.WatchEventInput {
	return library.WatchEventInput{
		PositionSeconds: *request.PositionSeconds, DurationSeconds: request.DurationSeconds,
		ExpectedRevision: *request.ExpectedRevision, SessionID: request.SessionID,
		EventSeq: *request.EventSeq, EventType: request.EventType, Reason: request.Reason,
	}
}

func (h *Handler) watchStateTarget(c *gin.Context) (string, int64, bool) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return "", 0, false
	}
	mediaID, ok := parseMediaID(c)
	return spaceID, mediaID, ok
}

func (h *Handler) writeWatchStateError(c *gin.Context, err error) {
	var conflict *library.WatchStateConflictError
	var validation *library.WatchEventValidationError
	switch {
	case errors.As(err, &conflict):
		c.JSON(http.StatusConflict, gin.H{
			"code": "WATCH_STATE_CONFLICT", "message": "观看状态已被其他会话更新",
			"applied": false, "current": conflict.Current,
		})
	case errors.As(err, &validation):
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_WATCH_STATE", "message": validation.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新观看状态失败"})
	}
}

func parseWatchLimit(c *gin.Context, fallback int) int {
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit < 1 {
		return fallback
	}
	return limit
}

// UpdateWatchPosition PUT /api/play/:id/position
// 请求体：{"position": 12.5}，持久化媒体上次播放位置（秒），返回更新后的媒体对象。
// 注意：此为「用户观看位置」，区别于 playback 的转码/缓冲进度（/api/play/:id/progress）。
func (h *Handler) UpdateWatchPosition(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
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

	mf, err := h.library.UpdateWatchPositionInSpace(spaceID, id, req.Position)
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
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	mf, err := h.library.MarkWatchedInSpace(spaceID, id)
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
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	items, err := h.library.ListContinueWatchingStatesInSpace(spaceID, parseWatchLimit(c, 12))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	// 家长控制（FR2-051）：继续观看按访客 max 过滤。
	items = filterWatchMediaByMaxRating(items, h.viewerMaxContentRating(c, spaceID))
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// OnThisDay GET /api/library/on-this-day?limit=N
// 返回「往年同一天」拍摄的媒体列表（FR-72「那年今日」，按 media_time 倒序），供首页回忆区块展示。
func (h *Handler) OnThisDay(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	limit := 12
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	items, err := h.library.ListOnThisDayInSpace(spaceID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	// 家长控制（FR2-051）：那年今日按访客 max 过滤。
	items = filterMediaByMaxRating(items, h.viewerMaxContentRating(c, spaceID))
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// MarkMediaViewed PUT /api/library/media/:id/viewed
// 记录媒体被打开（详情面板/播放页）的时刻（FR-120）：把 last_viewed_at 置为当前时间。
// 成功 200 {"ok":true}；非法 id → 400，媒体不存在 → 404。
func (h *Handler) MarkMediaViewed(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	if err := h.library.SetMediaViewedInSpace(spaceID, id); err != nil {
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
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	limit := 12
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	items, err := h.library.RecentlyViewedInSpace(spaceID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	// 家长控制（FR2-051）：最近查看按访客 max 过滤。
	items = filterMediaByMaxRating(items, h.viewerMaxContentRating(c, spaceID))
	c.JSON(http.StatusOK, gin.H{"items": items})
}
