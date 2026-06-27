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

func TestBrowseDirectory_MissingParentPath(t *testing.T) {
	router, _ := setupTestRouter(t)

	// 缺少 parent_path → 400
	req := httptest.NewRequest("GET", "/api/library/browse", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 parent_path 期望 400, 实际 %d", w.Code)
	}
}

// TestBrowseDirectory_LibraryIDIgnored library_id 已弃用（FR-121）：
// 不带 library_id 也能浏览，跨库合并返回结果。
func TestBrowseDirectory_LibraryIDIgnored(t *testing.T) {
	router, svc := setupTestRouter(t)
	// 不同库的同目录文件
	_, _ = svc.CreateMediaFile(1, "/media/movies/电影A.mkv", 1024)
	_, _ = svc.CreateMediaFile(2, "/media/movies/电影B.mkv", 2048)

	// 不带 library_id（旧版会 400，新版应 200 并跨库合并）
	req := httptest.NewRequest("GET", "/api/library/browse?parent_path=/media/movies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("不带 library_id 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := parseJSON(w.Body, &resp); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	files, ok := resp["files"].([]interface{})
	if !ok || len(files) != 2 {
		t.Fatalf("期望跨库合并 2 个文件, 实际: %v", resp["files"])
	}
}

func TestBrowseDirectory_EmptyResult(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/library/browse?parent_path=/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("空目录期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

// TestBrowseDirectory_SortParam sort 参数透传后端（FR-121）：按大小升序返回文件。
func TestBrowseDirectory_SortParam(t *testing.T) {
	router, svc := setupTestRouter(t)
	_, _ = svc.CreateMediaFile(1, "/s/big.mp4", 300)
	_, _ = svc.CreateMediaFile(1, "/s/small.mp4", 100)
	_, _ = svc.CreateMediaFile(1, "/s/mid.mp4", 200)

	req := httptest.NewRequest("GET", "/api/library/browse?parent_path=/s&sort=size", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sort=size 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Files []struct {
			FileName string `json:"file_name"`
			FileSize int64  `json:"file_size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	if len(resp.Files) != 3 {
		t.Fatalf("期望 3 个文件, 实际 %d", len(resp.Files))
	}
	// 按大小升序：small(100) < mid(200) < big(300)
	want := []string{"small.mp4", "mid.mp4", "big.mp4"}
	for i, f := range resp.Files {
		if f.FileName != want[i] {
			t.Fatalf("sort=size 文件[%d] 期望 %s, 实际 %s", i, want[i], f.FileName)
		}
	}
}

// parseJSON 辅助函数：解析响应体 JSON。
func parseJSON(body *bytes.Buffer, target interface{}) error {
	return json.Unmarshal(body.Bytes(), target)
}

// 确保 json 导入可用
var _ = gin.New // 防止 gin 被移除
