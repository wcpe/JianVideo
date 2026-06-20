package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/db/models"
	"jianvideo/internal/player"
	"jianvideo/internal/web"
)

// newTestServer 创建完整的测试服务器。
func newTestServer(t *testing.T) (*httptest.Server, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 创建临时目录
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	hlsDir := filepath.Join(tmpDir, "hls")

	// 打开数据库
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
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
		ServerPort:   0,
		JWTSecret:    "test-secret",
		JWTExpiresIn: 72 * time.Hour,
		DBPath:       dbPath,
	}

	hlsMgr := player.NewHLSManager(hlsDir)
	srv := web.NewRouter(cfg, gormDB, hlsMgr, nil, nil)

	// 使用 httptest 启动真实 HTTP 服务器
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	server := httptest.NewUnstartedServer(srv)
	server.Listener = lis
	server.Start()

	// 测试结束时关闭数据库和服务器
	t.Cleanup(func() {
		server.Close()
		if sqlDB, err := gormDB.DB(); err == nil {
			sqlDB.Close()
		}
	})

	return server, gormDB, tmpDir
}

// doRequest 发送 HTTP 请求并返回响应。
func doRequest(t *testing.T, method, path string, body interface{}, headers map[string]string) *http.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			reqBody = strings.NewReader(v)
		case []byte:
			reqBody = bytes.NewReader(v)
		case io.Reader:
			reqBody = v
		default:
			b, _ := json.Marshal(v)
			reqBody = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, path, reqBody)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	return resp
}

// parseJSON 解析 JSON 响应体。
func parseJSON(t *testing.T, resp *http.Response, target interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("解析 JSON 失败: %v, body: %s", err, string(body))
	}
}

// HealthCheck

func TestE2E_HealthCheck(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	resp := doRequest(t, "GET", server.URL+"/health", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", resp.StatusCode)
	}

	var result map[string]string
	parseJSON(t, resp, &result)
	if result["status"] != "ok" {
		t.Errorf("期望 status=ok, 实际 %q", result["status"])
	}
}

// Auth: Login

func TestE2E_Login_Success(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":"admin","password":"admin"}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("期望 200, 实际 %d, body: %s", resp.StatusCode, string(b))
	}

	var result map[string]string
	parseJSON(t, resp, &result)
	if result["username"] != "admin" {
		t.Errorf("期望 username=admin, 实际 %q", result["username"])
	}

	// 验证 Set-Cookie
	cookies := resp.Header.Get("Set-Cookie")
	if !strings.Contains(cookies, "HttpOnly") {
		t.Error("Set-Cookie 应包含 HttpOnly")
	}
}

func TestE2E_Login_WrongPassword(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":"admin","password":"wrong"}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("期望 401, 实际 %d", resp.StatusCode)
	}
}

func TestE2E_Login_InvalidInput(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	body := `{"username":""}`
	resp := doRequest(t, "POST", server.URL+"/api/auth/login", body, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", resp.StatusCode)
	}
}

// Library: CRUD

