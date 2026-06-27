package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// MediaTrends GET /api/library/trends
// 返回「按天新增媒体」全时段序列（FR-118，增强 FR-75）：按 added_at 本地时区天分桶的
// count / SUM(file_size) / SUM(duration)，仅含有新增的天、升序，供统计页媒体增长曲线前端累加。
func (h *Handler) MediaTrends(c *gin.Context) {
	trends, err := h.library.GetMediaTrends()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询趋势失败"})
		return
	}
	c.JSON(http.StatusOK, trends)
}
