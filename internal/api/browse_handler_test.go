package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBrowseDirectory_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	// 插入测试数据
	_, _ = svc.CreateMediaFile(1, "/media/movies/星际穿越.mkv", 1024)
	_, _ = svc.CreateMediaFile(1, "/media/tv/绝命毒师.S01E01.mkv", 2048)

	req := httptest.NewRequest("GET", "/api/library/browse?library_id=1&parent_path=/media", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := parseJSON(w.Body, &resp); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}

	breadcrumbs, ok := resp["breadcrumbs"].([]interface{})
	if !ok || len(breadcrumbs) != 1 {
		t.Fatalf("期望 1 段面包屑, 实际: %v", resp["breadcrumbs"])
	}

	dirs, ok := resp["directories"].([]interface{})
	if !ok || len(dirs) != 2 {
		t.Fatalf("期望 2 个子目录, 实际: %v", resp["directories"])
	}

	files, ok := resp["files"].([]interface{})
	if !ok || len(files) != 0 {
		t.Fatalf("期望 0 个直接文件, 实际: %v", resp["files"])
	}
}

func TestBrowseDirectory_Subdir_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	_, _ = svc.CreateMediaFile(1, "/media/movies/星际穿越.mkv", 1024)

	req := httptest.NewRequest("GET", "/api/library/browse?library_id=1&parent_path=/media/movies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := parseJSON(w.Body, &resp); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}

	// 面包屑：media -> movies
	breadcrumbs, ok := resp["breadcrumbs"].([]interface{})
	if !ok || len(breadcrumbs) != 2 {
		t.Fatalf("期望 2 段面包屑, 实际: %d", len(breadcrumbs))
	}

	// 当前目录下的文件
	files, ok := resp["files"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("期望 1 个文件, 实际: %d", len(files))
	}
}

func TestBrowseDirectory_MissingParams(t *testing.T) {
	router, _ := setupTestRouter(t)

	// 缺少 library_id
	req := httptest.NewRequest("GET", "/api/library/browse?parent_path=/media", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 library_id 期望 400, 实际 %d", w.Code)
	}

	// 缺少 parent_path
	req = httptest.NewRequest("GET", "/api/library/browse?library_id=1", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 parent_path 期望 400, 实际 %d", w.Code)
	}
}

func TestBrowseDirectory_InvalidLibraryID(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/browse?library_id=abc&parent_path=/media", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("无效 library_id 期望 400, 实际 %d", w.Code)
	}
}

func TestBrowseDirectory_EmptyResult(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/browse?library_id=1&parent_path=/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("空目录期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

// parseJSON 辅助函数：解析响应体 JSON。
func parseJSON(body *bytes.Buffer, target interface{}) error {
	return json.Unmarshal(body.Bytes(), target)
}

// 确保 json 导入可用
var _ = gin.New // 防止 gin 被移除
