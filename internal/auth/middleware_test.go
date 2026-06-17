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
