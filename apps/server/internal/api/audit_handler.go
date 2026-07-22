package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/audit"
)

// ListAuditEvents 查询审计事件。
// 支持按 Space 或系统作用域过滤，并按动作、资源、时间范围与游标分页。
func (h *Handler) ListAuditEvents(c *gin.Context) {
	if h.audit == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUDIT_UNAVAILABLE", "message": "审计服务未启用"})
		return
	}

	systemScope := c.Query("scope") == audit.ScopeSystem
	query := audit.Query{
		System:       systemScope,
		Action:       c.Query("action"),
		ResourceType: c.Query("resource_type"),
		ResourceID:   c.Query("resource_id"),
		Cursor:       c.Query("cursor"),
		Limit:        parseAuditLimit(c.Query("limit")),
	}
	if !systemScope {
		spaceID, ok := h.resolveAuditSpaceID(c)
		if !ok {
			return
		}
		query.SpaceID = spaceID
	}
	from, ok := parseAuditTime(c, "from")
	if !ok {
		return
	}
	to, ok := parseAuditTime(c, "to")
	if !ok {
		return
	}
	query.From = from
	query.To = to

	page, err := h.audit.List(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_QUERY", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, auditPageResponse(page))
}

func parseAuditLimit(raw string) int {
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return limit
}

func parseAuditTime(c *gin.Context, name string) (time.Time, bool) {
	raw := c.Query(name)
	if raw == "" {
		return time.Time{}, true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return t, true
		}
	}
	c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TIME", "message": name + " 时间格式无效"})
	return time.Time{}, false
}

func (h *Handler) resolveAuditSpaceID(c *gin.Context) (string, bool) {
	spaceID := strings.TrimSpace(c.Query("space_id"))
	if spaceID == "" {
		return h.resolveSpaceID(c)
	}
	if !validSpaceID(spaceID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_SPACE", "message": "Space ID 不合法"})
		return "", false
	}
	exists, err := h.library.SpaceExists(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "校验 Space 失败"})
		return "", false
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": "SPACE_NOT_FOUND", "message": "Space 不存在"})
		return "", false
	}
	return spaceID, true
}

type auditEventResponse struct {
	ID           int64           `json:"id"`
	Scope        string          `json:"scope"`
	SpaceID      *string         `json:"space_id"`
	ActorType    string          `json:"actor_type"`
	ActorID      string          `json:"actor_id,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id,omitempty"`
	BeforeJSON   json.RawMessage `json:"before_json"`
	AfterJSON    json.RawMessage `json:"after_json"`
	MetadataJSON json.RawMessage `json:"metadata_json"`
	RequestID    string          `json:"request_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type auditPageDTO struct {
	Items      []auditEventResponse `json:"items"`
	NextCursor *string              `json:"next_cursor"`
}

func auditPageResponse(page audit.Page) auditPageDTO {
	items := make([]auditEventResponse, 0, len(page.Items))
	for _, event := range page.Items {
		items = append(items, auditEventResponse{
			ID:           event.ID,
			Scope:        event.Scope,
			SpaceID:      event.SpaceID,
			ActorType:    event.ActorType,
			ActorID:      event.ActorID,
			Action:       event.Action,
			ResourceType: event.ResourceType,
			ResourceID:   event.ResourceID,
			BeforeJSON:   auditJSONValue(event.BeforeJSON),
			AfterJSON:    auditJSONValue(event.AfterJSON),
			MetadataJSON: auditJSONValue(event.MetadataJSON),
			RequestID:    event.RequestID,
			CreatedAt:    event.CreatedAt,
		})
	}
	return auditPageDTO{Items: items, NextCursor: auditCursorValue(page.NextCursor)}
}

func auditCursorValue(cursor string) *string {
	if strings.TrimSpace(cursor) == "" {
		return nil
	}
	return &cursor
}

func auditJSONValue(raw string) json.RawMessage {
	if strings.TrimSpace(raw) == "" {
		return json.RawMessage("null")
	}
	if !json.Valid([]byte(raw)) {
		return json.RawMessage("null")
	}
	return json.RawMessage(raw)
}
