package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// setupDebugLogRouter 构建带 settings 服务与调试日志应用回调的测试路由（FR-110）。
// applied 收集每次回调收到的开关值，供断言「保存即应用」。
func setupDebugLogRouter(t *testing.T, applied *[]bool) *gin.Engine {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	h := NewHandler(library.NewService(gdb)).
		WithSettings(settings.NewService(gdb)).
		WithDebugLogApply(func(on bool) { *applied = append(*applied, on) })
	r := gin.New()
	RegisterRoutes(r, h)
	return r
}

func putSettings(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestUpdateSettings_AppliesDebugLogOn 保存 debug_log=1 应即时调用切级别回调并传 true（FR-110）。
func TestUpdateSettings_AppliesDebugLogOn(t *testing.T) {
	var applied []bool
	r := setupDebugLogRouter(t, &applied)

	w := putSettings(t, r, `{"settings":{"`+settings.KeyDebugLog+`":"1"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if len(applied) != 1 || applied[0] != true {
		t.Fatalf("debug_log=1 应调用回调传 true，实际 %v", applied)
	}
}

// TestUpdateSettings_AppliesDebugLogOff 保存 debug_log=0 应即时调用切级别回调并传 false（FR-110）。
func TestUpdateSettings_AppliesDebugLogOff(t *testing.T) {
	var applied []bool
	r := setupDebugLogRouter(t, &applied)

	w := putSettings(t, r, `{"settings":{"`+settings.KeyDebugLog+`":"0"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if len(applied) != 1 || applied[0] != false {
		t.Fatalf("debug_log=0 应调用回调传 false，实际 %v", applied)
	}
}

// TestUpdateSettings_NoDebugLogKeyNoApply 未含 debug_log 键时不应调用切级别回调（FR-110）。
func TestUpdateSettings_NoDebugLogKeyNoApply(t *testing.T) {
	var applied []bool
	r := setupDebugLogRouter(t, &applied)

	w := putSettings(t, r, `{"settings":{"scan_interval":"60"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if len(applied) != 0 {
		t.Fatalf("未含 debug_log 不应调用回调，实际 %v", applied)
	}
}
