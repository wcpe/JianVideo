package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
	"jianvideo/internal/settings"
)

// setupSettingsRouter 构建带 settings 服务的测试路由。
func setupSettingsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	h := NewHandler(library.NewService(gdb)).WithSettings(settings.NewService(gdb))
	r := gin.New()
	RegisterRoutes(r, h)
	return r
}

// TestSettings_PutThenGet PUT 写入后 GET 能读回（持久化往返）。
func TestSettings_PutThenGet(t *testing.T) {
	router := setupSettingsRouter(t)

	body := `{"settings":{"scan_interval":"3600","recycle_bin_paths":"{\"D\":\"D:/.recycle\"}"}}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET 期望 200, 实际 %d", w.Code)
	}

	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v, body: %s", err, w.Body.String())
	}
	if resp.Settings["scan_interval"] != "3600" {
		t.Fatalf("期望 scan_interval=3600, 实际 %q", resp.Settings["scan_interval"])
	}
	if resp.Settings["recycle_bin_paths"] != `{"D":"D:/.recycle"}` {
		t.Fatalf("回收站路径不匹配, 实际 %q", resp.Settings["recycle_bin_paths"])
	}
}

// TestSettings_PutUpsert 同一 key 重复 PUT 覆盖旧值。
func TestSettings_PutUpsert(t *testing.T) {
	router := setupSettingsRouter(t)

	put := func(v string) {
		req := httptest.NewRequest("PUT", "/api/settings",
			bytes.NewBufferString(`{"settings":{"scan_interval":"`+v+`"}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s 期望 200, 实际 %d", v, w.Code)
		}
	}
	put("60")
	put("120")

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Settings["scan_interval"] != "120" {
		t.Fatalf("期望覆盖为 120, 实际 %q", resp.Settings["scan_interval"])
	}
}

// TestSettings_PutEmpty 空 settings 返回 400。
func TestSettings_PutEmpty(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空设置期望 400, 实际 %d", w.Code)
	}
}

// TestSettings_PutInvalidJSON 非法 JSON 返回 400。
func TestSettings_PutInvalidJSON(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 期望 400, 实际 %d", w.Code)
	}
}

// TestSettings_PutTriggersReload 保存成功后调用设置变更回调（定时扫描周期热生效，FR-28）。
func TestSettings_PutTriggersReload(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	reloaded := 0
	h := NewHandler(library.NewService(gdb)).
		WithSettings(settings.NewService(gdb)).
		WithSettingsReload(func() { reloaded++ })
	r := gin.New()
	RegisterRoutes(r, h)

	// 成功 PUT 应触发一次回调
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{"scan_interval":"600"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 期望 200, 实际 %d", w.Code)
	}
	if reloaded != 1 {
		t.Fatalf("成功保存应触发 1 次设置变更回调, 实际 %d", reloaded)
	}

	// 失败 PUT（空设置 400）不应触发回调
	req = httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空设置期望 400, 实际 %d", w.Code)
	}
	if reloaded != 1 {
		t.Fatalf("失败保存不应触发回调, 回调次数 %d", reloaded)
	}
}

// TestSettings_Unavailable 未注入 settings 服务时返回 503。
func TestSettings_Unavailable(t *testing.T) {
	h := NewHandler(library.NewService(nil))
	r := gin.New()
	RegisterRoutes(r, h)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用设置服务期望 503, 实际 %d", w.Code)
	}
}
