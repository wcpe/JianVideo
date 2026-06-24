// Package netproxy 提供后端出站 HTTP 的全局可热更代理配置（FR-80）。
// 以并发安全的原子指针保存当前代理 URL（nil 表示直连），对外暴露：
//   - SetProxy：校验并原子更新当前代理（空串=清空走直连）。
//   - ProxyFunc：供 http.Transport.Proxy 使用，返回当前代理（无则直连）。
//
// 本包无业务依赖，可被 update / api / main 等单向依赖，不依赖任何业务模块。
// scheme 仅接受 http/https/socks5/socks5h——均为 Go 标准库 net/http Transport 原生支持，无需第三方依赖。
package netproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
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

// SetProxy 校验并原子更新当前出站代理。
// rawURL 为空（去除首尾空白后）表示清空代理、走直连，恒成功。
// 非空时必须能被 url.Parse 解析、含受支持的 scheme 且有主机部分，否则返回错误且不改动当前值。
func SetProxy(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		current.Store(nil)
		return nil
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("代理地址无法解析: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if !supportedSchemes[scheme] {
		return fmt.Errorf("不支持的代理协议 %q（仅支持 http/https/socks5/socks5h）", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("代理地址缺少主机部分: %q", trimmed)
	}

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
