# 功能规格：后端出站网络代理设置

> 状态：开发中　·　关联 PRD：FR-80　·　分支：main

## 1. 背景与目标

FR-46 自更新真机验证暴露：本机直连 GitHub 下载 CDN 不可达，检测/下载失败。需要给后端的外部 HTTP 出站加一个运行期可配置的出站代理（HTTP/HTTPS/SOCKS5），让自更新等后端联网走代理穿透。属 P4 阶段。

当前后端外网出站只有 `update.Service`（GitHub Releases 检测 + 二进制下载），其 `client`/`downloadClient` 均无自定义 Transport、无代理。

## 2. 需求（要什么）

- 新增 settings 键 `network_proxy`（URL 字符串，空=直连），settings 持久化、保存即生效。
- 提供并发安全的全局代理 holder：校验并原子更新当前代理；对外暴露供 `http.Transport.Proxy` 使用的函数。
- 接线 `update.Service` 的检测 client 与下载 client，使两者经代理，且各自 Timeout 语义不变（检测 30s、下载无整体超时靠 context）。
- 前端「设置」页加「网络代理」输入框，复用 FR-63 热更新保存模式。
- 范围内：后端所有外部 HTTP 出站经该代理（当前唯一消费者为 update.Service）。
- 不做（范围外）：按域名分流/PAC、代理认证 UI 单列字段（认证走 URL userinfo 即可，由标准库支持）、前端自身（浏览器）代理。

## 3. 设计（怎么做）

- 新建 `internal/netproxy` 包（无业务依赖、被 update / api / main 依赖，依赖方向单向）：
  - 包级 `atomic.Pointer[url.URL]` 保存当前代理（nil=直连）。
  - `SetProxy(rawURL string) error`：空串清空（走直连）；非空 `url.Parse` 且 scheme ∈ {http,https,socks5,socks5h} 才接受、原子替换，非法返回错误且不覆盖当前值。
  - `ProxyFunc(*http.Request) (*url.URL, error)`：读当前代理，nil 则返回 (nil,nil)=直连。
  - `Raw() string`：返回当前代理原始串（供日志/测试）。
- settings：新增常量 `KeyNetworkProxy = "network_proxy"`。
- update：`NewService` 给 `client`/`downloadClient` 各设 `Transport: &http.Transport{Proxy: netproxy.ProxyFunc}`，Timeout 字段保持原样（client 30s、downloadClient 无）。
- api：`settings_handler.go` 新增 `applyNetworkProxySettings`，`PUT /api/settings` 含 `network_proxy` 键时落库后即时 `netproxy.SetProxy`；非法 URL 记 WARN 跳过应用（与现有空值守卫风格一致，落库不阻断）。
- main.go：启动期读 `network_proxy` 非空则 `SetProxy` 注入，非法记 WARN。
- 前端：`api/settings.ts` 加键、`SettingsPage.tsx` 加输入框（复用既有「保存设置」一并 PUT）。

socks5 支持：Go 标准库 `net/http.Transport.Proxy` 原生支持 `http`/`https`/`socks5`/`socks5h`（见 Transport.Proxy 文档），无需新依赖。

## 4. 任务拆分

- [ ] `internal/netproxy` 包 + 单测（空/http/https/socks5 解析、非法拒绝、并发安全）
- [ ] settings 常量 `KeyNetworkProxy`
- [ ] update.Service 两 client 接线 Transport（保留 Timeout）
- [ ] api handler 热更新 `applyNetworkProxySettings` + 测试
- [ ] main.go 启动注入
- [ ] 前端 SettingsPage 输入 + api/settings 键 + vitest
- [ ] 文档同步：PRD 状态、ARCHITECTURE §5.8、API、CHANGELOG

## 5. 验收标准

- 后端：`netproxy.SetProxy`/`ProxyFunc` 单测全绿（空→直连 nil、http/https/socks5 正确、非法拒绝且不覆盖、并发安全 `-race`）；handler 测试断言保存 `network_proxy` 后 `ProxyFunc` 返回新代理、空串清空。
- `go build ./...` + `go vet ./...` + 受影响包 `go test` 全绿。
- 前端：`SettingsPage` 渲染并保存网络代理输入 vitest 通过；`tsc --noEmit` + `vitest run` + `npm run build` 全绿。
- 真机维度（实际代理穿透 GitHub 下载）：本机未必有可用代理，标「待真机验」，需用户在有可用代理环境复验。

## 6. 风险 / 待定

- 无新依赖前提依赖「标准库 Transport 内置 socks5」——已据 `go doc net/http.Transport.Proxy` 确认（http/https/socks5/socks5h）。
- 代理 URL 含用户名密码时标准库以 Proxy-Authorization 头传递，不在日志打印明文（仅打印 scheme/host）。
