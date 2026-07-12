package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/library"
)

// GetMediaMetadata 返回媒体的当前文件自带元数据记录。
func (h *Handler) GetMediaMetadata(c *gin.Context) {
	spaceID, mediaID, ok := h.metadataRequestMedia(c)
	if !ok {
		return
	}
	items, err := h.library.ListMediaMetadata(c.Request.Context(), spaceID, mediaID)
	if err != nil {
		metadataReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RefreshMediaMetadata 标记 stale 并创建单文件解析任务。
func (h *Handler) RefreshMediaMetadata(c *gin.Context) {
	spaceID, mediaID, ok := h.metadataRequestMedia(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	media, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		metadataReadError(c, err)
		return
	}
	if err := h.library.MarkMediaMetadataStale(c.Request.Context(), spaceID, mediaID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "METADATA_STALE_FAILED", "message": "标记元数据过期失败"})
		return
	}
	task, err := library.EnqueueMetadataParse(c.Request.Context(), h.tasks, *media)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TASK_ENQUEUE_FAILED", "message": "元数据刷新入队失败"})
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{"status": task.Status, "task_id": task.ID})
}

// BackfillMediaMetadata 创建 Space 或指定媒体库的批量回填任务。
func (h *Handler) BackfillMediaMetadata(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	var request struct {
		LibraryID int64 `json:"library_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	if request.LibraryID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "媒体库 ID 不能为负数"})
		return
	}
	if request.LibraryID > 0 {
		if _, err := h.library.GetLibraryPathByIDInSpace(spaceID, request.LibraryID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体库不存在"})
			return
		}
	}
	task, err := library.EnqueueMetadataBackfill(c.Request.Context(), h.tasks, spaceID, request.LibraryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TASK_ENQUEUE_FAILED", "message": "元数据回填入队失败"})
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{"status": task.Status, "task_id": task.ID})
}

func (h *Handler) metadataRequestMedia(c *gin.Context) (string, int64, bool) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return "", 0, false
	}
	mediaID, ok := parseMediaID(c)
	return spaceID, mediaID, ok
}

func metadataReadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "METADATA_READ_FAILED", "message": "读取媒体元数据失败"})
}
