package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const cookieName = "auth_token"

// Middleware JWT 认证中间件
func Middleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "请先登录",
			})
			return
		}

		claims, err := ParseToken(token, jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "令牌无效或已过期",
			})
			return
		}

		c.Set("username", claims.Username)
		c.Next()
	}
}

// APIGuard 全局鉴权中间件：对路径前缀为 /api/ 但不属于 /api/auth/ 的请求强制校验 JWT。
// 其余路径（/health、/assets、SPA 回退等）一律放行，保证未登录也能加载前端壳与登录。
// 媒体请求（img/video/hls.js）靠同源 Cookie 鉴权，故 extractToken 同时支持 Cookie 与 Bearer。
func APIGuard(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// 仅保护 /api/* 路由；非 /api 路径（静态资源、SPA 回退、健康检查）直接放行
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}
		// 豁免认证端点：登录 / 登出本身不能要求登录
		if strings.HasPrefix(path, "/api/auth/") {
			c.Next()
			return
		}

		token := extractToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "请先登录",
			})
			return
		}

		claims, err := ParseToken(token, jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "令牌无效或已过期",
			})
			return
		}

		c.Set("username", claims.Username)
		c.Next()
	}
}

// extractToken 从请求中提取 JWT：优先 Cookie，其次 Authorization: Bearer。
func extractToken(c *gin.Context) string {
	token, err := c.Cookie(cookieName)
	if err == nil && token != "" {
		return token
	}
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return ""
}

// SetAuthCookie 设置认证 Cookie
func SetAuthCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		cookieName,
		token,
		72*3600, // 72 小时（秒）
		"/",
		"",
		true, // Secure
		true, // HttpOnly
	)
}

// ClearAuthCookie 清除认证 Cookie
func ClearAuthCookie(c *gin.Context) {
	c.SetCookie(cookieName, "", -1, "/", "", true, true)
}
