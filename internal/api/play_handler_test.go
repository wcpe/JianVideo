package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
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

// TestHLSRoute_RejectsMediaOutsideRequestedSpace 验证 HLS 直出不会跨 Space 读取媒体。
func TestHLSRoute_RejectsMediaOutsideRequestedSpace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	defaultMedia := models.MediaFile{SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: "D:/default.mp4", FileName: "default.mp4"}
	otherMedia := models.MediaFile{SpaceID: "space-other", LibraryID: 2, FilePath: "D:/other.mp4", FileName: "other.mp4"}
	if err := db.Create(&defaultMedia).Error; err != nil {
		t.Fatalf("创建默认 Space 媒体失败: %v", err)
	}
	if err := db.Create(&otherMedia).Error; err != nil {
		t.Fatalf("创建其他 Space 媒体失败: %v", err)
	}

	hlsMgr := player.NewHLSManager(t.TempDir())
	if err := hlsMgr.SaveMasterM3U8(defaultMedia.ID, "#EXTM3U\n"); err != nil {
		t.Fatalf("创建 HLS master 失败: %v", err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("space_id", c.GetHeader("X-JianVideo-Space-Id"))
		c.Next()
	})
	RegisterHLSRoutes(router, hlsMgr, t.TempDir(), library.NewService(db))

	deniedReq := httptest.NewRequest(http.MethodGet, "/api/play/hls/"+strconv.FormatInt(defaultMedia.ID, 10)+"/master.m3u8", nil)
	deniedReq.Header.Set("X-JianVideo-Space-Id", "space-other")
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, deniedReq)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("其他 Space 不得读取默认 Space HLS, 期望 404, 实际 %d", denied.Code)
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/api/play/hls/"+strconv.FormatInt(defaultMedia.ID, 10)+"/master.m3u8", nil)
	allowedReq.Header.Set("X-JianVideo-Space-Id", models.DefaultSpaceID)
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, allowedReq)
	if allowed.Code != http.StatusOK || allowed.Body.String() != "#EXTM3U\n" {
		t.Fatalf("默认 Space 应可读取自身 HLS, code=%d body=%q", allowed.Code, allowed.Body.String())
	}
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
