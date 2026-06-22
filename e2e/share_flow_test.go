package e2e

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/api"
	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
	"jianvideo/internal/playback"
	"jianvideo/internal/player"
	"jianvideo/internal/settings"
	"jianvideo/internal/share"
	"jianvideo/internal/web"
)

// newShareTestServer 构造一台「生产同构」测试服务器（FR-43 端到端用）：
// 注入分享服务 + 播放服务 + 完整迁移（含 Album/Share）+ 桩内嵌前端，
// 以便在真实 HTTP 层验证「免登 + APIGuard 豁免 + 范围隔离 + Range 流 + 下载」。
func newShareTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	hlsDir := filepath.Join(tmpDir, "hls")

	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	if err := gormDB.AutoMigrate(
		&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{},
		&models.User{}, &models.PlaybackSession{},
		&models.Album{}, &models.AlbumItem{}, &models.Share{},
	); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}

	cfg := &config.Config{ServerPort: 0, JWTSecret: "test-secret", JWTExpiresIn: 72 * time.Hour, DBPath: dbPath}

	libSvc := library.NewService(gormDB)
	pbSvc := playback.NewService()
	handler := api.NewHandler(libSvc).
		WithSettings(settings.NewService(gormDB)).
		WithShareService(share.NewService(gormDB))

	// 桩内嵌前端：用于验证公开查看页 /s/:token 免登返回 SPA 壳（不跳登录）
	frontendFS := fstest.MapFS{
		"frontend/dist/index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>JianVideo</title>")},
		"frontend/dist/assets/app.js": &fstest.MapFile{Data: []byte("// app")},
	}

	hlsMgr := player.NewHLSManager(hlsDir)
	srv := web.NewRouter(cfg, gormDB, hlsMgr, frontendFS, handler, pbSvc)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	server := httptest.NewUnstartedServer(srv)
	server.Listener = lis
	server.Start()

	t.Cleanup(func() {
		server.Close()
		pbSvc.Stop()
		if sqlDB, err := gormDB.DB(); err == nil {
			sqlDB.Close()
		}
	})

	return server, tmpDir
}

