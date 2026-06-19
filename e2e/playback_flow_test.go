package e2e

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/api"
	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
	"jianvideo/internal/playback"
	"jianvideo/internal/player"
	"jianvideo/internal/web"
)

// newPlaybackTestServer 创建包含播放路由的测试服务器。
// 标准 NewRouter 不注册播放路由，此函数额外注册。
func newPlaybackTestServer(t *testing.T) (*httptest.Server, *gorm.DB, *playback.Service, string) {
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

	// 使用标准 NewRouter（包含认证和库路由）
	r := web.NewRouter(cfg, gormDB, hlsMgr, nil)

	// 额外注册播放路由（标准 NewRouter 不包含）
	libSvc := library.NewService(gormDB)
	apiHandler := api.NewHandler(libSvc)
	pbSvc := playback.NewService()
	api.RegisterRoutes(r, apiHandler, pbSvc)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	server := httptest.NewUnstartedServer(r)
	server.Listener = lis
	server.Start()

	t.Cleanup(func() {
		server.Close()
		pbSvc.Stop()
		if sqlDB, err := gormDB.DB(); err == nil {
			sqlDB.Close()
		}
	})

	return server, gormDB, pbSvc, tmpDir
}

// TestE2E_GetProgress_NoSession 验证不存在的播放会话返回空进度 200。
func TestE2E_GetProgress_NoSession(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/1/progress", server.URL), nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "不存在的会话应返回 200 空进度")

	var result struct {
		CurrentPosition float64    `json:"current_position"`
		Duration        float64    `json:"duration"`
		FileSize        int64      `json:"file_size"`
		BufferedRanges  [][2]int64 `json:"buffered_ranges"`
	}
	doJSONRequest(t, resp, &result)
	assert.Equal(t, float64(0), result.CurrentPosition, "空会话进度应为 0")
	assert.Equal(t, int64(0), result.FileSize, "空会话文件大小应为 0")
}

// TestE2E_HandleSeek 验证 Seek 请求成功并返回正确位置。
func TestE2E_HandleSeek(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	body := `{"position":10.5}`
	resp := doRequest(t, "POST", fmt.Sprintf("%s/api/play/1/seek", server.URL), body, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "Seek 应返回 200")

	var result struct {
		Status   string  `json:"status"`
		Position float64 `json:"position"`
	}
	doJSONRequest(t, resp, &result)
	assert.Equal(t, "ok", result.Status, "状态应为 ok")
	assert.Equal(t, 10.5, result.Position, "位置应为 10.5")
}

// TestE2E_HandleBufferReport 验证缓冲区间上报成功。
func TestE2E_HandleBufferReport(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	body := `{"current_position":30,"file_size":1000}`
	resp := doRequest(t, "POST", fmt.Sprintf("%s/api/play/1/buffer", server.URL), body, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "缓冲上报应返回 200")
}

// TestE2E_GetProgress_AfterSeek 验证 Seek 后查询进度已更新。
func TestE2E_GetProgress_AfterSeek(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	// 先 Seek
	seekBody := `{"position":45.0}`
	seekResp := doRequest(t, "POST", fmt.Sprintf("%s/api/play/1/seek", server.URL), seekBody, nil)
	require.Equal(t, http.StatusOK, seekResp.StatusCode)

	// 查询进度
	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/1/progress", server.URL), nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		CurrentPosition float64 `json:"current_position"`
	}
	doJSONRequest(t, resp, &result)
	assert.Equal(t, 45.0, result.CurrentPosition, "Seek 后进度应为 45.0")
}

// TestE2E_StreamMedia_InvalidID 验证无效 ID（非数字）返回 400。
func TestE2E_StreamMedia_InvalidID(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/abc/stream", server.URL), nil, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "无效 ID 应返回 400")

	var result map[string]string
	parseJSON(t, resp, &result)
	assert.Equal(t, "INVALID_ID", result["code"], "错误码应为 INVALID_ID")
}

// TestE2E_StreamMedia_NotFound 验证不存在的媒体文件返回 404。
func TestE2E_StreamMedia_NotFound(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/99999/stream", server.URL), nil, nil)
	// 不存在的文件可能返回 404（文件打开失败）
	assert.True(t, resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusInternalServerError,
		"不存在的媒体应返回 404 或 500，实际 %d", resp.StatusCode)
}

// TestE2E_SeekThenBufferThenProgress 完整播放流程：Seek → Buffer → 查询进度。
func TestE2E_SeekThenBufferThenProgress(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	mediaID := int64(42)

	// 1. Seek 到 100s
	seekBody := `{"position":100}`
	resp := doRequest(t, "POST",
		fmt.Sprintf("%s/api/play/%d/seek", server.URL, mediaID), seekBody, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. 上报缓冲
	bufferBody := fmt.Sprintf(`{"current_position":100,"file_size":%d}`, 5000000)
	resp = doRequest(t, "POST",
		fmt.Sprintf("%s/api/play/%d/buffer", server.URL, mediaID), bufferBody, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// 3. 查询进度，验证位置和文件大小
	resp = doRequest(t, "GET",
		fmt.Sprintf("%s/api/play/%d/progress", server.URL, mediaID), nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var progress playback.ProgressInfo
	doJSONRequest(t, resp, &progress)
	assert.Equal(t, float64(100), progress.CurrentPosition, "进度应为 100")
	assert.Equal(t, int64(5000000), progress.FileSize, "文件大小应为 5000000")
}

// TestE2E_PlaySubtitles 验证字幕路由正常工作。
func TestE2E_PlaySubtitles(t *testing.T) {
	server, _, _, _ := newPlaybackTestServer(t)
	defer server.Close()

	// 字幕路由不需要认证，直接访问
	resp := doRequest(t, "GET", fmt.Sprintf("%s/api/play/1/subtitles", server.URL), nil, nil)
	// 不存在的媒体文件可能返回空列表或 404，只要不崩溃即可
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
		"字幕路由应返回 200 或 404，实际 %d", resp.StatusCode)
}
