package netproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// reset 把全局代理清回直连，保证用例之间互不污染。
func reset(t *testing.T) {
	t.Helper()
	if err := SetProxy(""); err != nil {
		t.Fatalf("重置代理失败: %v", err)
	}
}

// TestSetProxy_EmptyMeansDirect 空串清空代理，ProxyFunc 返回 nil（直连）。
func TestSetProxy_EmptyMeansDirect(t *testing.T) {
	reset(t)

	// 先设一个代理，再用空串清空，验证确实回到直连。
	if err := SetProxy("http://127.0.0.1:7890"); err != nil {
		t.Fatalf("设置代理失败: %v", err)
	}
	if err := SetProxy("   "); err != nil {
		t.Fatalf("空白串清空应成功: %v", err)
	}
	u, err := ProxyFunc(&http.Request{})
	if err != nil {
		t.Fatalf("ProxyFunc 不应报错: %v", err)
	}
	if u != nil {
		t.Errorf("清空后应直连（nil），实际 %v", u)
	}
}

// TestSetProxy_ValidSchemes http/https/socks5/socks5h 均能被接受并由 ProxyFunc 正确返回。
func TestSetProxy_ValidSchemes(t *testing.T) {
	cases := []struct {
		raw  string
		host string
	}{
		{"http://127.0.0.1:7890", "127.0.0.1:7890"},
		{"https://proxy.example.com:443", "proxy.example.com:443"},
		{"socks5://127.0.0.1:1080", "127.0.0.1:1080"},
		{"socks5h://127.0.0.1:1080", "127.0.0.1:1080"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			reset(t)
			if err := SetProxy(c.raw); err != nil {
				t.Fatalf("合法代理 %q 应被接受: %v", c.raw, err)
			}
			u, err := ProxyFunc(&http.Request{})
			if err != nil {
				t.Fatalf("ProxyFunc 不应报错: %v", err)
			}
			if u == nil {
				t.Fatalf("设置 %q 后 ProxyFunc 不应返回 nil", c.raw)
			}
			if u.Host != c.host {
				t.Errorf("代理 host 期望 %q，实际 %q", c.host, u.Host)
			}
		})
	}
}

// TestSetProxy_InvalidRejectedAndUnchanged 非法 URL 被拒绝且不覆盖当前代理。
func TestSetProxy_InvalidRejectedAndUnchanged(t *testing.T) {
	reset(t)

	const good = "http://127.0.0.1:7890"
	if err := SetProxy(good); err != nil {
		t.Fatalf("先设置合法代理失败: %v", err)
	}

	invalids := []string{
		"ftp://127.0.0.1:21",      // 不支持的协议
		"socks4://127.0.0.1:1080", // 不支持的协议
		"://missing-scheme",       // 缺 scheme
		"http://",                 // 缺主机
		"ht tp://bad",             // 解析失败（含空格）
	}
	for _, bad := range invalids {
		if err := SetProxy(bad); err == nil {
			t.Errorf("非法代理 %q 应被拒绝", bad)
		}
		// 拒绝后当前值不应被改动，仍为先前合法代理。
		u, _ := ProxyFunc(&http.Request{})
		if u == nil || u.Host != "127.0.0.1:7890" {
			t.Errorf("非法 %q 被拒后当前代理应保持 %q，实际 %v", bad, good, u)
		}
	}
}

// TestProxyFunc_ReturnsCopy ProxyFunc 返回副本，调用方改动不污染内部存储。
func TestProxyFunc_ReturnsCopy(t *testing.T) {
	reset(t)
	if err := SetProxy("http://127.0.0.1:7890"); err != nil {
		t.Fatalf("设置代理失败: %v", err)
	}
	u1, _ := ProxyFunc(&http.Request{})
	u1.Host = "tampered:1"
	u2, _ := ProxyFunc(&http.Request{})
	if u2.Host != "127.0.0.1:7890" {
		t.Errorf("内部存储被外部改动污染，期望 127.0.0.1:7890，实际 %q", u2.Host)
	}
}

