package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestStreamHandler_InvalidID 测试无效的媒体 ID 格式。
func TestStreamHandler_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/invalid/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 无效 ID 格式应返回 400
	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestGetProgress 测试获取播放进度。
func TestGetProgress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, pbSvc := setupPlayTestRouter(t)

	pbSvc.HandleBufferReport(1, playback.BufferReport{
		CurrentPosition: 10.0,
		FileSize:        4096,
		BufferedRanges:  [][2]int64{{0, 4096}},
	})

	req := httptest.NewRequest("GET", "/api/play/1/progress", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 已上报进度，应返回 200
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}
}

// TestReportBuffer 测试缓冲区间上报。
func TestReportBuffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	body, _ := json.Marshal(map[string]interface{}{
		"current_position": 15.0,
		"file_size":        4096,
		"buffered_ranges":  [][]int{{0, 2048}, {2048, 4096}},
	})
	req := httptest.NewRequest("POST", "/api/play/1/buffer", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
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

// TestStreamInvalidIDFormat 测试无效 ID 格式。
func TestStreamInvalidIDFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	req := httptest.NewRequest("GET", "/api/play/abc/stream", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestSeekInvalidJSON 测试无效 JSON。
func TestSeekInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, _, _ := setupPlayTestRouter(t)

	body := `invalid json`
	req := httptest.NewRequest("POST", "/api/play/1/seek", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}
