package netproxy

import (
	"net/http"
	"sync"
	"testing"
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
