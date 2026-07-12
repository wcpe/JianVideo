package auth

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authMW := Middleware(secret)

	r.GET("/protected", authMW, func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	return r
}

func TestMiddleware_NoToken(t *testing.T) {
	r := setupTestRouter("test-secret")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 得到 %d", w.Code)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	secret := "test-secret"
	r := setupTestRouter(secret)

	token, err := GenerateToken("admin", secret, 72*time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 得到 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	r := setupTestRouter("test-secret")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "invalid-token"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 得到 %d", w.Code)
	}
}

func TestMiddleware_BearerToken(t *testing.T) {
	secret := "test-secret"
	r := setupTestRouter(secret)

	token, err := GenerateToken("admin", secret, 72*time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 得到 %d, body: %s", w.Code, w.Body.String())
	}
}

// setupGuardRouter 构造一个挂了全局 APIGuard 的引擎，模拟真实路由结构：
// 含一个受保护的 /api/* 端点、豁免的 /api/auth/login、以及非 /api 的 /health。
func setupGuardRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIGuard(secret))
	r.GET("/api/library/media", func(c *gin.Context) { c.String(http.StatusOK, "media") })
	r.POST("/api/auth/login", func(c *gin.Context) { c.String(http.StatusOK, "login") })
	r.GET("/api/share/:token", func(c *gin.Context) { c.String(http.StatusOK, "share") })
	r.GET("/api/shares", func(c *gin.Context) { c.String(http.StatusOK, "shares") })
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

// TestAPIGuard_ExemptsPublicShareButGuardsManagement 公开分享端点豁免、管理端点仍受保护（FR-43 安全边界）。
func TestAPIGuard_ExemptsPublicShareButGuardsManagement(t *testing.T) {
	r := setupGuardRouter("test-secret")

	// 公开分享端点 /api/share/:token 免登放行
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/share/sometoken", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/api/share/:token 应豁免鉴权返回 200, 得到 %d", w.Code)
	}

	// 管理端点 /api/shares（复数）不被 "/api/share/" 前缀误伤，无凭据应 401
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/shares", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("/api/shares 管理端点无凭据应 401, 得到 %d", w2.Code)
	}
}

func TestAPIGuard_ProtectsAPIWithoutToken(t *testing.T) {
	r := setupGuardRouter("test-secret")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/library/media", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("受保护 /api 无凭据应 401, 得到 %d", w.Code)
	}
}

func TestAPIGuard_AllowsAPIWithValidToken(t *testing.T) {
	secret := "test-secret"
	r := setupGuardRouter(secret)

	token, err := GenerateToken("admin", secret, 72*time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/library/media", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("受保护 /api 带有效 Cookie 应 200, 得到 %d", w.Code)
	}
}

func TestAPIGuard_ExemptsAuthAndHealth(t *testing.T) {
	r := setupGuardRouter("test-secret")

	// /api/auth/login 豁免：无凭据也应放行
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/api/auth/login 应豁免鉴权返回 200, 得到 %d", w.Code)
	}

	// /health 非 /api 路径：无凭据也应放行
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("/health 应放行返回 200, 得到 %d", w2.Code)
	}
}

func setupSpaceOwnerGuardRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	secret := "test-secret"
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE TABLE spaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, owner_user_id INTEGER NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`INSERT INTO users(id, username, password_hash, created_at) VALUES (1, 'owner', 'x', datetime('now')), (2, 'other', 'x', datetime('now'))`,
		`INSERT INTO spaces(id, name, owner_user_id, created_at, updated_at) VALUES ('space-default', '默认 Space', 1, datetime('now'), datetime('now')), ('space-other', '其他 Space', 2, datetime('now'), datetime('now'))`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("初始化测试数据失败: %v", err)
		}
	}

	r := gin.New()
	r.Use(APIGuard(secret), SpaceOwnerGuard(NewService(db, secret)))
	for _, route := range []string{
		"/api/library/media",
		"/api/library/paths",
		"/api/play/1/stream",
		"/api/settings/storage",
		"/api/albums",
		"/api/shares",
	} {
		r.GET(route, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	}
	r.POST("/api/library/paths", func(c *gin.Context) { c.String(http.StatusCreated, "created") })
	r.POST("/api/albums", func(c *gin.Context) { c.String(http.StatusCreated, "created") })
	r.POST("/api/shares", func(c *gin.Context) { c.String(http.StatusCreated, "created") })
	r.DELETE("/api/library/media/1", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/api/system/info", func(c *gin.Context) { c.String(http.StatusOK, "system") })
	r.GET("/api/audit/events", func(c *gin.Context) { c.String(http.StatusOK, "audit") })
	return r, secret
}

func requestWithUserToken(t *testing.T, r *gin.Engine, secret, username, method, path, spaceID string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := GenerateToken(username, secret, time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	if spaceID != "" {
		req.Header.Set("X-JianVideo-Space-Id", spaceID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSpaceOwnerGuard_AllowsOwnerReadWriteAndList(t *testing.T) {
	r, secret := setupSpaceOwnerGuardRouter(t)
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/library/media", http.StatusOK},
		{http.MethodGet, "/api/library/paths", http.StatusOK},
		{http.MethodPost, "/api/library/paths", http.StatusCreated},
		{http.MethodDelete, "/api/library/media/1", http.StatusNoContent},
		{http.MethodGet, "/api/play/1/stream", http.StatusOK},
		{http.MethodGet, "/api/settings/storage", http.StatusOK},
		{http.MethodGet, "/api/albums", http.StatusOK},
		{http.MethodPost, "/api/albums", http.StatusCreated},
		{http.MethodGet, "/api/shares", http.StatusOK},
		{http.MethodPost, "/api/shares", http.StatusCreated},
	} {
		w := requestWithUserToken(t, r, secret, "owner", tc.method, tc.path, "space-default")
		if w.Code != tc.want {
			t.Fatalf("owner %s %s 期望 %d, 实际 %d, body: %s", tc.method, tc.path, tc.want, w.Code, w.Body.String())
		}
	}
}

func TestSpaceOwnerGuard_DeniesNonOwnerReadWriteAndList(t *testing.T) {
	r, secret := setupSpaceOwnerGuardRouter(t)
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/library/media"},
		{http.MethodGet, "/api/library/paths"},
		{http.MethodPost, "/api/library/paths"},
		{http.MethodDelete, "/api/library/media/1"},
		{http.MethodGet, "/api/play/1/stream"},
		{http.MethodGet, "/api/settings/storage"},
		{http.MethodGet, "/api/albums"},
		{http.MethodPost, "/api/albums"},
		{http.MethodGet, "/api/shares"},
		{http.MethodPost, "/api/shares"},
	} {
		w := requestWithUserToken(t, r, secret, "other", tc.method, tc.path, "space-default")
		if w.Code != http.StatusForbidden {
			t.Fatalf("非 owner %s %s 期望 403, 实际 %d, body: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSpaceOwnerGuard_RejectsUnauthenticatedInvalidAndMissingSpace(t *testing.T) {
	r, secret := setupSpaceOwnerGuardRouter(t)

	unauthenticated := httptest.NewRecorder()
	r.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/library/media", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("未认证请求期望 401, 实际 %d", unauthenticated.Code)
	}

	invalid := requestWithUserToken(t, r, secret, "owner", http.MethodGet, "/api/library/media", "bad space")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("非法 Space 期望 400, 实际 %d", invalid.Code)
	}

	missing := requestWithUserToken(t, r, secret, "owner", http.MethodGet, "/api/library/media", "space-missing")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("不存在 Space 期望 404, 实际 %d", missing.Code)
	}
}

func TestSpaceOwnerGuard_AuthorizesAuditQuerySpace(t *testing.T) {
	r, secret := setupSpaceOwnerGuardRouter(t)

	forbidden := requestWithUserToken(t, r, secret, "owner", http.MethodGet, "/api/audit/events?space_id=space-other", "space-default")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("默认 Space owner 查询其他 Space 审计应返回 403, 实际 %d", forbidden.Code)
	}

	allowed := requestWithUserToken(t, r, secret, "other", http.MethodGet, "/api/audit/events?space_id=space-other", "")
	if allowed.Code != http.StatusOK {
		t.Fatalf("目标 Space owner 查询自身审计应返回 200, 实际 %d", allowed.Code)
	}

	invalid := requestWithUserToken(t, r, secret, "owner", http.MethodGet, "/api/audit/events?space_id=bad%20space", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("非法审计 Space 应返回 400, 实际 %d", invalid.Code)
	}
}

func TestSpaceOwnerGuard_DoesNotBlockSystemEndpoint(t *testing.T) {
	r, secret := setupSpaceOwnerGuardRouter(t)
	for _, path := range []string{"/api/system/info", "/api/audit/events?scope=system"} {
		w := requestWithUserToken(t, r, secret, "other", http.MethodGet, path, "space-default")
		if w.Code != http.StatusOK {
			t.Fatalf("系统端点 %s 不应被 owner 守卫阻断, 实际 %d", path, w.Code)
		}
	}
}

func TestSetAndClearAuthCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	SetAuthCookie(c, "test-token")

	// 验证 Set-Cookie 头
	cookies := w.Header().Values("Set-Cookie")
	if len(cookies) == 0 {
		t.Fatal("应设置 Set-Cookie 头")
	}

	// 清除
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	ClearAuthCookie(c2)

	cookies2 := w2.Header().Values("Set-Cookie")
	if len(cookies2) == 0 {
		t.Fatal("清除时应设置 Set-Cookie 头")
	}
}
