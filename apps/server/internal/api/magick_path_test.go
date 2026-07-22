package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
)

// TestUpdateSettings_AppliesMagickPathToRuntime 保存 magick_path 设置后，
// 应即时应用到 library 运行期全局路径（GetMagickPath 变更），保存即生效（FR-63）。
// 复用 settings_handler_test.go 的 setupSettingsRouter。
func TestUpdateSettings_AppliesMagickPathToRuntime(t *testing.T) {
	// 保存并在结束后恢复全局路径，避免污染其他测试
	savedMagick := library.GetMagickPath()
	t.Cleanup(func() {
		library.SetMagickPath(savedMagick)
	})

	r := setupSettingsRouter(t)

	const newMagick = "D:/custom/magick.exe"
	body := `{"settings":{"` + settings.KeyMagickPath + `":"` + newMagick + `"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("保存设置应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if got := library.GetMagickPath(); got != newMagick {
		t.Errorf("保存后 GetMagickPath 应为 %q，实际 %q", newMagick, got)
	}
}

// TestUpdateSettings_EmptyMagickPathKeepsExisting 保存空 magick_path 不应覆盖既有运行期路径（FR-63）。
// 空串保留自动发现/捆绑版结果，由 SetMagickPath 自身空值守卫保证。
func TestUpdateSettings_EmptyMagickPathKeepsExisting(t *testing.T) {
	savedMagick := library.GetMagickPath()
	t.Cleanup(func() {
		library.SetMagickPath(savedMagick)
	})

	// 先把运行期路径置为已知值，再用空串保存
	const existing = "D:/existing/magick.exe"
	library.SetMagickPath(existing)

	r := setupSettingsRouter(t)
	// settings 不能为空，配合一个其他键，magick_path 给空串
	body := `{"settings":{"scan_interval":"60","` + settings.KeyMagickPath + `":""}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("保存设置应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if got := library.GetMagickPath(); got != existing {
		t.Errorf("空 magick_path 不应覆盖既有路径，期望 %q，实际 %q", existing, got)
	}
}
