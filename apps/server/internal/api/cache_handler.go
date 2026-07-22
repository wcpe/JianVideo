package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/storage"
)

// StorageCacheSummary 处理缓存汇总查询请求。
func (h *Handler) StorageCacheSummary(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "CACHE_UNAVAILABLE", "message": "缓存管理服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	summary, err := h.cache.Summary(c.Request.Context(), storage.SummaryQuery{
		SpaceID:   spaceID,
		Kind:      strings.TrimSpace(c.Query("kind")),
		LibraryID: parseOptionalID(c.Query("library_id")),
		MediaID:   parseOptionalID(c.Query("media_id")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CACHE_SUMMARY_FAILED", "message": "缓存统计失败"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

// StorageCacheAssets 处理缓存资产分页查询请求。
func (h *Handler) StorageCacheAssets(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "CACHE_UNAVAILABLE", "message": "缓存管理服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	page, err := h.cache.ListAssets(c.Request.Context(), storage.AssetQuery{
		SpaceID:   spaceID,
		Kind:      strings.TrimSpace(c.Query("kind")),
		LibraryID: parseOptionalID(c.Query("library_id")),
		MediaID:   parseOptionalID(c.Query("media_id")),
		Page:      parsePositiveInt(c.Query("page"), 1),
		PageSize:  parsePositiveInt(c.Query("page_size"), 20),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CACHE_ASSETS_FAILED", "message": "缓存资产查询失败"})
		return
	}
	c.JSON(http.StatusOK, page)
}

// StorageCacheInventory 处理缓存资产盘点请求。
func (h *Handler) StorageCacheInventory(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "CACHE_UNAVAILABLE", "message": "缓存管理服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	result, err := h.cache.Inventory(c.Request.Context(), storage.InventoryInput{SpaceID: spaceID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CACHE_INVENTORY_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, result)
}

// StorageCacheClean 处理缓存清理预览与执行请求。
func (h *Handler) StorageCacheClean(c *gin.Context) {
	if h.cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "CACHE_UNAVAILABLE", "message": "缓存管理服务未启用"})
		return
	}
	spaceID, ok := h.resolveSpaceID(c)
	if !ok {
		return
	}
	var req struct {
		Kinds  []string `json:"kinds"`
		DryRun bool     `json:"dry_run"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "请求体无效"})
		return
	}
	result, err := h.cache.Clean(c.Request.Context(), storage.CleanInput{
		SpaceID: spaceID,
		Kinds:   req.Kinds,
		DryRun:  req.DryRun,
	})
	if err != nil {
		if errors.Is(err, storage.ErrInvalidKind) || errors.Is(err, storage.ErrUnsafeCachePath) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_CACHE_CLEAN", "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CACHE_CLEAN_FAILED", "message": err.Error()})
		return
	}
	if req.DryRun {
		c.JSON(http.StatusOK, result)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func parseOptionalID(raw string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return value
}
