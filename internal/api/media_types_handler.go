package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/library"
)

// ListMediaTypes 查询媒体类型定义与当前生效规则。
func (h *Handler) ListMediaTypes(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	libraryID := parseOptionalInt64(c.Query("library_id"))
	data, err := h.library.ListMediaTypesInSpace(spaceID, libraryID)
	if err != nil {
		writeMediaTypeError(c, err)
		return
	}
	filterType := c.Query("type")
	if filterType != "" {
		data.Rules = filterMediaTypeRules(data.Rules, filterType)
	}
	c.JSON(http.StatusOK, data)
}

// CreateMediaTypeRule 新增全局或媒体库级规则。
func (h *Handler) CreateMediaTypeRule(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req library.MediaTypeRuleInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	rule, err := h.library.CreateMediaTypeRuleInSpace(spaceID, req)
	if err != nil {
		writeMediaTypeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, rule)
}

// UpdateMediaTypeRule 更新规则启用状态或说明。
func (h *Handler) UpdateMediaTypeRule(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req library.MediaTypeRuleUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	if req.LibraryID == nil {
		req.LibraryID = optionalInt64Ptr(c.Query("library_id"))
	}
	rule, err := h.library.UpdateMediaTypeRuleInSpace(spaceID, c.Param("id"), req)
	if err != nil {
		writeMediaTypeError(c, err)
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteMediaTypeRule 删除自定义规则，内置规则只能禁用。
func (h *Handler) DeleteMediaTypeRule(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if err := h.library.DeleteMediaTypeRuleInSpace(spaceID, c.Param("id")); err != nil {
		writeMediaTypeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func parseOptionalInt64(raw string) int64 {
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func optionalInt64Ptr(raw string) *int64 {
	value := parseOptionalInt64(raw)
	if value <= 0 {
		return nil
	}
	return &value
}

func filterMediaTypeRules(rules []library.MediaTypeRuleView, mediaType string) []library.MediaTypeRuleView {
	filtered := make([]library.MediaTypeRuleView, 0, len(rules))
	for _, rule := range rules {
		if rule.Type == mediaType {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

func writeMediaTypeError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "资源不存在"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"code": "MEDIA_TYPE_RULE_FAILED", "message": err.Error()})
}
