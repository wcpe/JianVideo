// Package api 提供 Web 层的 HTTP 路由与请求处理器，向下经 media-library 协调业务。
package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// parseAlbumID 解析路由中的相册 ID 参数。
// 返回 (id, ok)；解析失败时已写入 400 响应，调用方直接返回即可。
func parseAlbumID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的相册 ID"})
		return 0, false
	}
	return id, true
}

// CreateAlbum POST /api/albums
func (h *Handler) CreateAlbum(c *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	album, err := h.library.CreateAlbum(req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, album)
}

// ListAlbums GET /api/albums
func (h *Handler) ListAlbums(c *gin.Context) {
	items, err := h.library.ListAlbums()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// DeleteAlbum DELETE /api/albums/:id
// 仅删除相册与其成员关联，不删除源文件与媒体记录。
func (h *Handler) DeleteAlbum(c *gin.Context) {
	id, ok := parseAlbumID(c)
	if !ok {
		return
	}
	if err := h.library.DeleteAlbum(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "相册不存在"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListAlbumItems GET /api/albums/:id/items
func (h *Handler) ListAlbumItems(c *gin.Context) {
	id, ok := parseAlbumID(c)
	if !ok {
		return
	}
	files, err := h.library.ListAlbumItems(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": files})
}

// AddAlbumItem POST /api/albums/:id/items
// 请求体：{"media_id": 123}，把指定媒体加入相册（重复幂等）。
func (h *Handler) AddAlbumItem(c *gin.Context) {
	id, ok := parseAlbumID(c)
	if !ok {
		return
	}
	var req struct {
		MediaID int64 `json:"media_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	if err := h.library.AddAlbumItem(id, req.MediaID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "相册或媒体不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ADD_FAILED", "message": "加入相册失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// RemoveAlbumItem DELETE /api/albums/:id/items/:mediaId
func (h *Handler) RemoveAlbumItem(c *gin.Context) {
	id, ok := parseAlbumID(c)
	if !ok {
		return
	}
	mediaID, err := strconv.ParseInt(c.Param("mediaId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的媒体 ID"})
		return
	}
	if err := h.library.RemoveAlbumItem(id, mediaID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "REMOVE_FAILED", "message": "移出相册失败"})
		return
	}
	c.Status(http.StatusNoContent)
}
