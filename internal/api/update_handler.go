package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/settings"
)

// resolveUpdateChannel 解析更新频道：显式参数优先；否则读持久化设置（FR-46 测试/正式频道），
// 都没有时回退正式版（stable）。让「检查/更新」按用户持久化的频道走。
func (h *Handler) resolveUpdateChannel(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if h.settings != nil {
		if v, err := h.settings.Get(settings.KeyUpdateChannel); err == nil && v != "" {
			return v
		}
	}
	return "stable"
}

// CheckUpdate GET /api/system/update/check?channel=stable|prerelease&force=true
// 检测 GitHub Releases 是否有可应用的新版本（FR-46）。channel 省略时用持久化设置；
// force=true 跳过 TTL 缓存强制重新检测。
// 超时设 30s 与 update 服务的 http.Client（30s）对齐，给国内直连 GitHub 的慢请求留时间。
func (h *Handler) CheckUpdate(c *gin.Context) {
	force := c.Query("force") == "true"
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.updateSvc.Check(ctx, h.versionOrDefault(), h.resolveUpdateChannel(c.Query("channel")), force)
	if err != nil {
		// 记录原始错误便于排查，但不向用户回显裸超时/底层错误，给出友好提示。
		log.Printf("[WARN] 检查更新失败: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    "UPDATE_CHECK_FAILED",
			"message": "检查更新失败：网络不可达或 GitHub 暂时不可用，请稍后重试",
		})
		return
	}
	// 实时计算「是否可回滚」（不入缓存，避免 .old 状态过期），供前端决定回滚按钮显隐（FIX-2）
	res.RollbackAvailable = h.updateSvc.RollbackAvailable()
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
	if err := h.updateSvc.Apply(ctx, h.versionOrDefault(), h.resolveUpdateChannel(req.Channel)); err != nil {
		log.Printf("[ERROR] 自更新失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新失败：" + err.Error()})
		return
	}
	log.Printf("[INFO] 自更新已应用，服务即将重启")
	c.JSON(http.StatusOK, gin.H{"status": "updating", "message": "更新已应用，服务即将重启"})
}

// UpdateProgress GET /api/system/update/progress
// 返回自更新下载进度（FR-90）：{state, downloaded, total, percent}，供前端轮询展示进度条。
// 进度为进程内单例，无外部依赖恒可用；空闲时返回 state=idle。
func (h *Handler) UpdateProgress(c *gin.Context) {
	c.JSON(http.StatusOK, h.updateSvc.Progress())
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
