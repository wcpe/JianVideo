package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/tools"
)

// ListTools GET /api/system/tools
func (h *Handler) ListTools(c *gin.Context) {
	if h.tools == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TOOLS_UNAVAILABLE", "message": "工具下载服务未启用"})
		return
	}
	status, err := h.tools.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TOOLS_STATUS_FAILED", "message": "查询工具状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": status})
}

// ListToolSources GET /api/system/tools/sources
func (h *Handler) ListToolSources(c *gin.Context) {
	if h.tools == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TOOLS_UNAVAILABLE", "message": "工具下载服务未启用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sources": h.tools.Sources()})
}

// DownloadTool POST /api/system/tools/download
func (h *Handler) DownloadTool(c *gin.Context) {
	if h.tools == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "TOOLS_UNAVAILABLE", "message": "工具下载服务未启用"})
		return
	}
	var req struct {
		Tool              string `json:"tool"`
		SourceID          string `json:"source_id"`
		CustomURL         string `json:"custom_url"`
		SHA256            string `json:"sha256"`
		Version           string `json:"version"`
		AllowInsecureHTTP bool   `json:"allow_insecure_http"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	task, err := h.tools.EnqueueDownload(c.Request.Context(), tools.DownloadRequest{
		Tool:              req.Tool,
		SourceID:          req.SourceID,
		CustomURL:         req.CustomURL,
		SHA256:            req.SHA256,
		Version:           req.Version,
		AllowInsecureHTTP: req.AllowInsecureHTTP,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "TOOL_DOWNLOAD_REJECTED", "message": err.Error()})
		return
	}
	h.triggerTaskWorkers()
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "task_id": strconv.FormatInt(task.ID, 10)})
}
