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

// setupAlbumRouter 创建带相册表的测试路由。
func setupAlbumRouter(t *testing.T) (*gin.Engine, *library.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Space{}, &models.MediaFile{}, &models.Album{}, &models.AlbumItem{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	for _, spaceID := range []string{models.DefaultSpaceID, "space-a", "space-b"} {
		if err := gdb.Create(&models.Space{ID: spaceID, Name: spaceID, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建测试 Space 失败: %v", err)
		}
	}
	svc := library.NewService(gdb)
	h := NewHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if spaceID := c.GetHeader("X-JianVideo-Space-Id"); spaceID != "" {
			c.Set("space_id", spaceID)
		}
		c.Next()
	})
	RegisterRoutes(r, h)
	return r, svc
}

func TestCreateAndListAlbums_API(t *testing.T) {
	router, _ := setupAlbumRouter(t)

	// 建相册 → 201
	body := `{"name":"旅行","description":"夏天"}`
	req := httptest.NewRequest("POST", "/api/albums", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("建相册期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	// 空名称 → 400
	req = httptest.NewRequest("POST", "/api/albums", bytes.NewBufferString(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空名称建相册期望 400, 实际 %d", w.Code)
	}

	// 列相册 → 200，含 1 条
	req = httptest.NewRequest("GET", "/api/albums", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列相册期望 200, 实际 %d", w.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, ok := resp["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("期望 1 个相册, 实际响应: %s", w.Body.String())
	}
}

func TestAlbumItems_API_CrossLibraryAndKeepMedia(t *testing.T) {
	router, svc := setupAlbumRouter(t)

	// 跨目录媒体
	m1, _ := svc.CreateMediaFile(1, "/lib1/a.mp4", 100)
	m2, _ := svc.CreateMediaFile(2, "/lib2/b.jpg", 200)
	album, _ := svc.CreateAlbum("合集", "")
	albumPath := "/api/albums/" + strconv.FormatInt(album.ID, 10)

	// 加成员 m1 → 204
	req := httptest.NewRequest("POST", albumPath+"/items",
		bytes.NewBufferString(`{"media_id":`+strconv.FormatInt(m1.ID, 10)+`}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("加成员期望 204, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	// 加成员 m2
	req = httptest.NewRequest("POST", albumPath+"/items",
		bytes.NewBufferString(`{"media_id":`+strconv.FormatInt(m2.ID, 10)+`}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("加成员 m2 期望 204, 实际 %d", w.Code)
	}

	// 加不存在的媒体 → 404
	req = httptest.NewRequest("POST", albumPath+"/items", bytes.NewBufferString(`{"media_id":9999}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("加不存在媒体期望 404, 实际 %d", w.Code)
	}

	// 列成员 → 200，2 条跨目录
	req = httptest.NewRequest("GET", albumPath+"/items", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列成员期望 200, 实际 %d", w.Code)
	}
	var listResp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if items, _ := listResp["items"].([]interface{}); len(items) != 2 {
		t.Fatalf("期望 2 个跨目录成员, body: %s", w.Body.String())
	}

	// 移出 m1 → 204
	req = httptest.NewRequest("DELETE", albumPath+"/items/"+strconv.FormatInt(m1.ID, 10), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("移出成员期望 204, 实际 %d", w.Code)
	}

	// 删相册 → 204
	req = httptest.NewRequest("DELETE", albumPath, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("删相册期望 204, 实际 %d", w.Code)
	}

	// 删相册后源媒体记录仍在
	if _, err := svc.GetMediaFileByID(m2.ID); err != nil {
		t.Fatalf("删相册后源媒体记录不应被删: %v", err)
	}
}

func TestCreateAlbum_InvalidJSON(t *testing.T) {
	router, _ := setupAlbumRouter(t)
	req := httptest.NewRequest("POST", "/api/albums", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 期望 400, 实际 %d", w.Code)
	}
}

func TestDeleteAlbum_NotFound_API(t *testing.T) {
	router, _ := setupAlbumRouter(t)
	req := httptest.NewRequest("DELETE", "/api/albums/9999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("删不存在相册期望 404, 实际 %d", w.Code)
	}
}

func TestAlbums_AreIsolatedBySpace(t *testing.T) {
	router, svc := setupAlbumRouter(t)
	mediaA, err := svc.CreateMediaFileInSpace("space-a", 1, "/a/a.mp4", 1)
	if err != nil {
		t.Fatalf("创建 Space A 媒体失败: %v", err)
	}
	mediaB, err := svc.CreateMediaFileInSpace("space-b", 2, "/b/b.mp4", 1)
	if err != nil {
		t.Fatalf("创建 Space B 媒体失败: %v", err)
	}

	create := func(spaceID, name string) models.Album {
		req := httptest.NewRequest(http.MethodPost, "/api/albums", bytes.NewBufferString(`{"name":"`+name+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-JianVideo-Space-Id", spaceID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("%s 创建相册失败: code=%d body=%s", spaceID, w.Code, w.Body.String())
		}
		var album models.Album
		_ = json.Unmarshal(w.Body.Bytes(), &album)
		return album
	}
	albumA := create("space-a", "A 相册")
	albumB := create("space-b", "B 相册")

	add := func(spaceID string, albumID, mediaID int64) int {
		req := httptest.NewRequest(http.MethodPost, "/api/albums/"+strconv.FormatInt(albumID, 10)+"/items", bytes.NewBufferString(`{"media_id":`+strconv.FormatInt(mediaID, 10)+`}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-JianVideo-Space-Id", spaceID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	if code := add("space-a", albumA.ID, mediaA.ID); code != http.StatusNoContent {
		t.Fatalf("Space A 加自身媒体应成功，实际 %d", code)
	}
	if code := add("space-a", albumA.ID, mediaB.ID); code != http.StatusNotFound {
		t.Fatalf("Space A 不得加入 Space B 媒体，实际 %d", code)
	}
	if code := add("space-b", albumA.ID, mediaB.ID); code != http.StatusNotFound {
		t.Fatalf("Space B 不得写 Space A 相册，实际 %d", code)
	}

	for spaceID, wantName := range map[string]string{"space-a": "A 相册", "space-b": "B 相册"} {
		req := httptest.NewRequest(http.MethodGet, "/api/albums", nil)
		req.Header.Set("X-JianVideo-Space-Id", spaceID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(wantName)) {
			t.Fatalf("%s 只能列举自身相册，code=%d body=%s", spaceID, w.Code, w.Body.String())
		}
		otherName := "A 相册"
		if spaceID == "space-a" {
			otherName = "B 相册"
		}
		if bytes.Contains(w.Body.Bytes(), []byte(otherName)) {
			t.Fatalf("%s 泄露其他 Space 相册: %s", spaceID, w.Body.String())
		}
	}
	_ = albumB
}
