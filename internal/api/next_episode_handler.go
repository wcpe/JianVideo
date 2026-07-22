package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetNextEpisode GET /api/library/media/:id/next-episode（FR2-047）。
// 同 Space 内按推断 title+season+episode 定位下一集；无下一集时 media 为 null。
func (h *Handler) GetNextEpisode(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	id, ok := parseMediaID(c)
	if !ok {
		return
	}
	result, err := h.library.FindNextEpisodeInSpace(spaceID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询下一集失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetAlbumNeighbor GET /api/albums/:id/neighbor?media_id=&dir=next|prev（FR2-047）。
// 按合集成员顺序返回相邻媒体；越界 media 为 null。
func (h *Handler) GetAlbumNeighbor(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	albumID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || albumID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的相册 ID"})
		return
	}
	mediaID, err := strconv.ParseInt(c.Query("media_id"), 10, 64)
	if err != nil || mediaID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_MEDIA_ID", "message": "media_id 必填且为正"})
		return
	}
	dir := c.DefaultQuery("dir", "next")
	direction := 1
	switch dir {
	case "next", "":
		direction = 1
	case "prev":
		direction = -1
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_DIR", "message": "dir 仅支持 next 或 prev"})
		return
	}
	media, err := h.library.FindAlbumNeighborInSpace(spaceID, albumID, mediaID, direction)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 相册不存在或媒体不在相册
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "相册或媒体不在合集中"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询合集邻项失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"media": media})
}
