package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// ImageExportMedia POST /api/library/media/:id/image-export
// 入队图片编辑导出任务（FR2-038）。
func (h *Handler) ImageExportMedia(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	mediaID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || mediaID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的媒体 ID"})
		return
	}
	var req struct {
		Exposure   float64 `json:"exposure"`
		Contrast   float64 `json:"contrast"`
		Saturation float64 `json:"saturation"`
		Temp       float64 `json:"temperature"`
		Format     string  `json:"format"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	params := library.ImageExportParams{
		Exposure:   req.Exposure,
		Contrast:   req.Contrast,
		Saturation: req.Saturation,
		Temp:       req.Temp,
		Format:     req.Format,
	}
	if err := library.ValidateImageParams(params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": err.Error()})
		return
	}
	mf, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "MEDIA_NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	if strings.HasPrefix(mf.FilePath, "smb://") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": "SMB 文件暂不支持导出"})
		return
	}
	task, err := library.EnqueueImageExport(c.Request.Context(), h.tasks, spaceID, mediaID, params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TASK_ENQUEUE_FAILED", "message": "图片导出入队失败: " + err.Error()})
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{
		"status":  "queued",
		"task_id": strconv.FormatInt(task.ID, 10),
	})
}

// ClipExportMedia POST /api/library/media/:id/clip-export
// 入队视频片段粗剪导出任务（FR2-039）。
func (h *Handler) ClipExportMedia(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	mediaID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || mediaID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的媒体 ID"})
		return
	}
	var req struct {
		StartSec float64 `json:"start_sec"`
		EndSec   float64 `json:"end_sec"`
		Format   string  `json:"format"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	mf, err := h.library.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "MEDIA_NOT_FOUND", "message": "媒体文件不存在"})
		return
	}
	if strings.HasPrefix(mf.FilePath, "smb://") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "UNSUPPORTED_PATH", "message": "SMB 文件暂不支持导出"})
		return
	}
	params := library.VideoClipParams{
		StartSec: req.StartSec,
		EndSec:   req.EndSec,
		Format:   req.Format,
	}
	if err := library.ValidateVideoClipParams(params, mf.Duration, 0); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": err.Error()})
		return
	}
	task, err := library.EnqueueVideoClip(c.Request.Context(), h.tasks, spaceID, mediaID, params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TASK_ENQUEUE_FAILED", "message": "视频粗剪入队失败: " + err.Error()})
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{
		"status":  "queued",
		"task_id": strconv.FormatInt(task.ID, 10),
	})
}

// DownloadExportArtifact GET /api/library/exports/:task_id/download
// 下载导出产物。仅允许下载已完成且归属当前 Space 的任务产物。
func (h *Handler) DownloadExportArtifact(c *gin.Context) {
	if h.tasks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TASKS_UNAVAILABLE", "message": "任务中心未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	taskID, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的任务 ID"})
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), taskID, tasksvc.Query{
		Scope:   "space",
		SpaceID: spaceID,
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "TASK_NOT_FOUND", "message": "任务不存在"})
		return
	}
	if task.Type != library.TaskTypeImageExport && task.Type != library.TaskTypeVideoClip {
		c.JSON(http.StatusNotFound, gin.H{"code": "TASK_NOT_FOUND", "message": "任务类型不支持下载"})
		return
	}
	if task.Status != models.TaskStatusSucceeded {
		c.JSON(http.StatusConflict, gin.H{"code": "TASK_NOT_READY", "message": "任务未完成"})
		return
	}
	var result library.ExportTaskResult
	if err := json.Unmarshal([]byte(task.Checkpoint), &result); err != nil || strings.TrimSpace(result.OutputPath) == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": "RESULT_MISSING", "message": "导出产物不可用"})
		return
	}
	if !fileExists(result.OutputPath) {
		c.JSON(http.StatusNotFound, gin.H{"code": "FILE_NOT_FOUND", "message": "导出产物文件不存在"})
		return
	}
	filename := result.Filename
	if filename == "" {
		filename = fmt.Sprintf("export-%d", taskID)
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.File(result.OutputPath)
}
