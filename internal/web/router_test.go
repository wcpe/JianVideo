package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/config"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/player"
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

	r := NewRouter(cfg, gormDB, hlsMgr, nil, nil)

	// FR-109 起 NewRouter 不再自动建号；登录类用例显式播种 admin/admin。
	sqlDB, _ := gormDB.DB()
	if err := auth.NewService(sqlDB, cfg.JWTSecret).CreateDefaultUser(); err != nil {
		t.Fatalf("播种默认用户失败: %v", err)
	}
	return r
}

// setupFreshRouter 构建一个「尚无任何用户」的路由（独立临时文件库，不播种），
// 用于首次初始化（FR-109）相关用例。
func setupFreshRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gormDB.AutoMigrate(
		&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{},
		&models.User{}, &models.PlaybackSession{},
	); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	cfg := &config.Config{ServerPort: 8080, JWTSecret: "test-secret", JWTExpiresIn: 72 * time.Hour, DBPath: dbPath}
	// 关闭 DB，避免 Windows 下临时文件被占用致 TempDir 清理失败
	t.Cleanup(func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return NewRouter(cfg, gormDB, player.NewHLSManager(t.TempDir()), nil, nil)
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

func TestSetupStatus_NeedsSetupWhenNoUser(t *testing.T) {
	r := setupFreshRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/auth/setup-status", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 得到 %d", w.Code)
	}
	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp["needs_setup"] {
		t.Error("无用户时 needs_setup 应为 true")
	}
}

func TestSetup_CreatesUserAndAutoLogin(t *testing.T) {
	r := setupFreshRouter(t)

	// 首次初始化：设置账号密码
	body := `{"username":"alice","password":"secret123"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 得到 %d, body: %s", w.Code, w.Body.String())
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") {
		t.Error("初始化应自动登录并下发 HttpOnly cookie")
	}

	// 自动登录：带 cookie 可访问受保护的 /api/me
	meW := httptest.NewRecorder()
	meReq, _ := http.NewRequest("GET", "/api/me", nil)
	meReq.Header.Set("Cookie", cookie)
	r.ServeHTTP(meW, meReq)
	if meW.Code != http.StatusOK {
		t.Errorf("初始化后应已登录, /api/me 得到 %d", meW.Code)
	}

	// 初始化后 setup-status 变 false
	sW := httptest.NewRecorder()
	sReq, _ := http.NewRequest("GET", "/api/auth/setup-status", nil)
	r.ServeHTTP(sW, sReq)
	var resp map[string]bool
	json.Unmarshal(sW.Body.Bytes(), &resp)
	if resp["needs_setup"] {
		t.Error("初始化后 needs_setup 应为 false")
	}
}

func TestSetup_RejectsWhenInitialized(t *testing.T) {
	r := setupTestRouter(t) // 已播种 admin

	body := `{"username":"mallory","password":"evil"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("已初始化应返回 409, 得到 %d, body: %s", w.Code, w.Body.String())
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