// shareLogin 登录并返回认证 Cookie。
func shareLogin(t *testing.T, serverURL string) string {
	t.Helper()
	resp := doRequest(t, "POST", serverURL+"/api/auth/login", `{"username":"admin","password":"admin"}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}
	return resp.Header.Get("Set-Cookie")
}

// createShare 经管理端点创建分享，返回 token。
func createShare(t *testing.T, serverURL, cookie, resourceType string, resourceID int64) string {
	t.Helper()
	body := fmt.Sprintf(`{"resource_type":"%s","resource_id":%d}`, resourceType, resourceID)
	resp := doRequest(t, "POST", serverURL+"/api/shares", body, map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("创建分享失败: %d, body: %s", resp.StatusCode, string(b))
	}
	var sh models.Share
	parseJSON(t, resp, &sh)
	if sh.Token == "" {
		t.Fatal("分享 token 不应为空")
	}
	return sh.Token
}

// TestE2E_Share_PublicAccessFlow 端到端验证 FR-43：authed 建库扫描建分享 → 免登（无 Cookie）访问。
func TestE2E_Share_PublicAccessFlow(t *testing.T) {
	server, tmpDir := newShareTestServer(t)
	cookie := shareLogin(t, server.URL)

	// 建库 + 放一个视频与一张图片
	dir := filepath.Join(tmpDir, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	videoBytes := []byte("VIDEO-ORIGINAL-BYTES-1234567890")
	imageBytes := []byte("IMAGE-ORIGINAL-BYTES-abcdefghij")
	if err := os.WriteFile(filepath.Join(dir, "movie.mp4"), videoBytes, 0o644); err != nil {
		t.Fatalf("写视频失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), imageBytes, 0o644); err != nil {
		t.Fatalf("写图片失败: %v", err)
	}
	escaped := strings.ReplaceAll(dir, `\`, `\\`)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths",
		fmt.Sprintf(`{"path":"%s","type":"local","label":"分享测试"}`, escaped),
		map[string]string{"Cookie": cookie})
	if createResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(createResp.Body)
		t.Fatalf("建库失败: %d, body: %s", createResp.StatusCode, string(b))
	}
	var lp models.LibraryPath
	parseJSON(t, createResp, &lp)

	doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})
	items := waitForMediaItems(t, server.URL, cookie, 2)

	var videoID, imageID int64
	for _, m := range items {
		switch {
		case strings.HasSuffix(m.FileName, ".mp4"):
			videoID = m.ID
		case strings.HasSuffix(m.FileName, ".jpg"):
			imageID = m.ID
		}
	}
	if videoID == 0 || imageID == 0 {
		t.Fatalf("未取到视频/图片 ID: video=%d image=%d", videoID, imageID)
	}

	// 为视频建媒体分享
	token := createShare(t, server.URL, cookie, models.ShareResourceMedia, videoID)

	// ── 以下全部不带 Cookie（= 无痕/免登）────────────────────────────

	// 1) 公开查看页 /s/:token 免登返回 SPA 壳（200 html，不跳登录）
	pageResp := doRequest(t, "GET", server.URL+"/s/"+token, nil, nil)
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("/s/:token 免登应 200, 实际 %d", pageResp.StatusCode)
	}
	if ct := pageResp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("/s/:token 应返回 html, 实际 %q", ct)
	}
	pageResp.Body.Close()

	// 2) 分享元信息（免登）
	infoResp := doRequest(t, "GET", server.URL+"/api/share/"+token, nil, nil)
	if infoResp.StatusCode != http.StatusOK {
		t.Fatalf("分享元信息免登应 200, 实际 %d", infoResp.StatusCode)
	}
	var info struct {
		ResourceType string            `json:"resource_type"`
		Media        *models.MediaFile `json:"media"`
	}
	parseJSON(t, infoResp, &info)
	if info.ResourceType != "media" || info.Media == nil || info.Media.ID != videoID {
		t.Fatalf("元信息不符: %+v", info)
	}

	// 3) 下载原文件（免登）：附件头 + 字节一致
	dlResp := doRequest(t, "GET", fmt.Sprintf("%s/api/share/%s/media/%d/download", server.URL, token, videoID), nil, nil)
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("免登下载应 200, 实际 %d", dlResp.StatusCode)
	}
	if cd := dlResp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("下载应为附件, 实际 %q", cd)
	}
	dlBody, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()
	if string(dlBody) != string(videoBytes) {
		t.Fatalf("下载字节不一致: %q", string(dlBody))
	}

	// 4) 视频渐进式播放（免登）：带 Range 应 206 Partial Content + 片段字节
	streamURL := fmt.Sprintf("%s/api/share/%s/media/%d/stream", server.URL, token, videoID)
	rangeResp := doRequest(t, "GET", streamURL, nil, map[string]string{"Range": "bytes=0-4"})
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("带 Range 的流应 206, 实际 %d", rangeResp.StatusCode)
	}
	part, _ := io.ReadAll(rangeResp.Body)
	rangeResp.Body.Close()
	if string(part) != string(videoBytes[:5]) {
		t.Fatalf("Range 片段不符: 期望 %q, 实际 %q", string(videoBytes[:5]), string(part))
	}

	// 5) 范围隔离（安全核心）：用该 token 访问不在范围内的图片 → 404
	outResp := doRequest(t, "GET", fmt.Sprintf("%s/api/share/%s/media/%d/download", server.URL, token, imageID), nil, nil)
	if outResp.StatusCode != http.StatusNotFound {
		t.Fatalf("越权访问范围外媒体应 404, 实际 %d", outResp.StatusCode)
	}
	outResp.Body.Close()

	// 6) 豁免边界：管理端点 /api/shares 不被豁免，无 Cookie → 401
	mgmtResp := doRequest(t, "GET", server.URL+"/api/shares", nil, nil)
	if mgmtResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("管理端点免登应 401, 实际 %d", mgmtResp.StatusCode)
	}
	mgmtResp.Body.Close()

	// 7) 撤销后免登访问失效 → 404
	delResp := doRequest(t, "DELETE", server.URL+"/api/shares/"+token, nil, map[string]string{"Cookie": cookie})
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("撤销应 204, 实际 %d", delResp.StatusCode)
	}
	delResp.Body.Close()
	goneResp := doRequest(t, "GET", server.URL+"/api/share/"+token, nil, nil)
	if goneResp.StatusCode != http.StatusNotFound {
		t.Fatalf("撤销后免登访问应 404, 实际 %d", goneResp.StatusCode)
	}
	goneResp.Body.Close()
}

// TestE2E_Share_AlbumScopePublic 相册分享：成员免登可下载、非成员 404。
func TestE2E_Share_AlbumScopePublic(t *testing.T) {
	server, tmpDir := newShareTestServer(t)
	cookie := shareLogin(t, server.URL)

	dir := filepath.Join(tmpDir, "album")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "in.jpg"), []byte("IN-ALBUM"), 0o644)
	os.WriteFile(filepath.Join(dir, "out.jpg"), []byte("OUT-ALBUM"), 0o644)
	escaped := strings.ReplaceAll(dir, `\`, `\\`)
	createResp := doRequest(t, "POST", server.URL+"/api/library/paths",
		fmt.Sprintf(`{"path":"%s","type":"local","label":"相册分享"}`, escaped),
		map[string]string{"Cookie": cookie})
	var lp models.LibraryPath
	parseJSON(t, createResp, &lp)
	doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", server.URL, lp.ID), nil,
		map[string]string{"Cookie": cookie})
	items := waitForMediaItems(t, server.URL, cookie, 2)

	var inID, outID int64
	for _, m := range items {
		if strings.HasSuffix(m.FileName, "in.jpg") {
			inID = m.ID
		} else if strings.HasSuffix(m.FileName, "out.jpg") {
			outID = m.ID
		}
	}

	// 建相册并只把 in.jpg 加入
	albResp := doRequest(t, "POST", server.URL+"/api/albums", `{"name":"分享相册","description":""}`,
		map[string]string{"Cookie": cookie})
	var album models.Album
	parseJSON(t, albResp, &album)
	doRequest(t, "POST", fmt.Sprintf("%s/api/albums/%d/items", server.URL, album.ID),
		fmt.Sprintf(`{"media_id":%d}`, inID), map[string]string{"Cookie": cookie})

	token := createShare(t, server.URL, cookie, models.ShareResourceAlbum, album.ID)

	// 成员 in.jpg 免登可下载
	inResp := doRequest(t, "GET", fmt.Sprintf("%s/api/share/%s/media/%d/download", server.URL, token, inID), nil, nil)
	if inResp.StatusCode != http.StatusOK {
		t.Fatalf("相册成员免登下载应 200, 实际 %d", inResp.StatusCode)
	}
	inResp.Body.Close()
	// 非成员 out.jpg → 404
	outResp := doRequest(t, "GET", fmt.Sprintf("%s/api/share/%s/media/%d/download", server.URL, token, outID), nil, nil)
	if outResp.StatusCode != http.StatusNotFound {
		t.Fatalf("非相册成员应 404, 实际 %d", outResp.StatusCode)
	}
	outResp.Body.Close()
}
