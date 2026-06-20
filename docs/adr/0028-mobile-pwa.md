# ADR-0028: 移动端 PWA 与 Service Worker 缓存模型

## 状态
已接受

## 背景
FR-45（P2）要求把前端升级为渐进式 Web 应用（PWA），让用户在移动端「添加到主屏」、以独立窗口启动，并在断网时仍能加载应用壳。涉及两个约束：
- 前端经 `go:embed frontend/dist` 内嵌于 Go 二进制，Web 由 Go 统一承载；PWA 产物（manifest、Service Worker、图标）必须是 Vite 构建产物的一部分，才能被一并内嵌、被既有 SPA 回退正确服务。
- JianVideo 的媒体是大体积流式资源（HLS/TS 分片）且需实时性，**不适合离线缓存**；可离线的只有应用壳（HTML/JS/CSS）。

## 决策
- **引入 `vite-plugin-pwa`（构建期插件）生成 manifest + Service Worker**，注册策略 `autoUpdate`（新版本就绪后自动接管）。
- **仅离线缓存应用壳**：Workbox `globPatterns` 只预缓存构建产物（js/css/html/svg/png/ico/webmanifest）；`/api`、`/assets/`（媒体）、`/health` 列入 `navigateFallbackDenylist`，运行时一律走网络，不进离线缓存。
- **manifest 配置单一真源**：`frontend/src/utils/pwa-manifest.ts` 同时供 `vite.config.ts` 注入与单元测试断言，避免两处定义漂移。
- **Service Worker 注册封装** `frontend/src/utils/pwa.ts`（`registerPwa`）：把 `virtual:pwa-register` 的 `registerSW` 作为参数注入以便单测 mock，注册失败静默吞掉、不阻断应用启动。
- **后端不改**：新增的 manifest/sw/图标随 `dist/` 经现有 `go:embed` 内嵌，Go 侧 SPA 回退已能服务这些根级文件。

## 理由
- `vite-plugin-pwa` 是 Vite 生态的 PWA 标准方案，封装 Workbox，零后端改动即可产出 manifest + SW，契合「前端构建产物经 go:embed 内嵌」的既有形态。
- 只缓存壳、不缓存媒体，既实现离线壳（避免白屏），又不会因缓存过期的大体积流导致播放异常或占满存储，符合「简单优先」与媒体实时性。
- manifest 配置抽到独立模块，使「构建产物含正确 manifest」这一验收点可被单元测试覆盖，降低对真机的依赖。

## 后果
- 新增前端 devDependency `vite-plugin-pwa`（连带 Workbox），已获用户批准；`vite-plugin-pwa@1.x` 要求 Vite 7/8，与本项目 Vite 8 兼容。
- Service Worker 仅在生产构建产物中生效；`vitest`/开发环境不实际注册，故 SW 行为以「注册封装函数 + manifest 配置」单测覆盖，可安装/离线体验属真机维度。
- 应用更新经 SW `autoUpdate` 接管，用户下次进入自动获取新壳；首次安装后内容由 SW 提供，需注意发布新版后旧壳的过渡（Workbox 默认 `skipWaiting`/`clientsClaim` 行为）。

## 备选方案
- **手写 Service Worker + manifest（不引插件）**：可省一个依赖，但需手工维护预缓存清单与版本哈希，易漏易错，违背「简单优先」，不采用。
- **缓存媒体流以支持离线播放**：体积巨大、需失效策略且与追播实时性冲突，超出 FR-45 范围（镀金），不做。
- **保持纯响应式、不做 PWA**：无法满足 FR-45 的「添加到主屏 + 离线壳」验收，不采用。
