package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 感知哈希去重端点（FR-70）：扫描计算缺失 dHash + 查询重复组。

// ScanDuplicates POST /api/library/duplicates/scan
// 为缺 dHash 的未软删媒体计算并持久化哈希（缩略图缺失则现场生成），返回本次新算条数。
// 同步执行 + 有界并发；单条失败仅记日志跳过，不影响整体（详见 library.ComputeMissingDHashes）。
func (h *Handler) ScanDuplicates(c *gin.Context) {
	computed, err := h.library.ComputeMissingDHashes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DEDUP_SCAN_FAILED", "message": "去重扫描失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"computed": computed})
}

// ListDuplicates GET /api/library/duplicates
// 返回按汉明距离阈值聚类的重复组（每组 ≥2 项、排除软删），供「重复项」页展示与批量清理候选。
func (h *Handler) ListDuplicates(c *gin.Context) {
	groups, err := h.library.FindDuplicateGroups(h.library.DedupThreshold())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询重复组失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups})
}