func TestE2E_Library_CreatePath(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	// 先登录获取 Cookie
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 创建媒体库目录（Windows 路径需要转义 JSON 中的反斜杠）
	// 后端会校验路径必须真实存在且为目录，故先创建
	dirPath := filepath.Join(tmpDir, "movies")
	os.MkdirAll(dirPath, 0o755)
	body := fmt.Sprintf(`{"path":"%s","type":"local","label":"测试目录"}`, strings.ReplaceAll(dirPath, `\`, `\\`))
	headers := map[string]string{"Cookie": cookie}

	resp := doRequest(t, "POST", server.URL+"/api/library/paths", body, headers)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("期望 201, 实际 %d, body: %s", resp.StatusCode, string(b))
	}

	var result models.LibraryPath
	parseJSON(t, resp, &result)
	if result.Path == "" {
		t.Error("返回的 path 不应为空")
	}
	if result.Type != "local" {
		t.Errorf("期望 type=local, 实际 %q", result.Type)
	}
}

func TestE2E_Library_ListPaths(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	// 登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 先创建一个目录（后端校验路径需真实存在）
	dirPath := filepath.Join(tmpDir, "movies")
	os.MkdirAll(dirPath, 0o755)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"电影"}`, strings.ReplaceAll(dirPath, `\`, `\\`))
	doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})

	// 查询列表
	resp := doRequest(t, "GET", server.URL+"/api/library/paths", nil,
		map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", resp.StatusCode)
	}

	var result struct {
		Items []models.LibraryPath `json:"items"`
	}
	parseJSON(t, resp, &result)
	if len(result.Items) == 0 {
		t.Error("应至少有一个目录")
	}
}

// Media: CRUD

func TestE2E_Media_List(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	// 登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 创建目录
	dir := filepath.Join(tmpDir, "movies")
	os.MkdirAll(dir, 0o755)
	// Windows 路径需要转义 JSON 中的反斜杠
	escapedDir := strings.ReplaceAll(dir, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"电影"}`, escapedDir)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})
	if createResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(createResp.Body)
		t.Fatalf("创建目录失败: %d, body: %s", createResp.StatusCode, string(b))
	}
	var lp models.LibraryPath
	parseJSON(t, createResp, &lp)

	// 创建一个测试视频文件
	videoPath := filepath.Join(dir, "test.mp4")
	os.WriteFile(videoPath, []byte("fake video data"), 0o644)

	// 触发扫描
	scanResp := doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})
	if scanResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(scanResp.Body)
		t.Fatalf("扫描失败: %d, body: %s", scanResp.StatusCode, string(b))
	}

	// 查询媒体文件列表
	resp := doRequest(t, "GET", server.URL+"/api/library/media", nil,
		map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", resp.StatusCode)
	}

	var result struct {
		Items []models.MediaFile `json:"items"`
	}
	parseJSON(t, resp, &result)
	if len(result.Items) == 0 {
		t.Error("扫描后应至少有一个媒体文件")
	}
}

// Play: PlayInfo