// TestSetProxy_Concurrent 并发读写无数据竞争（配合 -race）。
func TestSetProxy_Concurrent(t *testing.T) {
	reset(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = SetProxy("socks5://127.0.0.1:1080")
		}()
		go func() {
			defer wg.Done()
			_, _ = ProxyFunc(&http.Request{})
		}()
	}
	wg.Wait()
}

// TestRaw_RedactsCredentials Raw 不回显 userinfo 明文密码。
func TestRaw_RedactsCredentials(t *testing.T) {
	reset(t)
	if err := SetProxy("http://user:secret@127.0.0.1:7890"); err != nil {
		t.Fatalf("设置带凭据代理失败: %v", err)
	}
	got := Raw()
	if got == "" {
		t.Fatal("Raw 不应为空")
	}
	if contains(got, "secret") {
		t.Errorf("Raw 不应回显明文密码，实际 %q", got)
	}
}

// contains 简单子串判断，避免引入额外依赖。
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestTestProxy_EmptyMeansDirectReachable 空代理 = 直连，能连通本地 httptest 目标。
func TestTestProxy_EmptyMeansDirectReachable(t *testing.T) {
	reset(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	reachable, detail, latency := TestProxy(context.Background(), "", target.URL)
	if !reachable {
		t.Fatalf("直连本地目标应可达，detail=%q", detail)
	}
	if latency <= 0 {
		t.Errorf("耗时应为正值，实际 %v", latency)
	}
}

// TestTestProxy_ForwardsThroughProxy 合法代理转发命中：探测请求确实经过本地代理桩。
func TestTestProxy_ForwardsThroughProxy(t *testing.T) {
	reset(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// 本地 HTTP 代理桩：收到任何转发请求即计数并回 200，证明探测确经代理。
	var hit atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	reachable, detail, _ := TestProxy(context.Background(), proxy.URL, target.URL)
	if !reachable {
		t.Fatalf("经本地代理桩应可达，detail=%q", detail)
	}
	if hit.Load() == 0 {
		t.Error("探测请求未命中代理桩，说明未经代理转发")
	}
}

// TestTestProxy_InvalidURLReturnsError 非法代理 URL 判不可达且给出明确错误，不发起请求。
func TestTestProxy_InvalidURLReturnsError(t *testing.T) {
	reset(t)
	reachable, detail, latency := TestProxy(context.Background(), "ftp://127.0.0.1:21", "http://127.0.0.1:1")
	if reachable {
		t.Error("非法代理协议应判不可达")
	}
	if detail == "" {
		t.Error("非法代理应返回明确错误说明")
	}
	if latency != 0 {
		t.Errorf("非法 URL 不应发起请求，耗时应为 0，实际 %v", latency)
	}
}

// TestTestProxy_DoesNotPolluteCurrent 探测（无论合法 / 非法 / 直连）绝不改动运行期 current。
func TestTestProxy_DoesNotPolluteCurrent(t *testing.T) {
	reset(t)
	const baseline = "http://127.0.0.1:7890"
	if err := SetProxy(baseline); err != nil {
		t.Fatalf("设置基线代理失败: %v", err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// 用与基线不同的待测代理 + 直连 + 非法值分别探测，运行期 current 都不应被改动。
	_, _, _ = TestProxy(context.Background(), "http://127.0.0.1:9999", target.URL)
	_, _, _ = TestProxy(context.Background(), "", target.URL)
	_, _, _ = TestProxy(context.Background(), "ftp://bad", target.URL)

	u, _ := ProxyFunc(&http.Request{})
	if u == nil || u.Host != "127.0.0.1:7890" {
		t.Errorf("探测后运行期代理应保持基线 %q，实际 %v", baseline, u)
	}
}

// TestTestProxy_RedactsCredentials 含凭据的代理连不上时，错误说明不回显明文密码。
func TestTestProxy_RedactsCredentials(t *testing.T) {
	reset(t)
	// 指向一个必然连不上的地址，触发网络层错误，检查 detail 不含明文密码。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reachable, detail, _ := TestProxy(ctx, "http://user:supersecret@127.0.0.1:1/", "http://127.0.0.1:2/")
	if reachable {
		t.Error("连不上的代理应判不可达")
	}
	if contains(detail, "supersecret") {
		t.Errorf("错误说明不应回显明文密码，实际 %q", detail)
	}
}
