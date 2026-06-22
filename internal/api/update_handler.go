package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// CheckUpdate GET /api/system/update/check?channel=stable|prerelease
// 检测 GitHub Releases 是否有可应用的新版本（FR-46）。
func (h *Handler) CheckUpdate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	res, err := h.updateSvc.Check(ctx, h.versionOrDefault(), c.Query("channel"))
	if err != nil {
		log.Printf("[WARN] 检查更新失败: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"code": "UPDATE_CHECK_FAILED", "message": "检查更新失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// ApplyUpdate POST /api/system/update/apply  body: {channel}
// 下载校验目标版本后替换二进制并自动重启（FR-46）。
// 下载用独立 context（不随请求取消），成功后进程将在短延时退出由新进程接管。
func (h *Handler) ApplyUpdate(c *gin.Context) {
	var req struct {
		Channel string `json:"channel"`
	}
	_ = c.ShouldBindJSON(&req)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := h.updateSvc.Apply(ctx, h.versionOrDefault(), req.Channel); err != nil {
		log.Printf("[ERROR] 自更新失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新失败：" + err.Error()})
		return
	}
	log.Printf("[INFO] 自更新已应用，服务即将重启")
	c.JSON(http.StatusOK, gin.H{"status": "updating", "message": "更新已应用，服务即将重启"})
}

// RollbackUpdate POST /api/system/update/rollback
// 回滚到上一版二进制并重启（FR-46）。
func (h *Handler) RollbackUpdate(c *gin.Context) {
	if err := h.updateSvc.Rollback(); err != nil {
		log.Printf("[ERROR] 回滚失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ROLLBACK_FAILED", "message": "回滚失败：" + err.Error()})
		return
	}
	log.Printf("[INFO] 已回滚到上一版本，服务即将重启")
	c.JSON(http.StatusOK, gin.H{"status": "rolling_back", "message": "已回滚到上一版本，服务即将重启"})
}
