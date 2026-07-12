package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// StartHealthScan POST /api/library/health/scan
// 触发后台媒体健康巡检（FR-73）。已有巡检在跑时返回 already_running，不重复启动。
func (h *Handler) StartHealthScan(c *gin.Context) {
	if h.health == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HEALTH_UNAVAILABLE", "message": "健康巡检服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	if !h.health.StartScanInSpace(spaceID) {
		c.JSON(http.StatusOK, gin.H{"status": "already_running"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "scanning"})
}

// HealthStatus GET /api/library/health/status
// 返回当前健康巡检进度（FR-73）：idle / scanning / completed / error 及计数。
func (h *Handler) HealthStatus(c *gin.Context) {
	if h.health == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HEALTH_UNAVAILABLE", "message": "健康巡检服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.health.StatusInSpace(spaceID))
}

// HealthIssues GET /api/library/health/issues
// 返回当前问题清单（FR-73），每项附带媒体基本信息，供前端按问题类型分组展示。
func (h *Handler) HealthIssues(c *gin.Context) {
	if h.health == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "HEALTH_UNAVAILABLE", "message": "健康巡检服务未启用"})
		return
	}

	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	issues, err := h.health.ListIssuesInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询问题清单失败"})
		return
	}

	// 附带媒体基本信息（文件名 / 路径 / 库），供前端直接展示而不必逐条再查媒体
	items := make([]gin.H, 0, len(issues))
	for _, issue := range issues {
		item := gin.H{
			"id":         issue.ID,
			"media_id":   issue.MediaID,
			"issue_type": issue.IssueType,
			"detail":     issue.Detail,
			"checked_at": issue.CheckedAt,
		}
		if mf, err := h.library.GetMediaFileByIDInSpace(spaceID, issue.MediaID); err == nil {
			item["file_name"] = mf.FileName
			item["file_path"] = mf.FilePath
			item["library_id"] = mf.LibraryID
			item["display_name"] = mf.DisplayName
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
