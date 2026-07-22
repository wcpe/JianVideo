package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// WatchStats GET /api/library/stats
// 返回观看统计聚合（FR-75）：已看/未看计数、最近观看时间线、续播位置热力、
// 各库/各格式观看分布、观看次数 Top N，供观看统计页展示。
func (h *Handler) WatchStats(c *gin.Context) {
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	stats, err := h.library.GetWatchStatsInSpace(spaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询统计失败"})
		return
	}
	c.JSON(http.StatusOK, stats)
}
