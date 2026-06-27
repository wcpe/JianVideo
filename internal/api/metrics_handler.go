package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SystemMetrics GET /api/system/metrics?range=1h|24h|7d
// 返回系统指标下采样时序（points）与当前快照（current），供监控页折线与当前值卡（FR-119）。
// range 缺省/未知按 24h 处理；刚启动无样本时 points 为空、current 为 null，仍返回 200。
func (h *Handler) SystemMetrics(c *gin.Context) {
	if h.metrics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "UNAVAILABLE", "message": "指标采样器未启用"})
		return
	}
	result, err := h.metrics.GetMetrics(c.Query("range"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询系统指标失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}
