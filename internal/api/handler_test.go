package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *library.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(gdb)
	h := NewHandler(svc)

	r := gin.New()
	RegisterRoutes(r, h)
	return r, svc
}

func TestCreateLibraryPath_API(t *testing.T) {
	router, _ := setupTestRouter(t)

	body := `{"path":"/tmp/test","type":"local","label":"测试"}"`
	req := httptest.NewRequest("POST", "/api/library/paths", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["path"] == nil {
		t.Fatal("响应缺少 path 字段")
	}
}

func TestListLibraryPaths_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	_, _ = svc.CreateLibraryPath("/tmp/dir1", "local", "目录1")

	req := httptest.NewRequest("GET", "/api/library/paths", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, ok := resp["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("期望 1 条记录, 实际响应: %s", w.Body.String())
	}
}

func TestDeleteLibraryPath_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	lp, _ := svc.CreateLibraryPath("/tmp/to_del", "local", "")

	req := httptest.NewRequest("DELETE", "/api/library/paths/"+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("期望 204, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCreateMediaFileAndList_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	lp, err := svc.CreateLibraryPath("/tmp/media_test", "local", "")
	if err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	_, _ = svc.CreateMediaFile(lp.ID, "/tmp/media_test/video.mp4", 1024)

	req := httptest.NewRequest("GET", "/api/library/media?library_id="+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"].(float64) != 1 {
		t.Fatalf("期望 total=1, 实际 %v", resp["total"])
	}
}

func TestGetMediaFile_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, _ := svc.CreateMediaFile(1, "/tmp/single.mp4", 2048)

	req := httptest.NewRequest("GET", "/api/library/media/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["file_name"] != "single.mp4" {
		t.Fatalf("期望 file_name=single.mp4, 实际 %v", resp["file_name"])
	}
}

func TestDeleteMediaFile_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	mf, _ := svc.CreateMediaFile(1, "/tmp/to_del.mp4", 512)

	req := httptest.NewRequest("DELETE", "/api/library/media/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("期望 204, 实际 %d", w.Code)
	}
}

func TestScanLibrary_API(t *testing.T) {
	router, svc := setupTestRouter(t)

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "scan_test.mp4"), []byte("fake"), 0o644)

	lp, _ := svc.CreateLibraryPath(dir, "local", "扫描测试")

	req := httptest.NewRequest("POST", "/api/library/scan/"+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["scanned"].(float64) != 1 {
		t.Fatalf("期望 scanned=1, 实际 %v", resp["scanned"])
	}
}

// ─── 错误路径测试 ──────────────────────────────────────

func TestCreateLibraryPath_InvalidJSON(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/library/paths", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestDeleteLibraryPath_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("DELETE", "/api/library/paths/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestGetMediaFile_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/media/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestGetMediaFile_NotFound(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/media/99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

func TestDeleteMediaFile_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("DELETE", "/api/library/media/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestScanLibrary_InvalidID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/library/scan/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}
