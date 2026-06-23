package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// GetSettings GET /api/settings
// 返回全部运行期设置，形如 {"settings": {"scan_interval": "3600", ...}}。
func (h *Handler) GetSettings(c *gin.Context) {
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}

	all, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询设置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": all})
}

// UpdateSettings PUT /api/settings
// 批量写入设置，请求体形如 {"settings": {"scan_interval": "3600", ...}}。
func (h *Handler) UpdateSettings(c *gin.Context) {
	if h.settings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SETTINGS_UNAVAILABLE", "message": "设置服务未启用"})
		return
	}

	var req struct {
		Settings map[string]string `json:"settings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "请求参数错误"})
		return
	}
	if len(req.Settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "EMPTY_SETTINGS", "message": "settings 不能为空"})
		return
	}

	if err := h.settings.SetMany(req.Settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "保存设置失败"})
		return
	}

	// 落库成功后，把 ffmpeg/ffprobe 路径设置应用到转码运行期，保存即生效（FR-56）。
	applyFFmpegPathSettings(req.Settings)

	// 通知设置变更，让定时扫描周期等运行期配置即时生效（FR-28）。
	if h.settingsReload != nil {
		h.settingsReload()
	}

	// 写入成功后回读返回，便于前端直接刷新状态。
	all, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询设置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": all})
}

// applyFFmpegPathSettings 把本次保存的 ffmpeg/ffprobe 路径设置应用到运行期（FR-56）。
// 仅当对应键出现且非空时覆盖；ffprobe 同步给 library，与 main.go 启动注入保持一致。
// 空串不覆盖（保留自动发现/捆绑版结果），由 transcoder.Set* 自身的空值守卫保证。
func applyFFmpegPathSettings(values map[string]string) {
	if p, ok := values[settings.KeyFFmpegPath]; ok && p != "" {
		transcoder.SetFFmpegPath(p)
	}
	if p, ok := values[settings.KeyFFprobePath]; ok && p != "" {
		transcoder.SetFFprobePath(p)
		library.SetFFprobePath(p)
	}
}
