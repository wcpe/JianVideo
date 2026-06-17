package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/library"
	"jianvideo/internal/playback"
)

// setupPlayTestRouter 创建带播放路由的测试路由器。
func setupPlayTestRouter(t *testing.T) (*gin.Engine, *library.Service, *playback.Service) {
	t.Helper()
	db := setupTestDB(t)
	libSvc := library.NewService(db)
	pbSvc := playback.NewService()
	h := NewHandler(libSvc)

	r := gin.New()
	RegisterRoutes(r, h, pbSvc)
	return r, libSvc, pbSvc
}

// createTestMediaFile 创建测试用的媒体文件和数据库记录。
func createTestMediaFile(t *testing.T, svc *library.Service, dir string, name string, size int64) (int64, string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	fullPath := filepath.Join(dir, name)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	lp, err := svc.CreateLibraryPath(dir, "local", "测试目录")
	if err != nil {
		t.Fatalf("创建目录记录失败: %v", err)
	}

	mf, err := svc.CreateMediaFile(lp.ID, fullPath, size)
	if err != nil {
		t.Fatalf("创建媒体文件记录失败: %v", err)
	}

	return mf.ID, fullPath
}

// TestStreamWithoutRange 测试无 Range 请求时返回完整文件。
func TestStreamWithoutRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, svc, _ := setupPlayTestRouter(t)
	dir := t.TempDir()
	mediaID, _ := createTestMediaFile(t, svc, dir, "test.mp4", 4096)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/stream", mediaID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	acceptRanges := w.Header().Get("Accept-Ranges")
	if acceptRanges != "bytes" {
		t.Fatalf("期望 Accept-Ranges: bytes, 实际 %q", acceptRanges)
	}

	if w.Body.Len() != 4096 {
		t.Fatalf("期望响应体 4096 字节, 实际 %d", w.Body.Len())
	}
}

// TestStreamWithRange 测试带 Range 请求时返回 206 Partial Content。
func TestStreamWithRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, svc, _ := setupPlayTestRouter(t)
	dir := t.TempDir()
	mediaID, _ := createTestMediaFile(t, svc, dir, "test_range.mp4", 4096)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/stream", mediaID), nil)
	req.Header.Set("Range", "bytes=0-1023")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("期望 206, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	contentRange := w.Header().Get("Content-Range")
	if !strings.HasPrefix(contentRange, "bytes 0-1023/") {
		t.Fatalf("期望 Content-Range 以 'bytes 0-1023/' 开头, 实际 %q", contentRange)
	}

	if w.Body.Len() != 1024 {
		t.Fatalf("期望响应体 1024 字节, 实际 %d", w.Body.Len())
	}
}

// TestStreamFileNotFound 测试媒体文件不存在时返回 404。
func TestStreamFileNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/9999/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

// TestSeek 测试 Seek 操作。
func TestSeek(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, svc, _ := setupPlayTestRouter(t)
	dir := t.TempDir()
	mediaID, _ := createTestMediaFile(t, svc, dir, "seek_test.mp4", 4096)

	body, _ := json.Marshal(map[string]float64{"position": 30.5})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/play/%d/seek", mediaID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Fatalf("期望 status=ok, 实际 %v", resp["status"])
	}
	if resp["position"] != 30.5 {
		t.Fatalf("期望 position=30.5, 实际 %v", resp["position"])
	}
}

// TestSeekInvalidInput 测试 Seek 无效输入。
func TestSeekInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	body := `{"invalid": true}`
	req := httptest.NewRequest("POST", "/api/play/1/seek", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestGetProgress 测试获取播放进度。
func TestGetProgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, svc, pbSvc := setupPlayTestRouter(t)
	dir := t.TempDir()
	mediaID, _ := createTestMediaFile(t, svc, dir, "progress_test.mp4", 4096)

	pbSvc.HandleBufferReport(mediaID, playback.BufferReport{
		CurrentPosition: 10.0,
		FileSize:        4096,
		BufferedRanges:  [][2]int64{{0, 4096}},
	})

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/play/%d/progress", mediaID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["file_size"].(float64) != 4096 {
		t.Fatalf("期望 file_size=4096, 实际 %v", resp["file_size"])
	}
}

// TestReportBuffer 测试缓冲区间上报。
func TestReportBuffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, svc, _ := setupPlayTestRouter(t)
	dir := t.TempDir()
	mediaID, _ := createTestMediaFile(t, svc, dir, "buffer_test.mp4", 4096)

	body, _ := json.Marshal(map[string]interface{}{
		"current_position": 15.0,
		"file_size":        4096,
		"buffered_ranges":  [][]int{{0, 2048}, {2048, 4096}},
	})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/play/%d/buffer", mediaID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

// TestStreamInvalidID 测试无效的媒体 ID。
func TestStreamInvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/invalid/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}
