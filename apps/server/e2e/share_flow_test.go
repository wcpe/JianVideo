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

	"github.com/wcpe/JianVideo/config"
	"github.com/wcpe/JianVideo/internal/api"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/share"
	"github.com/wcpe/JianVideo/internal/web"
)

// newShareTestServer 构造一台「生产同构」测试服务器（FR-43 端到端用）：
// 注入分享服务 + 播放服务 + 完整迁移（含 Album/Share）+ 桩内嵌前端，
// 以便在真实 HTTP 层验证「免登 + APIGuard 豁免 + 范围隔离 + Range 流 + 下载」。
// 返回 gormDB 供测试直接操纵过期时间等（每台服务器一套 t.TempDir 隔离库）。
func newShareTestServer(t *testing.T) (*httptest.Server, *gorm.DB, string) {
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
		"web/dist/index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>JianVideo</title>")},
		"web/dist/assets/app.js": &fstest.MapFile{Data: []byte("// app")},
	}

	hlsMgr := player.NewHLSManager(hlsDir)
	srv := web.NewRouter(cfg, gormDB, hlsMgr, frontendFS, handler, pbSvc)
	seedAdmin(t, gormDB, cfg.JWTSecret)

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
			_ = sqlDB.Close() // 测试清理，忽略关闭错误
		}
	})

	return server, gormDB, tmpDir
}

// shareLogin 登录并返回认证 Cookie。
func shareLogin(t *testing.T, serverURL string) string {
	t.Helper()
	resp := doRequest(t, "POST", serverURL+"/api/auth/login", `{"username":"admin","password":"admin"}`, nil)
	defer func() { _ = resp.Body.Close() }() // 测试清理，忽略关闭错误
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}
	return resp.Header.Get("Set-Cookie")
}