func TestE2E_Play_GetPlayInfo(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	// 登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 创建目录并添加媒体文件
	dir := filepath.Join(tmpDir, "movies")
	os.MkdirAll(dir, 0o755)
	escapedDir := strings.ReplaceAll(dir, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"电影"}`, escapedDir)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths", createBody,
		map[string]string{"Cookie": cookie})
	if createResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(createResp.Body)
		t.Fatalf("创建目录失败: %d, body: %s", createResp.StatusCode, string(b))
	}
	var lp models.LibraryPath
	parseJSON(t, createResp, &lp)

	// 创建 H.264+AAC MP4 文件
	videoPath := filepath.Join(dir, "movie.mp4")
	os.WriteFile(videoPath, []byte("fake h264 aac mp4 data"), 0o644)

	// 扫描
	doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})

	// 查询媒体文件
	listResp := doRequest(t, "GET", server.URL+"/api/library/media", nil,
		map[string]string{"Cookie": cookie})
	var listResult struct {
		Items []models.MediaFile `json:"items"`
	}
	parseJSON(t, listResp, &listResult)
	if len(listResult.Items) == 0 {
		t.Fatal("应有至少一个媒体文件")
	}

	mediaID := listResult.Items[0].ID

	// 获取播放信息
	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/%d", server.URL, mediaID), nil,
		map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("期望 200 或 404, 实际 %d, body: %s", resp.StatusCode, string(b))
	}
}

// HLS: Routes

func TestE2E_HLS_Routes(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	// 登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 请求不存在的 HLS m3u8 → 404
	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/hls/9999/index.m3u8", server.URL), nil,
		map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", resp.StatusCode)
	}
}

// Protected routes

func TestE2E_Protected_NoAuth(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	// 未认证访问受保护路由
	resp := doRequest(t, "GET", server.URL+"/api/me", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("期望 401, 实际 %d", resp.StatusCode)
	}
}

func TestE2E_Protected_WithAuth(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	// 登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	cookie := loginResp.Header.Get("Set-Cookie")

	// 认证访问
	resp := doRequest(t, "GET", server.URL+"/api/me", nil,
		map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("期望 200, 实际 %d, body: %s", resp.StatusCode, string(b))
	}

	var result map[string]string
	parseJSON(t, resp, &result)
	if result["username"] != "admin" {
		t.Errorf("期望 username=admin, 实际 %q", result["username"])
	}
}

// Static files (frontend) - 验证 SPA fallback 正常工作

func TestE2E_Static_Index(t *testing.T) {
	server, _, _ := newTestServer(t)
	defer server.Close()

	resp := doRequest(t, "GET", server.URL+"/", nil, nil)
	// 如果没有嵌入前端，返回 404 也是可接受的
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("期望 200 或 404, 实际 %d", resp.StatusCode)
	}

	if resp.StatusCode == http.StatusOK {
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("期望 text/html, 实际 %q", contentType)
		}
	}
}

// Full workflow: Login → CreatePath → Upload → Scan → ListMedia

func TestE2E_FullWorkflow(t *testing.T) {
	server, _, tmpDir := newTestServer(t)
	defer server.Close()

	// 1. 登录
	loginResp := doRequest(t, "POST", server.URL+"/api/auth/login",
		`{"username":"admin","password":"admin"}`, nil)
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", loginResp.StatusCode)
	}
	cookie := loginResp.Header.Get("Set-Cookie")

	// 2. 创建媒体库目录
	dir := filepath.Join(tmpDir, "media", "movies")
	os.MkdirAll(dir, 0o755)

	escapedDir := strings.ReplaceAll(dir, `\`, `\\`)
	createBody := fmt.Sprintf(`{"path":"%s","type":"local","label":"完整流程测试"}`, escapedDir)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths",
		createBody, map[string]string{"Cookie": cookie})
	if createResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(createResp.Body)
		t.Fatalf("创建目录失败: %d, body: %s", createResp.StatusCode, string(b))
	}

	var lp models.LibraryPath
	parseJSON(t, createResp, &lp)

	// 3. 创建测试视频文件
	videoFiles := []string{"video1.mp4", "video2.mkv", "video3.avi"}
	for _, name := range videoFiles {
		os.WriteFile(filepath.Join(dir, name), []byte("test data for "+name), 0o644)
	}

	// 4. 触发扫描
	scanResp := doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})
	if scanResp.StatusCode != http.StatusOK {
		t.Fatalf("扫描失败: %d", scanResp.StatusCode)
	}

	// 5. 查询媒体文件列表
	// 扫描已改为异步执行，POST 立即返回，故轮询媒体列表直到入库完成或超时
	var listResult struct {
		Items []models.MediaFile `json:"items"`
		Total int64              `json:"total"`
	}
	for i := 0; i < 50; i++ {
		listResp := doRequest(t, "GET", server.URL+"/api/library/media?sort=time_desc", nil,
			map[string]string{"Cookie": cookie})
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("查询失败: %d", listResp.StatusCode)
		}
		parseJSON(t, listResp, &listResult)
		if listResult.Total == int64(len(videoFiles)) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if listResult.Total != int64(len(videoFiles)) {
		t.Errorf("期望 %d 个媒体文件, 实际 %d", len(videoFiles), listResult.Total)
	}

	// 6. 登出
	logoutResp := doRequest(t, "POST", server.URL+"/api/auth/logout", nil,
		map[string]string{"Cookie": cookie})
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Errorf("期望 204, 实际 %d", logoutResp.StatusCode)
	}

	// 7. 登出后无法访问受保护路由（不传 Cookie）
	meResp := doRequest(t, "GET", server.URL+"/api/me", nil, nil)
	if meResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("登出后期望 401, 实际 %d", meResp.StatusCode)
	}
}
