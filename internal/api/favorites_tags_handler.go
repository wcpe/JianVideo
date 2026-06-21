package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"jianvideo/internal/library"
)

// SetMediaFavorite PUT /api/library/media/:id/favorite
// 请求体：{"favorite": true}，设置或取消收藏，返回更新后的媒体对象。
func (h *Handler) SetMediaFavorite(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	mf, err := h.library.SetMediaFavorite(id, req.Favorite)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "FAVORITE_FAILED", "message": "设置收藏失败"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// ListTags GET /api/library/tags
func (h *Handler) ListTags(c *gin.Context) {
	tags, err := h.library.ListTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": tags})
}

// CreateTag POST /api/library/tags
// 请求体：{"name": "..."}，按名唯一创建，返回标签对象。
func (h *Handler) CreateTag(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	tag, err := h.library.CreateTag(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TAG", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, tag)
}

// ListMediaTags GET /api/library/media/:id/tags
func (h *Handler) ListMediaTags(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	tags, err := h.library.ListMediaTags(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": tags})
}

// AddMediaTag POST /api/library/media/:id/tags
// 请求体：{"tag_id": 1} 或 {"name": "..."}（按名时先建/取标签再绑定），返回绑定的标签。
func (h *Handler) AddMediaTag(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	var req struct {
		TagID int64  `json:"tag_id"`
		Name  string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}

	tagID := req.TagID
	// 按名打标签：先建/取标签拿到 ID
	if tagID == 0 {
		if req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "需提供 tag_id 或 name"})
			return
		}
		tag, err := h.library.CreateTag(req.Name)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TAG", "message": err.Error()})
			return
		}
		tagID = tag.ID
	}

	if err := h.library.AddMediaTag(id, tagID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "ADD_TAG_FAILED", "message": err.Error()})
		return
	}

	// 回读标签对象返回，便于前端直接展示
	tags, err := h.library.ListMediaTags(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	for _, t := range tags {
		if t.ID == tagID {
			c.JSON(http.StatusCreated, t)
			return
		}
	}
	c.Status(http.StatusCreated)
}

// RemoveMediaTag DELETE /api/library/media/:id/tags/:tag_id
func (h *Handler) RemoveMediaTag(c *gin.Context) {
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	tagID, err := strconv.ParseInt(c.Param("tag_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_TAG_ID", "message": "无效的标签 ID"})
		return
	}
	if err := h.library.RemoveMediaTag(id, tagID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "REMOVE_TAG_FAILED", "message": "去标签失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// parseMediaFilter 从查询参数解析收藏/标签筛选条件（供 ListMediaFiles 使用）。
func parseMediaFilter(c *gin.Context, libraryID int64, sort, search string) library.MediaFilter {
	filter := library.MediaFilter{
		LibraryID: libraryID,
		Sort:      sort,
		Search:    search,
	}
	if fav := c.Query("favorite"); fav != "" {
		v := fav == "true" || fav == "1"
		filter.Favorite = &v
	}
	if tagID, err := strconv.ParseInt(c.Query("tag_id"), 10, 64); err == nil && tagID > 0 {
		filter.TagID = tagID
	}
	return filter
}
