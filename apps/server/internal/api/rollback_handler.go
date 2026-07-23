package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/rollback"
)

// ListRollbackEvents GET /api/rollback/events（FR2-041）
// 查询参数：days（默认 30）、limit、cursor、scope=system|space（默认 space，含合并 settings.updated）。
func (h *Handler) ListRollbackEvents(c *gin.Context) {
	if h.rollback == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "ROLLBACK_UNAVAILABLE", "message": "回滚服务未启用"})
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	systemOnly := c.Query("scope") == "system"
	spaceID := models.DefaultSpaceID
	if !systemOnly {
		sid, ok := h.resolveSpaceID(c)
		if !ok {
			return
		}
		spaceID = sid
	}
	result, err := h.rollback.ListRollbackEvents(c.Request.Context(), spaceID, systemOnly, days, limit, c.Query("cursor"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_QUERY", "message": err.Error()})
		return
	}
	// 序列化为精简结构
	items := make([]gin.H, 0, len(result.Items))
	for _, it := range result.Items {
		e := it.Event
		items = append(items, gin.H{
			"id":            e.ID,
			"scope":         e.Scope,
			"space_id":      e.SpaceID,
			"action":        e.Action,
			"resource_type": e.ResourceType,
			"resource_id":   e.ResourceID,
			"before_json":   auditJSONValue(e.BeforeJSON),
			"after_json":    auditJSONValue(e.AfterJSON),
			"created_at":    e.CreatedAt,
			"rollbackable":  it.Rollbackable,
			"reason_key":    it.ReasonKey,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"next_cursor": auditCursorValue(result.NextCursor),
	})
}

// ApplyRollback POST /api/rollback/apply（FR2-041）
// 请求体：{ "event_id": 1, "confirm": true }
func (h *Handler) ApplyRollback(c *gin.Context) {
	if h.rollback == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "ROLLBACK_UNAVAILABLE", "message": "回滚服务未启用"})
		return
	}
	var req struct {
		EventID int64 `json:"event_id"`
		Confirm bool  `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.EventID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	spaceID := models.DefaultSpaceID
	if sid, ok := h.resolveSpaceID(c); ok {
		spaceID = sid
	}
	actorID := actorIDFromContext(c)
	err := h.rollback.Apply(c.Request.Context(), rollback.ApplyInput{
		EventID: req.EventID,
		SpaceID: spaceID,
		Confirm: req.Confirm,
		ActorID: actorID,
	})
	if err != nil {
		writeRollbackErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func writeRollbackErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, rollback.ErrConfirmRequired):
		c.JSON(http.StatusBadRequest, gin.H{"code": "CONFIRM_REQUIRED", "message": "回滚需 confirm=true"})
	case errors.Is(err, rollback.ErrEventNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "审计事件不存在"})
	case errors.Is(err, rollback.ErrSpaceMismatch):
		c.JSON(http.StatusForbidden, gin.H{"code": "SPACE_FORBIDDEN", "message": "事件不属于当前 Space"})
	case errors.Is(err, rollback.ErrNotRollbackable):
		reason := strings.TrimSpace(err.Error())
		// 尝试提取 reason_key 后缀
		key := rollback.ReasonNotRegistered
		if i := strings.LastIndex(reason, ": "); i >= 0 {
			key = strings.TrimSpace(reason[i+2:])
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "NOT_ROLLBACKABLE", "message": "事件不可回滚", "reason_key": key})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "ROLLBACK_FAILED", "message": err.Error()})
	}
}
