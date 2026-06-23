package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// setupOnThisDayRouter 创建带媒体表的测试路由与服务，并返回底层 gorm.DB 以便旁路写 media_time。
func setupOnThisDayRouter(t *testing.T) (*gin.Engine, *library.Service, *gorm.DB) {
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
	return r, svc, gdb
}

func TestOnThisDay_API(t *testing.T) {
	router, svc, gdb := setupOnThisDayRouter(t)
	now := time.Now()

	// 往年同一天，应命中
	idHit := seedMedia(t, svc, 1, "hit.jpg")
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", idHit).
		Update("media_time", now.AddDate(-2, 0, 0)).Error; err != nil {
		t.Fatalf("设置 media_time 失败: %v", err)
	}
	// 今年当天，不应命中
	idThisYear := seedMedia(t, svc, 1, "thisyear.jpg")
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", idThisYear).
		Update("media_time", now).Error; err != nil {
		t.Fatalf("设置 media_time 失败: %v", err)
	}

	w := doJSON(t, router, "GET", "/api/library/on-this-day", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("期望 1 条那年今日, 实际响应: %s", w.Body.String())
	}
	first, _ := items[0].(map[string]any)
	if int64(first["id"].(float64)) != idHit {
		t.Fatalf("期望那年今日含 id=%d, 实际响应: %s", idHit, w.Body.String())
	}
}
