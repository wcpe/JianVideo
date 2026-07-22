package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// setupRecentlyViewedRouter 创建带媒体表的测试路由与服务。
func setupRecentlyViewedRouter(t *testing.T) (*gin.Engine, *library.Service) {
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

func TestMarkMediaViewed_API(t *testing.T) {
	router, svc := setupRecentlyViewedRouter(t)
	id := seedMedia(t, svc, 1, "a.jpg")

	w := doJSON(t, router, "PUT", "/api/library/media/"+strconv.FormatInt(id, 10)+"/viewed", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("期望响应 {\"ok\":true}, 实际 %s", w.Body.String())
	}

	// 记录后应能在最近查看列表取到
	mf, err := svc.GetMediaFileByID(id)
	if err != nil {
		t.Fatalf("读取媒体失败: %v", err)
	}
	if mf.LastViewedAt == nil {
		t.Fatalf("期望 last_viewed_at 已被置位")
	}
}

func TestMarkMediaViewed_NotFound(t *testing.T) {
	router, _ := setupRecentlyViewedRouter(t)
	w := doJSON(t, router, "PUT", "/api/library/media/999/viewed", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestMarkMediaViewed_InvalidID(t *testing.T) {
	router, _ := setupRecentlyViewedRouter(t)
	w := doJSON(t, router, "PUT", "/api/library/media/abc/viewed", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

func TestRecentlyViewed_API(t *testing.T) {
	router, svc := setupRecentlyViewedRouter(t)

	// 空库：items 为空
	w := doJSON(t, router, "GET", "/api/library/recently-viewed", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var empty map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &empty)
	if items, _ := empty["items"].([]any); len(items) != 0 {
		t.Fatalf("期望空库 items 为空, 实际 %s", w.Body.String())
	}

	// 标记一条已查看后应出现在列表
	id := seedMedia(t, svc, 1, "a.jpg")
	if err := svc.SetMediaViewed(id); err != nil {
		t.Fatalf("记录最近查看失败: %v", err)
	}
	w = doJSON(t, router, "GET", "/api/library/recently-viewed", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("期望 1 条最近查看, 实际响应: %s", w.Body.String())
	}
	first, _ := items[0].(map[string]any)
	if int64(first["id"].(float64)) != id {
		t.Fatalf("期望最近查看含 id=%d, 实际响应: %s", id, w.Body.String())
	}
}
