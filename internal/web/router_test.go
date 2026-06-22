package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
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

// setupTestRouterWithFrontend 同上，但注入桩内嵌前端（含 PWA 根资源），用于静态服务测试。
func setupTestRouterWithFrontend(t *testing.T, frontendFS fs.FS) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gormDB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gormDB.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.User{}, &models.PlaybackSession{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	cfg := &config.Config{ServerPort: 8080, JWTSecret: "test-secret", JWTExpiresIn: 72 * time.Hour, DBPath: ":memory:"}
	return NewRouter(cfg, gormDB, player.NewHLSManager(t.TempDir()), frontendFS, nil)
}

// TestServesRootPWAAssets PWA 根资源（sw.js/manifest.webmanifest/workbox）按真实 MIME 服出，
// 而非被 SPA 兜底成 index.html（FR-45 修复回归）。
func TestServesRootPWAAssets(t *testing.T) {
	frontendFS := fstest.MapFS{
		"frontend/dist/index.html":           &fstest.MapFile{Data: []byte("<!doctype html><title>JianVideo</title>")},
		"frontend/dist/sw.js":                &fstest.MapFile{Data: []byte("self.addEventListener('install',()=>{})")},
		"frontend/dist/manifest.webmanifest": &fstest.MapFile{Data: []byte(`{"name":"JianVideo"}`)},
		"frontend/dist/workbox-abc123.js":    &fstest.MapFile{Data: []byte("// workbox")},
		"frontend/dist/assets/index-xyz.js":  &fstest.MapFile{Data: []byte("// app")},
	}
	r := setupTestRouterWithFrontend(t, frontendFS)

	cases := []struct {
		path       string
		wantBody   string
		ctContains string
		notHTML    bool
	}{
		{"/sw.js", "self.addEventListener('install',()=>{})", "javascript", true},
		{"/manifest.webmanifest", `{"name":"JianVideo"}`, "application/manifest+json", true},
		{"/workbox-abc123.js", "// workbox", "javascript", true},
		{"/assets/index-xyz.js", "// app", "javascript", true},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", c.path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s 期望 200, 得到 %d", c.path, w.Code)
			continue
		}
		if w.Body.String() != c.wantBody {
			t.Errorf("%s 应返回真实文件内容, 得到 %q", c.path, w.Body.String())
		}
		ct := w.Header().Get("Content-Type")
		if c.notHTML && strings.Contains(ct, "text/html") {
			t.Errorf("%s 不应是 text/html（PWA 资源被 SPA 兜底了）, 实际 %q", c.path, ct)
		}
		if !strings.Contains(ct, c.ctContains) {
			t.Errorf("%s Content-Type 期望含 %q, 实际 %q", c.path, c.ctContains, ct)
		}
	}

	// SPA 路由仍回退到 index.html
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/browse", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("SPA 路由 /browse 应回退 index.html(html), 得到 %d %q", w.Code, w.Header().Get("Content-Type"))
	}
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
