# 功能规格：移动端 PWA

> 状态：开发中　·　关联 PRD：FR-45　·　分支：feature/fr-45-pwa

## 1. 背景与目标

JianVideo 前端已具备基础移动端响应式（汉堡菜单 + 抽屉导航，见 FR-13/FR-14 修复）。
本功能（P2）将其升级为渐进式 Web 应用（PWA）：

- 让用户能把 JianVideo「添加到主屏」，以独立窗口（`display: standalone`）启动，体验接近原生应用。
- 提供离线应用壳：网络不可用时应用外壳（HTML/JS/CSS）仍可加载，避免白屏（媒体流本身不离线缓存）。
- 完善移动端响应式与触控细节，使小屏布局更顺手。

## 2. 需求（要什么）

- 范围内：
  - 引入 `vite-plugin-pwa`，构建产物包含 Web App Manifest（`manifest.webmanifest`）与 Service Worker（`sw.js`）。
  - Manifest 配置：应用名称（`name`/`short_name`）、图标（含 192/512 及 maskable）、主题色 `theme_color`、背景色 `background_color`、`display: standalone`、`start_url`。
  - 注册 Service Worker（`registerType: autoUpdate`），预缓存应用壳静态资源，实现离线壳加载。
  - 移动端触控/响应式细节完善：视口 `viewport-fit=cover`，PWA 主题色与状态栏 meta，触控目标尺寸友好。
- 不做（范围外）：
  - 不缓存媒体流（HLS/TS 分片、`/api/*` 动态数据）——它们体积大且需实时，运行时仍走网络。
  - 不做推送通知、后台同步等高级 PWA 能力。
  - 不做 `@vite-pwa/assets-generator` 自动多尺寸图标生成；图标手工提供以避免额外可选依赖。

## 3. 设计（怎么做）

涉及架构决策（引入 PWA 构建插件 + Service Worker 缓存策略），另写 [ADR-0028](../adr/0028-mobile-pwa.md)，此处不重复决策正文。

- **构建插件**：`frontend/vite.config.ts` 注册 `VitePWA`：
  - `registerType: 'autoUpdate'`：新版本就绪后自动接管，无需用户手动刷新。
  - `manifest`：填入中文名称、主题色、图标清单、`display: standalone`、`start_url: '/'`。
  - `workbox.navigateFallback: 'index.html'`：SPA 离线时导航回退到应用壳。
  - `workbox.globPatterns`：预缓存 `js/css/html/svg/png/ico` 等壳资源。
  - 仅缓存壳资源，`/api/` 与 `/assets/`（媒体）不进预缓存（默认 globPatterns 只覆盖构建产物，天然排除运行时 API）。
- **SW 注册**：在 `frontend/src/main.tsx` 调用 `vite-plugin-pwa` 虚拟模块 `virtual:pwa-register` 注册，失败静默（开发期不阻塞）。
- **图标资源**：`frontend/public/` 新增 `pwa-192x192.png`、`pwa-512x512.png`、`pwa-maskable-512x512.png`、`apple-touch-icon.png`，复用现有品牌紫色。
- **HTML meta**：`frontend/index.html` 补 `theme-color`、`apple-mobile-web-app-*`、`viewport-fit=cover`、中文 `lang="zh-CN"`、标题改 `JianVideo`。
- **后端**：`go:embed frontend/dist` 已整体内嵌 `dist/`，新增的 `manifest.webmanifest`、`sw.js`、图标随构建产物自动内嵌；Go 侧的 SPA 回退（`NoRoute` → `index.html`）已能正确服务这些根级文件，无需改后端。

## 4. 任务拆分

- [x] 创建规格文档 `docs/specs/mobile-pwa.md`
- [x] 引入 `vite-plugin-pwa`（`package.json` devDependency）
- [x] 配置 `vite.config.ts` 的 `VitePWA`（manifest + workbox 离线壳）
- [x] `main.tsx` 注册 Service Worker
- [x] 新增图标资源与 `index.html` meta
- [x] 测试：manifest 字段、SW 注册逻辑（单元/集成可覆盖部分）
- [x] 写 ADR-0028
- [x] 文档同步：PRD 状态、ARCHITECTURE、CHANGELOG

## 5. 验收标准

- 自动化可覆盖：
  - 构建产物 `frontend/dist/` 含 `manifest.webmanifest` 与 `sw.js`（构建后断言文件存在，且 manifest 含 `display: standalone`、名称、图标、主题色）。
  - SW 注册封装函数被正确调用（单元测试 mock `virtual:pwa-register`）。
  - `index.html` 含 `theme-color` 与 `apple-mobile-web-app-capable` meta。
- 待真机验（用户确认）：
  - 移动端浏览器可触发「添加到主屏」并以独立窗口启动（`standalone`）。
  - 断网后重新打开应用，应用壳（登录/布局）仍能加载不白屏。
  - 小屏（375px）布局顺手、触控目标可点。

## 6. 风险 / 待定

- `vite-plugin-pwa@1.x` 需 Vite 7/8，已选 `^1.3.0` 与本项目 Vite 8 兼容。
- Service Worker 仅在生产构建（`npm run build` 产物）启用，`vitest` 环境不实际注册，故 SW 注册以函数封装 + mock 验证为主。
- 「可安装 / 离线」属真机维度，自动化只能验证产物与注册逻辑，最终安装/离线体验需真机确认。
</content>
</invoke>
