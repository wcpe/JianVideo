package openapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetHealth 公开探活（FR2-071 / FR2-072）：返回契约 HealthStatus，无需鉴权。
// 由 web.NewRouter 单挂 /health，不调用 RegisterHandlers，避免与手写 auth 路由重复注册。
func GetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthStatus{Status: "ok"})
}
