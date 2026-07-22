package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
)

// PlayHandler 播放请求处理器。
type PlayHandler struct {
	library  *library.Service
	playback *playback.Service
}

// NewPlayHandler 创建播放处理器。
func NewPlayHandler(lib *library.Service, pb *playback.Service) *PlayHandler {
	return &PlayHandler{
		library:  lib,
		playback: pb,
	}
}

// Stream 处理文件流播放请求（支持 Range）。
// GET /api/play/:id/stream
func (h *PlayHandler) Stream(c *gin.Context) {
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

	// 检查文件是否存在
	if _, err := os.Stat(mf.FilePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"code": "FILE_NOT_FOUND", "message": "文件不存在"})
		return
	}

	// 设置响应头
	c.Header("Accept-Ranges", "bytes")

	// 获取文件名用于 Content-Disposition
	filename := filepath.Base(mf.FilePath)

	// 使用 playback 服务处理 Range 请求
	h.playback.StreamFile(c.Writer, c.Request, id, mf.FilePath, mf.FileSize, mf.Duration)

	// 避免 gin 再次写入响应
	c.Header("Content-Disposition", "inline; filename="+url.PathEscape(filename))
}

// Seek 处理 Seek 请求。
// POST /api/play/:id/seek
func (h *PlayHandler) Seek(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	var req playback.SeekRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	resp, err := h.playback.HandleSeek(id, req.Position)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SEEK_FAILED", "message": "Seek 失败"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetProgress 获取播放进度信息。
// GET /api/play/:id/progress
func (h *PlayHandler) GetProgress(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	progress, err := h.playback.GetProgress(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, progress)
}

// ReportBuffer 处理缓冲区间上报。
// POST /api/play/:id/buffer
func (h *PlayHandler) ReportBuffer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "无效的 ID"})
		return
	}

	var report playback.BufferReport
	if err := c.ShouldBindJSON(&report); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}

	h.playback.HandleBufferReport(id, report)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
