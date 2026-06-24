// Package netproxy 提供后端出站 HTTP 的全局可热更代理配置（FR-80）。
// 以并发安全的原子指针保存当前代理 URL（nil 表示直连），对外暴露：
//   - SetProxy：校验并原子更新当前代理（空串=清空走直连）。
//   - ProxyFunc：供 http.Transport.Proxy 使用，返回当前代理（无则直连）。
//   - TestProxy：用临时 client 探测待测代理对目标的连通性（FR-89），绝不改动运行期 current。
//
// 本包无业务依赖，可被 update / api / main 等单向依赖，不依赖任何业务模块。
// scheme 仅接受 http/https/socks5/socks5h——均为 Go 标准库 net/http Transport 原生支持，无需第三方依赖。
package netproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// current 当前出站代理，nil 表示直连。以原子指针保证读写并发安全且无锁读。
var current atomic.Pointer[url.URL]

// supportedSchemes 标准库 http.Transport.Proxy 原生支持的代理协议集合（不含点，小写）。
var supportedSchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

// parseProxy 校验并解析代理 URL。trimmed 为空时返回 (nil, nil) 表示直连。
// 非空时必须能被 url.Parse 解析、含受支持的 scheme 且有主机部分，否则返回错误。
// 错误信息中的地址用脱敏串，避免泄露 userinfo 凭据明文。
func parseProxy(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, nil
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("代理地址无法解析: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if !supportedSchemes[scheme] {
		return nil, fmt.Errorf("不支持的代理协议 %q（仅支持 http/https/socks5/socks5h）", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("代理地址缺少主机部分: %q", u.Redacted())
	}
	return u, nil
}

// SetProxy 校验并原子更新当前出站代理。
// rawURL 为空（去除首尾空白后）表示清空代理、走直连，恒成功。
// 非空时必须能被 url.Parse 解析、含受支持的 scheme 且有主机部分，否则返回错误且不改动当前值。
func SetProxy(rawURL string) error {
	u, err := parseProxy(rawURL)
	if err != nil {
		return err
	}
	// u 为 nil（直连）时清空，否则原子更新为新代理。
	current.Store(u)
	return nil
}

// ProxyFunc 供 http.Transport.Proxy 使用：返回当前出站代理。
// 当前无代理（直连）时返回 (nil, nil)，由标准库走直连。
func ProxyFunc(_ *http.Request) (*url.URL, error) {
	u := current.Load()
	if u == nil {
		return nil, nil
	}
	// 返回副本，避免调用方持有/改动内部存储的同一指针。
	clone := *u
	return &clone, nil
}

// Raw 返回当前代理的原始字符串（直连时为空串），供日志与测试使用；不回显 userinfo 凭据。
func Raw() string {
	u := current.Load()
	if u == nil {
		return ""
	}
	return u.Redacted()
}

// TestProxy 用临时 http.Client 探测待测代理对 target 的连通性（FR-89）。
//
//   - rawURL 为空（去空白后）= 测直连（不经任何代理）；非空则按待测代理转发。
//   - 全程使用临时 Transport / Client，绝不读写运行期全局 current，避免坏值污染出站。
//   - 探测仅发一次轻量 GET：只要 HTTP 层拿到任意响应（含 4xx）即视为「代理链路可达目标」，
//     仅在网络层错误（连不上代理 / 连不上目标 / 超时）时判不可达。
//   - reachable=false 时 detail 为脱敏后的原因；含 userinfo 凭据的代理地址一律脱敏，绝不回显明文。
//
// 返回 (是否可达, 脱敏说明, 本次探测耗时)。
func TestProxy(ctx context.Context, rawURL, target string) (bool, string, time.Duration) {
	proxyURL, err := parseProxy(rawURL)
	if err != nil {
		// 非法代理 URL：直接判不可达，错误信息已脱敏，不发起请求。
		return false, err.Error(), 0
	}

	// 临时 Transport：代理为 nil 时走直连；显式关闭长连接复用，探测即用即弃。
	transport := &http.Transport{DisableKeepAlives: true}
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, fmt.Sprintf("构造探测请求失败: %v", err), 0
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		// 网络层错误：脱敏后返回。url.Error 的 URL 字段是 target（无凭据），但代理拨号错误
		// 可能在消息里带上代理地址，故统一用 redactErr 兜底脱敏。
		return false, redactErr(err, proxyURL), latency
	}
	defer resp.Body.Close()
	// 拿到任意 HTTP 响应即说明代理链路连通到了目标。
	return true, fmt.Sprintf("HTTP %d", resp.StatusCode), latency
}

// redactErr 把错误信息中可能出现的代理凭据明文替换为脱敏串，避免日志 / 返回泄露密码。
func redactErr(err error, proxyURL *url.URL) string {
	msg := err.Error()
	if proxyURL == nil {
		return msg
	}
	// 代理含 userinfo 时，把可能内联到错误消息里的明文用户信息替换为脱敏后的 host。
	if user := proxyURL.User; user != nil {
		raw := proxyURL.String()
		msg = strings.ReplaceAll(msg, raw, proxyURL.Redacted())
		if pwd, ok := user.Password(); ok && pwd != "" {
			msg = strings.ReplaceAll(msg, pwd, "xxxxx")
		}
	}
	return msg
}
