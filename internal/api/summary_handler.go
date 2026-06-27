package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// LibrarySummary GET /api/library/summary
// 返回媒体库总量聚合（FR-117）：媒体总数、视频/图片拆分、占用空间、总时长、
// 启用库数与各库分组聚合，供首页概览看板的 KPI 大数卡与各库分布展示。
func (h *Handler) LibrarySummary(c *gin.Context) {
	summary, err := h.library.GetLibrarySummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询概览失败"})
		return
	}
	c.JSON(http.StatusOK, summary)
}
