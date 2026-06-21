package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
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