// createLibraryWithMedia 建库并放入指定文件（name→bytes），扫描后返回库 ID 与文件名→媒体 ID 映射。
func createLibraryWithMedia(t *testing.T, serverURL, cookie, dir, label string, files map[string][]byte) (int64, map[string]int64) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("写文件 %s 失败: %v", name, err)
		}
	}
	escaped := strings.ReplaceAll(dir, `\`, `\\`)
	createResp := doRequest(t, "POST", serverURL+"/api/library/paths",
		fmt.Sprintf(`{"path":"%s","type":"local","label":"%s"}`, escaped, label),
		map[string]string{"Cookie": cookie})
	defer func() { _ = createResp.Body.Close() }() // 测试清理，忽略关闭错误
	if createResp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(createResp.Body)
		t.Fatalf("建库失败: %d, body: %s", createResp.StatusCode, string(b))
	}
	var lp models.LibraryPath
	parseJSON(t, createResp, &lp)

	scanResp := doRequest(t, "POST", fmt.Sprintf("%s/api/library/scan/%d", serverURL, lp.ID), nil,
		map[string]string{"Cookie": cookie})
	if scanResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(scanResp.Body)
		t.Fatalf("触发扫描失败: %d, body: %s", scanResp.StatusCode, string(b))
	}
	_ = scanResp.Body.Close() // 测试清理，忽略关闭错误

	items := waitForLibraryMedia(t, serverURL, cookie, lp.ID, len(files))
	idByName := make(map[string]int64, len(files))
	for _, m := range items {
		if _, want := files[m.FileName]; want {
			idByName[m.FileName] = m.ID
		}
	}
	for name := range files {
		if idByName[name] == 0 {
			t.Fatalf("未取到媒体 ID: %s（入库映射 %v）", name, idByName)
		}
	}
	return lp.ID, idByName
}

// waitForLibraryMedia 按 library_id 过滤轮询，等待本库恰好/至少 want 条入库。
// 相比全局 waitForMediaItems，显式按库隔离，不隐式依赖「TempDir 全局空库」前提。
func waitForLibraryMedia(t *testing.T, serverURL, cookie string, libraryID int64, want int) []models.MediaFile {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := doRequest(t, "GET",
			fmt.Sprintf("%s/api/library/media?library_id=%d&page_size=100", serverURL, libraryID), nil,
			map[string]string{"Cookie": cookie})
		var result struct {
			Items []models.MediaFile `json:"items"`
		}
		parseJSON(t, resp, &result)
		_ = resp.Body.Close() // 测试清理，忽略关闭错误
		if len(result.Items) >= want {
			return result.Items
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待本库媒体入库超时：库 %d 期望 ≥%d，实际 %d", libraryID, want, len(result.Items))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// createShare 经管理端点创建分享，expiresInHours>0 设过期、否则永不过期；返回 token。
func createShare(t *testing.T, serverURL, cookie, resourceType string, resourceID int64, expiresInHours int) string {
	t.Helper()
	body := fmt.Sprintf(`{"resource_type":"%s","resource_id":%d,"expires_in_hours":%d}`, resourceType, resourceID, expiresInHours)
	resp := doRequest(t, "POST", serverURL+"/api/shares", body, map[string]string{"Cookie": cookie})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("创建分享失败: %d, body: %s", resp.StatusCode, string(b))
	}
	var sh models.Share
	parseJSON(t, resp, &sh)
	if sh.Token == "" {
		t.Fatal("分享 token 不应为空")
	}
	return sh.Token
}

// shareMediaStatus 免登 GET 某分享媒体子端点，返回状态码（不带 Cookie = 无痕）。
func shareMediaStatus(t *testing.T, serverURL, token string, mediaID int64, suffix string, headers map[string]string) int {
	t.Helper()
	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/share/%s/media/%d/%s", serverURL, token, mediaID, suffix), nil, headers)
	defer func() { _ = resp.Body.Close() }() // 测试清理，忽略关闭错误
	return resp.StatusCode
}

// TestE2E_Share_PublicAccessFlow 端到端验证 FR-43：authed 建库扫描建分享 → 免登（无 Cookie）访问。
func TestE2E_Share_PublicAccessFlow(t *testing.T) {
	server, gdb, tmpDir := newShareTestServer(t)
	cookie := shareLogin(t, server.URL)

	videoBytes := []byte("VIDEO-ORIGINAL-BYTES-1234567890")
	imageBytes := []byte("IMAGE-ORIGINAL-BYTES-abcdefghij")
	_, ids := createLibraryWithMedia(t, server.URL, cookie, filepath.Join(tmpDir, "media"), "分享测试",
		map[string][]byte{"movie.mp4": videoBytes, "photo.jpg": imageBytes})
	videoID, imageID := ids["movie.mp4"], ids["photo.jpg"]

	// 为视频建媒体分享（永不过期）
	token := createShare(t, server.URL, cookie, models.ShareResourceMedia, videoID, 0)

	// ── 以下全部不带 Cookie（= 无痕/免登）────────────────────────────

	// 1) 公开查看页 /s/:token 免登返回 SPA 壳（200 html，不跳登录）
	pageResp := doRequest(t, "GET", server.URL+"/s/"+token, nil, nil)
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("/s/:token 免登应 200, 实际 %d", pageResp.StatusCode)
	}
	if ct := pageResp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("/s/:token 应返回 html, 实际 %q", ct)
	}
	_ = pageResp.Body.Close() // 测试清理，忽略关闭错误

	// 2) 分享元信息（免登）
	infoResp := doRequest(t, "GET", server.URL+"/api/share/"+token, nil, nil)
	defer func() { _ = infoResp.Body.Close() }() // 测试清理，忽略关闭错误
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
	_ = dlResp.Body.Close() // 测试清理，忽略关闭错误
	if string(dlBody) != string(videoBytes) {
		t.Fatalf("下载字节不一致: %q", string(dlBody))
	}

	// 4) 视频 stream 端点免登 Range 转发：带 Range 应 206 Partial Content + 片段字节
	//    （此处验证 stream 端点的 Range 转发链路；真实可解码播放由 Playwright 真浏览器用例覆盖）
	streamURL := fmt.Sprintf("%s/api/share/%s/media/%d/stream", server.URL, token, videoID)
	rangeResp := doRequest(t, "GET", streamURL, nil, map[string]string{"Range": "bytes=0-4"})
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("带 Range 的流应 206, 实际 %d", rangeResp.StatusCode)
	}
	part, _ := io.ReadAll(rangeResp.Body)
	_ = rangeResp.Body.Close() // 测试清理，忽略关闭错误
	if string(part) != string(videoBytes[:5]) {
		t.Fatalf("Range 片段不符: 期望 %q, 实际 %q", string(videoBytes[:5]), string(part))
	}

	// 5) 范围内 thumbnail/raw 也免登可达（证明 raw/thumbnail 端点的范围门已穿过）
	//    缩略图可能尚在异步生成 → 200 或 202 均视为门已开
	if code := shareMediaStatus(t, server.URL, token, videoID, "thumbnail", nil); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("范围内 thumbnail 应 200/202, 实际 %d", code)
	}
	// 对视频请求 raw → 范围门通过后被 serveRawImage 以「非图片」拒为 400（≠404 证明门是开的）
	if code := shareMediaStatus(t, server.URL, token, videoID, "raw", nil); code != http.StatusBadRequest {
		t.Fatalf("范围内视频 raw 应 400(非图片), 实际 %d", code)
	}

	// 6) 范围隔离（安全核心）：用该 token 访问不在范围内的图片，raw/thumbnail/download/stream 全部 404
	for _, suffix := range []string{"raw", "thumbnail", "download", "stream"} {
		if code := shareMediaStatus(t, server.URL, token, imageID, suffix, nil); code != http.StatusNotFound {
			t.Fatalf("越权访问范围外媒体 %s 应 404, 实际 %d", suffix, code)
		}
	}

	// 7) 豁免边界：管理端点 /api/shares 不被豁免，无 Cookie → 401（GET 与 DELETE 都验）
	if mgmt := doRequest(t, "GET", server.URL+"/api/shares", nil, nil); mgmt.StatusCode != http.StatusUnauthorized {
		_ = mgmt.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("GET /api/shares 免登应 401, 实际 %d", mgmt.StatusCode)
	} else {
		_ = mgmt.Body.Close() // 测试清理，忽略关闭错误
	}
	if del := doRequest(t, "DELETE", server.URL+"/api/shares/"+token, nil, nil); del.StatusCode != http.StatusUnauthorized {
		_ = del.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("DELETE /api/shares/:token 免登应 401, 实际 %d", del.StatusCode)
	} else {
		_ = del.Body.Close() // 测试清理，忽略关闭错误
	}

	// 8) 伪造/不存在 token → 404（API 层独立断言，与前端文案解耦）
	if forged := doRequest(t, "GET", server.URL+"/api/share/deadbeefdeadbeefdeadbeef", nil, nil); forged.StatusCode != http.StatusNotFound {
		_ = forged.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("伪造 token 应 404, 实际 %d", forged.StatusCode)
	} else {
		_ = forged.Body.Close() // 测试清理，忽略关闭错误
	}

	// 9) 过期 token → 404：另建一个分享，把 ExpiresAt 改到过去，免登访问应失效
	expToken := createShare(t, server.URL, cookie, models.ShareResourceMedia, videoID, 0)
	past := time.Now().Add(-time.Hour)
	if err := gdb.Model(&models.Share{}).Where("token = ?", expToken).Update("expires_at", past).Error; err != nil {
		t.Fatalf("置过期失败: %v", err)
	}
	if exp := doRequest(t, "GET", server.URL+"/api/share/"+expToken, nil, nil); exp.StatusCode != http.StatusNotFound {
		_ = exp.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("过期 token 应 404, 实际 %d", exp.StatusCode)
	} else {
		_ = exp.Body.Close() // 测试清理，忽略关闭错误
	}

	// 10) 撤销前后对照（让 204 有证明力，而非空操作也返回 204）：撤销前 200 → 撤销 204 → 撤销后 404
	if pre := doRequest(t, "GET", server.URL+"/api/share/"+token, nil, nil); pre.StatusCode != http.StatusOK {
		_ = pre.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("撤销前 token 应仍有效 200, 实际 %d", pre.StatusCode)
	} else {
		_ = pre.Body.Close() // 测试清理，忽略关闭错误
	}
	delResp := doRequest(t, "DELETE", server.URL+"/api/shares/"+token, nil, map[string]string{"Cookie": cookie})
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("撤销应 204, 实际 %d", delResp.StatusCode)
	}
	_ = delResp.Body.Close() // 测试清理，忽略关闭错误
	if goneResp := doRequest(t, "GET", server.URL+"/api/share/"+token, nil, nil); goneResp.StatusCode != http.StatusNotFound {
		_ = goneResp.Body.Close() // 测试清理，忽略关闭错误
		t.Fatalf("撤销后免登访问应 404, 实际 %d", goneResp.StatusCode)
	} else {
		_ = goneResp.Body.Close() // 测试清理，忽略关闭错误
	}
}

// TestE2E_Share_AlbumScopePublic 相册分享：成员免登可下载（字节一致），非成员各端点 404。
func TestE2E_Share_AlbumScopePublic(t *testing.T) {
	server, _, tmpDir := newShareTestServer(t)
	cookie := shareLogin(t, server.URL)

	inBytes := []byte("IN-ALBUM-BYTES")
	outBytes := []byte("OUT-ALBUM-BYTES")
	_, ids := createLibraryWithMedia(t, server.URL, cookie, filepath.Join(tmpDir, "album"), "相册分享",
		map[string][]byte{"in.jpg": inBytes, "out.jpg": outBytes})
	inID, outID := ids["in.jpg"], ids["out.jpg"]

	// 建相册并只把 in.jpg 加入
	albResp := doRequest(t, "POST", server.URL+"/api/albums", `{"name":"分享相册","description":""}`,
		map[string]string{"Cookie": cookie})
	defer func() { _ = albResp.Body.Close() }() // 测试清理，忽略关闭错误
	if albResp.StatusCode != http.StatusCreated && albResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(albResp.Body)
		t.Fatalf("建相册失败: %d, body: %s", albResp.StatusCode, string(b))
	}
	var album models.Album
	parseJSON(t, albResp, &album)
	addResp := doRequest(t, "POST", fmt.Sprintf("%s/api/albums/%d/items", server.URL, album.ID),
		fmt.Sprintf(`{"media_id":%d}`, inID), map[string]string{"Cookie": cookie})
	if addResp.StatusCode != http.StatusOK && addResp.StatusCode != http.StatusCreated && addResp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(addResp.Body)
		t.Fatalf("加入相册失败: %d, body: %s", addResp.StatusCode, string(b))
	}
	_ = addResp.Body.Close() // 测试清理，忽略关闭错误

	token := createShare(t, server.URL, cookie, models.ShareResourceAlbum, album.ID, 0)

	// 成员 in.jpg 免登可下载，且字节一致
	inResp := doRequest(t, "GET", fmt.Sprintf("%s/api/share/%s/media/%d/download", server.URL, token, inID), nil, nil)
	if inResp.StatusCode != http.StatusOK {
		t.Fatalf("相册成员免登下载应 200, 实际 %d", inResp.StatusCode)
	}
	inBody, _ := io.ReadAll(inResp.Body)
	_ = inResp.Body.Close() // 测试清理，忽略关闭错误
	if string(inBody) != string(inBytes) {
		t.Fatalf("相册成员下载字节不一致: %q", string(inBody))
	}
	// 成员 raw 免登可达（图片 → 200）
	if code := shareMediaStatus(t, server.URL, token, inID, "raw", nil); code != http.StatusOK {
		t.Fatalf("相册成员 raw 应 200, 实际 %d", code)
	}

	// 非成员 out.jpg：raw/thumbnail/download/stream 全部 404（范围隔离）
	for _, suffix := range []string{"raw", "thumbnail", "download", "stream"} {
		if code := shareMediaStatus(t, server.URL, token, outID, suffix, nil); code != http.StatusNotFound {
			t.Fatalf("非相册成员 %s 应 404, 实际 %d", suffix, code)
		}
	}
}
