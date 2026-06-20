package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/db/models"
	"jianvideo/internal/player"
)

func setupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gormDB.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.User{},
		&models.PlaybackSession{},
	); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}

	cfg := &config.Config{
		ServerPort:   8080,
		JWTSecret:    "test-secret",
		JWTExpiresIn: 72 * time.Hour,
		DBPath:       ":memory:",
	}

	hlsMgr := player.NewHLSManager(t.TempDir())

	return NewRouter(cfg, gormDB, hlsMgr, nil, nil)
}

func TestLogin_Success(t *testing.T) {
	r := setupTestRouter(t)

	body := `{"username":"admin","password":"admin"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 得到 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["username"] != "admin" {
		t.Errorf("期望 username=admin, 得到 %q", resp["username"])
	}

	// 验证 Set-Cookie 头包含 HttpOnly
	cookies := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookies, "HttpOnly") {
		t.Error("Set-Cookie 应包含 HttpOnly")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r := setupTestRouter(t)

	body := `{"username":"admin","password":"wrong"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 得到 %d", w.Code)
	}
}

func TestLogin_InvalidInput(t *testing.T) {
	r := setupTestRouter(t)

	body := `{"username":""}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("期望 400, 得到 %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/logout", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("期望 204, 得到 %d", w.Code)
	}
}

func TestProtectedRoute_Unauthorized(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("期望 401, 得到 %d", w.Code)
	}
}

func TestProtectedRoute_Authorized(t *testing.T) {
	r := setupTestRouter(t)

	// 先登录获取 Cookie
	loginBody := `{"username":"admin","password":"admin"}`
	loginW := httptest.NewRecorder()
	loginReq, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(loginW, loginReq)

	// 提取 Cookie
	cookie := loginW.Header().Get("Set-Cookie")

	// 访问受保护路由
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 得到 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["username"] != "admin" {
		t.Errorf("期望 username=admin, 得到 %q", resp["username"])
	}
}

func TestHealthCheck(t *testing.T) {
	r := setupTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望 200, 得到 %d", w.Code)
	}
}
