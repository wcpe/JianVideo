package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	cookieName     = "auth_token"
	spaceHeader    = "X-JianVideo-Space-Id"
	defaultSpaceID = "space-default"
)

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
// 可选传入 *Service：JWT 有效时再查 users.status，禁用用户拒绝（FR2-010）。
func APIGuard(jwtSecret string, svc ...*Service) gin.HandlerFunc {
	var authSvc *Service
	if len(svc) > 0 {
		authSvc = svc[0]
	}
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
		// 豁免公开分享端点（FR-43）：免登只读访问，路由内经 shareAuth 自校验 token。
		// 注意带尾斜杠 "/api/share/"，不会误伤受保护的管理端点 "/api/shares"。
		if strings.HasPrefix(path, "/api/share/") {
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

		if authSvc != nil {
			user, err := authSvc.FindUserByUsername(claims.Username)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":    "INTERNAL",
					"message": "校验用户状态失败",
				})
				return
			}
			if user == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"code":    "UNAUTHORIZED",
					"message": "用户不存在",
				})
				return
			}
			if user.Status == models.UserStatusDisabled {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"code":    "USER_DISABLED",
					"message": "用户已禁用",
				})
				return
			}
			// 供 handler 使用，避免非 Space 路由拿不到 user_id（如用户管理）。
			c.Set("user_id", user.ID)
		}

		c.Set("username", claims.Username)
		c.Next()
	}
}

// SpaceOwnerGuard 对 Space 资源执行成员校验（FR2-010：至少 viewer 可读；写操作另由 RequireSpaceRole 收紧）。
// 函数名保留兼容既有装配；语义为「Space 成员守卫」。
func SpaceOwnerGuard(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requiresSpaceOwner(c) || authorizeSpaceMember(c, svc) {
			c.Next()
		}
	}
}

// RequireSpaceRole 在已通过成员守卫的请求上要求最低角色（用于写操作）。
// 若 context 尚无 space_role，则现场解析。
func RequireSpaceRole(svc *Service, minRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := c.Get("space_role")
		roleStr, _ := role.(string)
		if !ok || roleStr == "" {
			spaceID, ok := requestedSpaceID(c)
			if !ok {
				return
			}
			username, ok := currentUsername(c)
			if !ok {
				return
			}
			exists, r, err := svc.SpaceMemberAccess(username, spaceID)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "校验 Space 权限失败"})
				return
			}
			if !exists {
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"code": "SPACE_NOT_FOUND", "message": "Space 不存在"})
				return
			}
			roleStr = r
			c.Set("space_id", spaceID)
			c.Set("space_role", roleStr)
		}
		if !roleAtLeast(roleStr, minRole) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "SPACE_FORBIDDEN",
				"message": "当前角色无权执行此操作",
			})
			return
		}
		c.Next()
	}
}

func roleAtLeast(actual, min string) bool {
	rank := map[string]int{"viewer": 1, "editor": 2, "owner": 3}
	return rank[actual] >= rank[min]
}

func authorizeSpaceMember(c *gin.Context, svc *Service) bool {
	spaceID, ok := requestedSpaceID(c)
	if !ok {
		return false
	}
	username, ok := currentUsername(c)
	if !ok {
		return false
	}
	exists, role, err := svc.SpaceMemberAccess(username, spaceID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": "校验 Space 权限失败"})
		return false
	}
	if !exists {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"code": "SPACE_NOT_FOUND", "message": "Space 不存在"})
		return false
	}
	if role == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "SPACE_FORBIDDEN", "message": "不是该 Space 的成员"})
		return false
	}
	// 写操作最低 editor（GET/HEAD/OPTIONS 仅需成员）。
	if !isReadMethod(c.Request.Method) && !roleAtLeast(role, "editor") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "SPACE_FORBIDDEN", "message": "当前角色无权执行写操作"})
		return false
	}
	c.Set("space_id", spaceID)
	c.Set("space_role", role)
	if user, err := svc.FindUserByUsername(username); err == nil && user != nil {
		c.Set("user_id", user.ID)
	}
	return true
}

func isReadMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func requestedSpaceID(c *gin.Context) (string, bool) {
	spaceID := strings.TrimSpace(c.GetHeader(spaceHeader))
	if c.Request.URL.Path == "/api/audit/events" && c.Query("scope") != "system" {
		if querySpaceID := strings.TrimSpace(c.Query("space_id")); querySpaceID != "" {
			spaceID = querySpaceID
		}
	}
	if spaceID == "" {
		spaceID = defaultSpaceID
	}
	if validSpaceID(spaceID) {
		return spaceID, true
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": "INVALID_SPACE", "message": "Space ID 不合法"})
	return "", false
}

func currentUsername(c *gin.Context) (string, bool) {
	value, ok := c.Get("username")
	username, valid := value.(string)
	if ok && valid && strings.TrimSpace(username) != "" {
		return username, true
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "请先登录"})
	return "", false
}

func requiresSpaceOwner(c *gin.Context) bool {
	path := c.Request.URL.Path
	for _, prefix := range []string{"/api/library", "/api/albums", "/api/shares", "/api/media-types", "/api/play", "/api/transcode/tasks", "/api/storage/cache", "/api/settings/storage"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	if strings.HasPrefix(path, "/api/tasks") || strings.HasPrefix(path, "/api/audit/events") {
		return c.Query("scope") != "system"
	}
	return false
}

func validSpaceID(spaceID string) bool {
	if spaceID == "" || len(spaceID) > 128 {
		return false
	}
	for _, ch := range spaceID {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
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
