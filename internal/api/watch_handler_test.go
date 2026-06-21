package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
)

// setupWatchRouter 创建带媒体表的测试路由与服务。
func setupWatchRouter(t *testing.T) (*gin.Engine, *library.Service) {
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

func TestUpdateWatchPosition_API(t *testing.T) {
	router, svc := setupWatchRouter(t)
	id := seedMedia(t, svc, 1, "a.mp4")

	w := doJSON(t, router, "PUT", "/api/play/"+strconv.FormatInt(id, 10)+"/position", `{"position":33.5}`)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["last_position"].(float64) != 33.5 {
		t.Fatalf("期望 last_position=33.5, 实际 %v", resp["last_position"])
	}
}

func TestUpdateWatchPosition_API_NotFound(t *testing.T) {
	router, _ := setupWatchRouter(t)
	w := doJSON(t, router, "PUT", "/api/play/999/position", `{"position":1}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

func TestMarkWatched_API(t *testing.T) {
	router, svc := setupWatchRouter(t)
	id := seedMedia(t, svc, 1, "a.mp4")
	// 先有进度
	if _, err := svc.UpdateWatchPosition(id, 20); err != nil {
		t.Fatalf("上报失败: %v", err)
	}

	w := doJSON(t, router, "PUT", "/api/play/"+strconv.FormatInt(id, 10)+"/watched", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["watched"] != true {
		t.Fatalf("期望 watched=true, 实际 %v", resp["watched"])
	}
	if resp["last_position"].(float64) != 0 {
		t.Fatalf("期望标记已看后 last_position=0, 实际 %v", resp["last_position"])
	}
}

func TestContinueWatching_API(t *testing.T) {
	router, svc := setupWatchRouter(t)
	idA := seedMedia(t, svc, 1, "a.mp4")
	idB := seedMedia(t, svc, 1, "b.mp4")
	if _, err := svc.UpdateWatchPosition(idA, 10); err != nil {
		t.Fatalf("上报 A 失败: %v", err)
	}
	if _, err := svc.UpdateWatchPosition(idB, 20); err != nil {
		t.Fatalf("上报 B 失败: %v", err)
	}
	// B 标记已看，不应出现
	if _, err := svc.MarkWatched(idB); err != nil {
		t.Fatalf("标记 B 已看失败: %v", err)
	}

	w := doJSON(t, router, "GET", "/api/library/continue-watching", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("期望 1 条继续观看, 实际响应: %s", w.Body.String())
	}
	first, _ := items[0].(map[string]any)
	if int64(first["id"].(float64)) != idA {
		t.Fatalf("期望继续观看含 A(id=%d), 实际响应: %s", idA, w.Body.String())
	}
}
