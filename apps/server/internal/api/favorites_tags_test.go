package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// setupTagRouter 创建带标签表迁移的测试路由与服务。
func setupTagRouter(t *testing.T) (*gin.Engine, *library.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.Tag{},
		&models.TagMapping{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := library.NewService(gdb)
	h := NewHandler(svc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, svc
}

// seedMedia 入库一条媒体并返回 ID。
func seedMedia(t *testing.T, svc *library.Service, libraryID int64, name string) int64 {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, "D:/Videos/"+name, 1024)
	if err != nil {
		t.Fatalf("入库媒体失败: %v", err)
	}
	return mf.ID
}

func doJSON(t *testing.T, router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestSetFavorite_API(t *testing.T) {
	router, svc := setupTagRouter(t)
	id := seedMedia(t, svc, 1, "a.mp4")

	w := doJSON(t, router, "PUT", "/api/library/media/"+strconv.FormatInt(id, 10)+"/favorite", `{"favorite":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["favorite"] != true {
		t.Fatalf("期望响应 favorite=true, 实际 %v", resp["favorite"])
	}
}

func TestSetFavorite_API_NotFound(t *testing.T) {
	router, _ := setupTagRouter(t)
	w := doJSON(t, router, "PUT", "/api/library/media/999/favorite", `{"favorite":true}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

func TestCreateAndListTags_API(t *testing.T) {
	router, _ := setupTagRouter(t)

	w := doJSON(t, router, "POST", "/api/library/tags", `{"name":"旅行"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建标签期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	w = doJSON(t, router, "GET", "/api/library/tags", "")
	if w.Code != http.StatusOK {
		t.Fatalf("列标签期望 200, 实际 %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("期望 1 个标签, 实际响应: %s", w.Body.String())
	}
}

func TestCreateTag_API_EmptyName(t *testing.T) {
	router, _ := setupTagRouter(t)
	w := doJSON(t, router, "POST", "/api/library/tags", `{"name":"  "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

func TestAddListRemoveMediaTag_API(t *testing.T) {
	router, svc := setupTagRouter(t)
	mediaID := seedMedia(t, svc, 1, "a.mp4")
	mediaPath := "/api/library/media/" + strconv.FormatInt(mediaID, 10) + "/tags"

	// 用 name 直接打标签（自动建标签 + 绑定）
	w := doJSON(t, router, "POST", mediaPath, `{"name":"风景"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("打标签期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var tag map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &tag)
	tagID := int64(tag["id"].(float64))

	// 列出媒体标签
	w = doJSON(t, router, "GET", mediaPath, "")
	if w.Code != http.StatusOK {
		t.Fatalf("列媒体标签期望 200, 实际 %d", w.Code)
	}
	var listResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if items, _ := listResp["items"].([]any); len(items) != 1 {
		t.Fatalf("期望媒体含 1 个标签, 实际响应: %s", w.Body.String())
	}

	// 去标签
	w = doJSON(t, router, "DELETE", mediaPath+"/"+strconv.FormatInt(tagID, 10), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("去标签期望 204, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, router, "GET", mediaPath, "")
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if items, _ := listResp["items"].([]any); len(items) != 0 {
		t.Fatalf("期望去标签后无标签, 实际响应: %s", w.Body.String())
	}
}

func TestListMedia_API_FavoriteFilter(t *testing.T) {
	router, svc := setupTagRouter(t)
	id1 := seedMedia(t, svc, 1, "a.mp4")
	seedMedia(t, svc, 1, "b.mp4")
	if _, err := svc.SetMediaFavorite(id1, true); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}

	w := doJSON(t, router, "GET", "/api/library/media?library_id=1&favorite=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"].(float64) != 1 {
		t.Fatalf("期望 total=1, 实际 %v, body: %s", resp["total"], w.Body.String())
	}
}

func TestListMedia_API_TagFilter(t *testing.T) {
	router, svc := setupTagRouter(t)
	id1 := seedMedia(t, svc, 1, "a.mp4")
	seedMedia(t, svc, 1, "b.mp4")
	tag, _ := svc.CreateTag("精选")
	if err := svc.AddMediaTag(id1, tag.ID); err != nil {
		t.Fatalf("打标签失败: %v", err)
	}

	w := doJSON(t, router, "GET", "/api/library/media?library_id=1&tag_id="+strconv.FormatInt(tag.ID, 10), "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"].(float64) != 1 {
		t.Fatalf("期望 total=1, 实际 %v, body: %s", resp["total"], w.Body.String())
	}
}
