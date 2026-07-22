package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wcpe/JianVideo/internal/netproxy"
	"github.com/wcpe/JianVideo/internal/settings"
)

// TestUpdateSettings_AppliesNetworkProxyToRuntime 保存 network_proxy 设置后，
// 应即时应用到全局出站代理（netproxy.ProxyFunc 返回新代理），保存即生效（FR-80）。
// 复用 settings_handler_test.go 的 setupSettingsRouter。
func TestUpdateSettings_AppliesNetworkProxyToRuntime(t *testing.T) {
	// 测试结束后清回直连，避免污染其他测试。
	t.Cleanup(func() { _ = netproxy.SetProxy("") })

	r := setupSettingsRouter(t)

	const proxy = "socks5h://127.0.0.1:1080"
	body := `{"settings":{"` + settings.KeyNetworkProxy + `":"` + proxy + `"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("保存设置应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	u, err := netproxy.ProxyFunc(&http.Request{})
	if err != nil {
		t.Fatalf("ProxyFunc 不应报错: %v", err)
	}
	if u == nil || u.Host != "127.0.0.1:1080" {
		t.Errorf("保存后出站代理应为 %q，实际 %v", proxy, u)
	}
}

// TestUpdateSettings_EmptyNetworkProxyMeansDirect 保存空 network_proxy 应清空代理走直连（FR-80）。
func TestUpdateSettings_EmptyNetworkProxyMeansDirect(t *testing.T) {
	t.Cleanup(func() { _ = netproxy.SetProxy("") })

	// 先把运行期代理置为已知值，再用空串保存以清空。
	if err := netproxy.SetProxy("http://127.0.0.1:7890"); err != nil {
		t.Fatalf("预置代理失败: %v", err)
	}

	r := setupSettingsRouter(t)
	// settings 不能为空，配合一个其他键，network_proxy 给空串。
	body := `{"settings":{"scan_interval":"60","` + settings.KeyNetworkProxy + `":""}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("保存设置应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	u, _ := netproxy.ProxyFunc(&http.Request{})
	if u != nil {
		t.Errorf("空 network_proxy 应清空走直连（nil），实际 %v", u)
	}
}

// TestUpdateSettings_InvalidNetworkProxyRejected 非法 network_proxy 保存前被拒绝（FR2-024）。
// 既有运行期代理保持不变。
func TestUpdateSettings_InvalidNetworkProxyKeepsExisting(t *testing.T) {
	t.Cleanup(func() { _ = netproxy.SetProxy("") })

	const existing = "http://127.0.0.1:7890"
	if err := netproxy.SetProxy(existing); err != nil {
		t.Fatalf("预置代理失败: %v", err)
	}

	r := setupSettingsRouter(t)
	body := `{"settings":{"` + settings.KeyNetworkProxy + `":"ftp://bad-scheme:21"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法代理应返回 400，实际 %d，body=%s", w.Code, w.Body.String())
	}
	u, _ := netproxy.ProxyFunc(&http.Request{})
	if u == nil || u.Host != "127.0.0.1:7890" {
		t.Errorf("非法 network_proxy 不应覆盖既有代理，期望 %q，实际 %v", existing, u)
	}
}

func TestApplyNetworkProxySettings_ReturnsRuntimeError(t *testing.T) {
	t.Cleanup(func() { _ = netproxy.SetProxy("") })

	err := applyNetworkProxySettings(map[string]string{settings.KeyNetworkProxy: "ftp://bad-scheme:21"})
	if err == nil {
		t.Fatalf("运行期应用非法代理应返回错误")
	}
}
