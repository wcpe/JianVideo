package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/netproxy"
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

	// 落库成功后，把 magick 路径设置应用到 HEIC/RAW 转换运行期，保存即生效（FR-63）。
	applyMagickPathSettings(req.Settings)

	// 落库成功后，把网络代理设置应用到后端出站 HTTP 运行期，保存即生效（FR-80）。
	applyNetworkProxySettings(req.Settings)

	// 落库成功后，把调试日志开关应用到 GORM 日志运行期，保存即生效（FR-110）。
	h.applyDebugLogSetting(req.Settings)

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

// applyMagickPathSettings 把本次保存的 magick 路径设置应用到运行期（FR-63）。
// 仿 applyFFmpegPathSettings：仅当 magick_path 出现且非空时覆盖全局路径，与 main.go 启动注入保持一致。
// 空串不覆盖（保留自动发现/捆绑版结果），由 library.SetMagickPath 自身的空值守卫保证。
func applyMagickPathSettings(values map[string]string) {
	if p, ok := values[settings.KeyMagickPath]; ok && p != "" {
		library.SetMagickPath(p)
	}
}

// applyDebugLogSetting 把本次保存的调试日志开关应用到 GORM 日志运行期（FR-110）。
// 仅当 debug_log 键出现时处理：按 "1"/"true"=开、其余=关，调用注入的切级别回调即时生效。
// 未注入回调时仅落库（启动时仍会读取该设置决定初始级别）。
func (h *Handler) applyDebugLogSetting(values map[string]string) {
	if v, ok := values[settings.KeyDebugLog]; ok && h.debugLogApply != nil {
		h.debugLogApply(settings.ParseDebugLog(v))
	}
}

// applyNetworkProxySettings 把本次保存的网络代理设置应用到后端出站 HTTP 运行期（FR-80）。
// 仅当 network_proxy 键出现时处理：空串清空走直连、非空设置代理；
// 非法 URL 仅记 WARN、不阻断整体保存（落库已成功），与 SetProxy 的「非法不覆盖」语义配合，
// 保留既有运行期代理不被坏值打乱。SetProxy 自身做 URL 校验与原子更新（并发安全）。
func applyNetworkProxySettings(values map[string]string) {
	if p, ok := values[settings.KeyNetworkProxy]; ok {
		if err := netproxy.SetProxy(p); err != nil {
			log.Printf("[WARN] 网络代理设置无效，保留既有出站代理: %v", err)
		}
	}
}
