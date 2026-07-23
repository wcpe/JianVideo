package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
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
		&models.Space{},
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
	if err := gormDB.FirstOrCreate(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1}).Error; err != nil {
		t.Fatalf("播种默认 Space 失败: %v", err)
	}
	return r
}

// TestHealthEndpointPublic 验证 /health 无需鉴权即可探活（FR2-072）。
func TestHealthEndpointPublic(t *testing.T) {
	router := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/health 期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 /health 失败: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("/health status 期望 ok, 实际 %q", body.Status)
	}
}

// TestNewRouter_DefaultHandlerExposesStorageSettings 验证默认装配也注入数据库路径。
func TestNewRouter_DefaultHandlerExposesStorageSettings(t *testing.T) {
	router := setupTestRouter(t)
	token, err := auth.GenerateToken("admin", "test-secret", time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/storage", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("读取存储设置期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Space        models.Space `json:"space"`
		DatabasePath string       `json:"database_path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析存储设置失败: %v", err)
	}
	if body.Space.ID != models.DefaultSpaceID || body.DatabasePath != ":memory:" {
		t.Fatalf("默认路由未完整注入 Space 或数据库路径: %+v", body)
	}
}

// TestRegisterStreamRoute_RejectsMediaOutsideRequestedSpace 验证流式播放按 Space 查询媒体。
func TestRegisterStreamRoute_RejectsMediaOutsideRequestedSpace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "stream-space.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gormDB.AutoMigrate(&models.User{}, &models.Space{}, &models.MediaFile{}); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	users := []models.User{
		{ID: 1, Username: "owner", PasswordHash: "x"},
		{ID: 2, Username: "other", PasswordHash: "x"},
	}
	spaces := []models.Space{
		{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1},
		{ID: "space-other", Name: "其他 Space", OwnerUserID: 2},
	}
	if err := gormDB.Create(&users).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	if err := gormDB.Create(&spaces).Error; err != nil {
		t.Fatalf("创建测试 Space 失败: %v", err)
	}
	mediaPath := filepath.Join(t.TempDir(), "default.mp4")
	if err := os.WriteFile(mediaPath, []byte("default-space-media"), 0o600); err != nil {
		t.Fatalf("创建测试媒体失败: %v", err)
	}
	media := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: mediaPath, FileName: "default.mp4", FileSize: 19}
	if err := gormDB.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}

	sqlDB, _ := gormDB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	secret := "test-secret"
	playbackSvc := playback.NewService()
	t.Cleanup(playbackSvc.Stop)
	router := gin.New()
	authSvc := auth.NewService(sqlDB, secret)
	router.Use(auth.APIGuard(secret, authSvc), auth.SpaceOwnerGuard(authSvc))
	registerStreamRoute(router, library.NewService(gormDB), playbackSvc)

	request := func(username, spaceID string) *httptest.ResponseRecorder {
		token, tokenErr := auth.GenerateToken(username, secret, time.Hour)
		if tokenErr != nil {
			t.Fatalf("生成令牌失败: %v", tokenErr)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/play/"+strconv.FormatInt(media.ID, 10)+"/stream", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
		req.Header.Set("X-JianVideo-Space-Id", spaceID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	denied := request("other", "space-other")
	if denied.Code != http.StatusNotFound {
		t.Fatalf("其他 Space owner 不得读取默认 Space 媒体, 期望 404, 实际 %d", denied.Code)
	}
	allowed := request("owner", models.DefaultSpaceID)
	if allowed.Code != http.StatusOK || allowed.Body.String() != "default-space-media" {
		t.Fatalf("默认 Space owner 应可读取自身媒体, code=%d body=%q", allowed.Code, allowed.Body.String())
	}
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
			_ = sqlDB.Close()
		}
	})
	return NewRouter(cfg, gormDB, player.NewHLSManager(t.TempDir()), nil, nil)
}

// setupSeededRouter 构建一个「已播种 admin/admin」的独立临时文件库路由。
// 用于会改动用户数据（如改密）的用例，避免污染共享内存库里的 admin。
func setupSeededRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "seeded.db")
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
	t.Cleanup(func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	r := NewRouter(cfg, gormDB, player.NewHLSManager(t.TempDir()), nil, nil)
	sqlDB, _ := gormDB.DB()
	if err := auth.NewService(sqlDB, cfg.JWTSecret).CreateDefaultUser(); err != nil {
		t.Fatalf("播种默认用户失败: %v", err)
	}
	return r
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
		"web/dist/index.html":           &fstest.MapFile{Data: []byte("<!doctype html><title>JianVideo</title>")},
		"web/dist/sw.js":                &fstest.MapFile{Data: []byte("self.addEventListener('install',()=>{})")},
		"web/dist/manifest.webmanifest": &fstest.MapFile{Data: []byte(`{"name":"JianVideo"}`)},
		"web/dist/workbox-abc123.js":    &fstest.MapFile{Data: []byte("// workbox")},
		"web/dist/assets/index-xyz.js":  &fstest.MapFile{Data: []byte("// app")},
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
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
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
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
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
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
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
	if err := json.Unmarshal(sW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
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

func TestChangePassword_RequiresAuth(t *testing.T) {
	r := setupSeededRouter(t)

	body := `{"old_password":"admin","new_password":"new-secret"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/me/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("未认证改密应 401, 得到 %d", w.Code)
	}
}

// loginCookie 登录 admin/admin 并返回 Set-Cookie，供受保护端点测试复用。
func loginCookie(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("登录失败: %d %s", w.Code, w.Body.String())
	}
	return w.Header().Get("Set-Cookie")
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	r := setupSeededRouter(t)
	cookie := loginCookie(t, r)

	body := `{"old_password":"wrong","new_password":"new-secret"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/me/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("当前密码错误应 401, 得到 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestChangePassword_Success(t *testing.T) {
	r := setupSeededRouter(t)
	cookie := loginCookie(t, r)

	body := `{"old_password":"admin","new_password":"new-secret"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/me/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("改密应 204, 得到 %d, body: %s", w.Code, w.Body.String())
	}

	// 旧密码登录失败
	oldW := httptest.NewRecorder()
	oldReq, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"admin"}`))
	oldReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(oldW, oldReq)
	if oldW.Code != http.StatusUnauthorized {
		t.Errorf("改密后旧密码登录应 401, 得到 %d", oldW.Code)
	}

	// 新密码登录成功
	newW := httptest.NewRecorder()
	newReq, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"new-secret"}`))
	newReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(newW, newReq)
	if newW.Code != http.StatusOK {
		t.Errorf("改密后新密码登录应 200, 得到 %d", newW.Code)
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


// TestLogin_LocksAfterFailures 连续错误密码达阈值后 429（FR2-062）。
func TestLogin_LocksAfterFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, err := gorm.Open(sqlite.Open("file:login_lock?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := gormDB.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gormDB.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, status TEXT DEFAULT 'active', created_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(sqlDB, "sec")
	if err := svc.CreateDefaultUser(); err != nil {
		t.Fatal(err)
	}
	limiter := auth.NewLoginLimiter()
	limiter.MaxFailures = 3
	limiter.Window = time.Minute
	limiter.LockDuration = time.Minute
	cfg := &config.Config{JWTSecret: "sec", JWTExpiresIn: time.Hour}
	r := gin.New()
	r.POST("/api/auth/login", handleLogin(svc, cfg, limiter, nil))

	post := func(pass string) *httptest.ResponseRecorder {
		body := `{"username":"admin","password":"` + pass + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.10:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for i := 0; i < 2; i++ {
		w := post("wrong")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次错误期望 401, 得到 %d %s", i+1, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "不存在") || strings.Contains(w.Body.String(), "已禁用") {
			t.Fatalf("失败响应不应暴露用户是否存在: %s", w.Body.String())
		}
	}
	w := post("wrong")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次错误期望 429, 得到 %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "LOGIN_LOCKED") {
		t.Fatalf("429 应含 LOGIN_LOCKED: %s", w.Body.String())
	}
	// 锁定期间即使正确密码也 429
	w = post("admin")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("锁定期间期望 429, 得到 %d", w.Code)
	}
}
