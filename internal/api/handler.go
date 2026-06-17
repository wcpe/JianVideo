package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/library"
)

// Handler API 请求处理器。
type Handler struct {
	library *library.Service
}

// NewHandler 创建处理器。
func NewHandler(lib *library.Service) *Handler {
	return &Handler{library: lib}
}

// ListLibraryPaths GET /api/library/paths
func (h *Handler) ListLibraryPaths(c *gin.Context) {
	items, err := h.library.ListLibraryPaths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateLibraryPath POST /api/library/paths
func (h *Handler) CreateLibraryPath(c *gin.Context) {
	var req struct {
		Path  string `json:"path" binding:"required"`
		Type  string `json:"type" binding:"required"`
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	lp, err := h.library.CreateLibraryPath(req.Path, req.Type, req.Label)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "添加失败"})
		return
	}
	c.JSON(http.StatusCreated, lp)
}

// DeleteLibraryPath DELETE /api/library/paths/:id
func (h *Handler) DeleteLibraryPath(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.DeleteLibraryPath(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListMediaFiles GET /api/library/media
func (h *Handler) ListMediaFiles(c *gin.Context) {
	libraryID, _ := strconv.ParseInt(c.Query("library_id"), 10, 64)
	sort := c.DefaultQuery("sort", "time_desc")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")

	items, total, err := h.library.ListMediaFiles(libraryID, sort, search, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetMediaFile GET /api/library/media/:id
func (h *Handler) GetMediaFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	mf, err := h.library.GetMediaFileByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	c.JSON(http.StatusOK, mf)
}

// DeleteMediaFile DELETE /api/library/media/:id
func (h *Handler) DeleteMediaFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	if err := h.library.DeleteMediaFile(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ScanLibrary POST /api/library/scan/:id
func (h *Handler) ScanLibrary(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	lp, err := h.library.GetLibraryPathByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "目录不存在"})
		return
	}

	count, err := h.library.ScanLibrary(id, lp.Path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SCAN_FAILED", "message": "扫描失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scanned": count})
}
