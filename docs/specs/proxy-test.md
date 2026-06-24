# 功能规格：代理连通性测试（扩 FR-80）

> 状态：开发中　·　关联 PRD：FR-89　·　分支：feature/fr-89-proxy-test

## 1. 背景与目标
FR-80 已落地后端出站 HTTP 全局代理（`internal/netproxy`），但 `SetProxy` 只做 URL 语法校验、不做真实连通性探测；设置页「网络」分区也仅有一个代理输入框、无法在保存前验证代理是否真能连通。用户配错代理只能等自更新等出站请求失败才发现。本能力补齐「保存前先验」闭环，仿 FR-56 的 ffmpeg 路径检测交互。属于 P8 界面体验打磨与可用性增强。

## 2. 需求（要什么）
- 后端新增 `POST /api/system/proxy/test`，请求体 `{"proxy":"..."}`（proxy 可空 = 测当前运行期代理 / 直连）。
- 用**临时 `http.Client`**（`Transport.Proxy` 指向待测代理、带 ~8s ctx 超时）对默认目标 `https://api.github.com`（与 FR-46 自更新出站目标一致）做一次轻量 GET 探测，返回 `{reachable, detail, latency_ms}`。
- 代理 URL 可能含 userinfo 凭据，返回与日志一律脱敏（沿用 `netproxy` 的 `Redacted` 思路），绝不回显明文。
- 前端在设置页「网络」分区代理输入旁加「测试」按钮（loading 态）+ 成功 / 失败 Badge，仿 ffmpeg「检测」交互；`system.ts` 加 `testProxy` api（real/mock 双实现）。
- 范围内：一次轻量可达性探测（单个 GET）、URL 校验复用、脱敏。
- 不做（范围外）：真的下载产物 / 探测吞吐；不新增 GET 查询当前代理端点（输入框已承载）；不改 `netproxy.current` 运行期真源；不引入新依赖。

## 3. 设计（怎么做）
- `internal/netproxy`：新增纯函数 `TestProxy(ctx, rawURL, target string) (reachable bool, detail string, latency time.Duration)`：
  - 复用与 `SetProxy` 一致的校验逻辑（空串 = 直连；非空必须可解析、scheme 受支持、有 host），非法时 `reachable=false` 且 `detail` 为脱敏错误。
  - 构造**临时** `http.Transport{Proxy: ...}` + `http.Client`，对 `target` 发 GET；2xx-4xx 均视为「代理可达目标」（HTTP 层有响应即说明代理链路通），网络层错误视为不可达。
  - **绝不读写 `current`**，与 FR-80「非法不覆盖」「临时探测不污染运行期」语义一致。
  - 日志 / detail 中的代理地址用脱敏串（`url.URL.Redacted`）。
- `internal/api/system_handler.go`：`TestProxy` 处理器，默认目标常量 `defaultProxyTestTarget = "https://api.github.com"`，整体 ctx 超时 ~10s，返回 `{reachable, detail, latency_ms}`。
- `internal/api/router.go`：`sys.POST("/proxy/test", h.TestProxy)`。
- 前端：`frontend/src/types`：`ProxyTestResult`；`system.ts`：`testProxy(proxy?)` real/mock；`SettingsPage.tsx`：网络分区加测试按钮 + Badge + 状态。
- 无新 ADR（沿用 FR-80 已定的代理机制，仅扩展探测能力）。

## 4. 任务拆分
- [x] netproxy.TestProxy 纯函数 + 单测
- [x] system_handler.TestProxy 处理器 + 单测
- [x] 路由注册
- [x] 前端 types + system.ts testProxy（real/mock）
- [x] SettingsPage 网络分区测试按钮 + Badge + vitest
- [x] 文档同步：PRD 状态、API.md、CHANGELOG

## 5. 验收标准
- 后端：空 proxy = 直连可达（httptest 目标）；合法代理转发命中（本地代理桩）；非法 URL 返回明确错误且 `reachable=false`；探测前后 `netproxy.current` 不被污染；返回 / detail 对带凭据代理脱敏。
- 前端 vitest：点击「测试」按钮 loading → 成功 / 失败 Badge 渲染。
- 真机（可选加强，标「待真机验」）：配可用代理显示可达、配错误代理显示不可达。
- 完成判据：前端 `npm run build`+`npx vitest run` 全绿；后端 `go test ./internal/api/... ./internal/netproxy/...` 全绿、`gofmt -l` 无输出、`go vet` 干净。

## 6. 风险 / 待定
- 探测目标 `api.github.com` 在无外网 / CI 沙箱下会失败——单测一律用 httptest 本地目标 + 本地代理桩注入，不依赖真实外网；真机项另标。
