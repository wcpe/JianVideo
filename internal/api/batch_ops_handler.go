package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

const batchOpsMaxIDs = 100

// BatchTranscodeMediaFiles POST /api/library/media/batch-transcode（FR2-053）。
// body: { "ids": [...], "preset_id": N }；仅对视频入队，图片与缺失项计入 skipped。
func (h *Handler) BatchTranscodeMediaFiles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		IDs      []int64 `json:"ids"`
		PresetID int64   `json:"preset_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"queued": 0, "skipped": 0, "failed": 0, "task_ids": []int64{}})
		return
	}
	if len(req.IDs) > batchOpsMaxIDs {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TOO_MANY_IDS", "message": "单次最多处理 100 项"})
		return
	}
	if req.PresetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "preset_id 必填且为正"})
		return
	}
	if h.presets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "PRESETS_UNAVAILABLE", "message": "转码预设服务未启用"})
		return
	}
	preset, err := h.presets.Get(req.PresetID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "PRESET_NOT_FOUND", "message": "预设不存在"})
		return
	}
	if h.hlsPreview == nil && h.pregenQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TRANSCODE_UNAVAILABLE", "message": "转码服务未启用"})
		return
	}

	items, err := h.library.GetMediaFilesByIDsInSpace(spaceID, req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询媒体失败"})
		return
	}
	byID := make(map[int64]models.MediaFile, len(items))
	for i := range items {
		byID[items[i].ID] = items[i]
	}

	var queued, skipped, failed int
	taskIDs := make([]int64, 0, len(req.IDs))
	for _, id := range req.IDs {
		mf, found := byID[id]
		if !found {
			skipped++
			continue
		}
		if isImageMediaFormat(mf.Format) {
			skipped++
			continue
		}
		taskID, enqErr := h.enqueueBatchTranscode(c, spaceID, mf.ID, preset)
		if enqErr != nil {
			failed++
			continue
		}
		queued++
		taskIDs = append(taskIDs, taskID)
	}
	if queued > 0 && h.taskWorkers != nil {
		h.taskWorkers.Wake()
	}
	c.JSON(http.StatusOK, gin.H{
		"queued":   queued,
		"skipped":  skipped,
		"failed":   failed,
		"task_ids": taskIDs,
	})
}

func (h *Handler) enqueueBatchTranscode(c *gin.Context, spaceID string, mediaID int64, preset *models.TranscodePreset) (int64, error) {
	if h.hlsPreview != nil {
		task, err := h.hlsPreview.Enqueue(c.Request.Context(), transcoder.HLSPreviewRequest{
			SpaceID: spaceID, MediaID: mediaID, PresetID: preset.ID, ProfileID: preset.Codec,
			Codec: preset.Codec, Width: preset.Width, Height: preset.Height,
		})
		if err != nil {
			return 0, err
		}
		return task.ID, nil
	}
	return h.pregenQueue.EnqueueInSpace(spaceID, mediaID, preset.ID, preset.Codec, preset.Width, preset.Height)
}

// BatchMoveMediaFiles POST /api/library/media/batch-move（FR2-053 索引层）。
// body: { "ids": [...], "target_library_id": N }；仅改 library_id，不搬磁盘文件。
func (h *Handler) BatchMoveMediaFiles(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		IDs             []int64 `json:"ids"`
		TargetLibraryID int64   `json:"target_library_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"moved": 0, "skipped": 0})
		return
	}
	if len(req.IDs) > batchOpsMaxIDs {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TOO_MANY_IDS", "message": "单次最多处理 100 项"})
		return
	}
	if req.TargetLibraryID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "target_library_id 必填且为正"})
		return
	}
	result, err := h.library.BatchReassignLibraryInSpace(spaceID, req.IDs, req.TargetLibraryID)
	if err != nil {
		if errors.Is(err, library.ErrBatchTargetLibraryNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "LIBRARY_NOT_FOUND", "message": "目标媒体库不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "MOVE_FAILED", "message": "批量移动失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"moved": result.Moved, "skipped": result.Skipped})
}

func isImageMediaFormat(format string) bool {
	ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if ext == "" {
		return false
	}
	// 与 library 内置图片后缀对齐的最小集合，避免 handler 依赖未导出 map
	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "tif", "tiff", "heic", "heif",
		"cr2", "nef", "arw", "dng", "rw2", "raf", "orf", "srw", "pef":
		return true
	default:
		return false
	}
}
