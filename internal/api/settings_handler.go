package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

	// 写入成功后回读返回，便于前端直接刷新状态。
	all, err := h.settings.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "查询设置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": all})
}
