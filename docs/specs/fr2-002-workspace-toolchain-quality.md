# 功能规格：apps/packages 工作区、前端技术栈与最严质量门

> 状态：开发中　·　关联 PRD：FR2-002 / FR2-005 / FR2-015 / FR2-016　·　阶段：P0.5 `0.21.x`　·　分支：未指定

## 1. 背景与目标

P0 已明确 v2 是自托管视频素材与 AI 媒体中心。进入 P1 做 mockup、UI 博物馆和 PixiJS 原型前，必须先冻结项目架构、前端技术栈、mock 先行方式和静态检查门，避免边做边换工具链。

目标：

- 将所有可运行目标放在 `apps/*`。
- 共享能力放在 `packages/*`，供 web、wiki、mock-studio 和未来多端复用。
- 前端栈冻结为 pnpm/Turborepo/Vite/React Router/TanStack Query/Zustand/react-i18next/MSW/PixiJS。
- 建立 `apps/wiki` 作为 UI 博物馆和 mockup 入口。
- 冻结 Go、TypeScript、Rust、Kotlin、Swift 的最严静态检查门。

## 2. 目标目录

下面是**目标目录树**，并标注当前 v0.20 单体（`main.go` + `internal/*` + `frontend/`）迁入的落位；迁移方案与批次顺序见 [ADR-0060](../adr/0060-v2-migration-plan.md)。当前代码尚未迁移，`docs/ARCHITECTURE.md` 仍描述现状。

```text
apps/
  server/                  Go 一体化服务端（API、索引、任务队列、转码、AI 调度、静态资源）
    main.go                程序入口（现根目录 main.go 迁入）
    internal/              现 internal/* 18 个模块整体迁入为 server 内部包：
      api/  web/  library/  transcoder/  player/  playback/
      auth/  db/  settings/  share/  smb/  watcher/
      netproxy/  dblog/  metrics/  update/  config/
      space/               新增：Space 权限与多用户（ADR-0056）
      taskqueue/           新增：通用任务队列中心（ADR-0055）
      ai/                  新增：AI 管线调度与结果（ADR-0059）
    embed.go               go:embed 内嵌 apps/web 构建产物，仍为单二进制
  web/                     Web/PWA 主端（Vite + React Router，现 frontend/ 迁入）
    src/routes|pages|layout|features/   路由、页面、业务功能
  wiki/                    UI 博物馆与 mockup 站
  mock-studio/             Mock 场景与百万素材数据工作台
  desktop/                 桌面壳预留（复用 packages/player-core）
  android/  ios/  tv/  automotive/      多端预留
packages/
  ui/                      共享 UI 控件源码（现 frontend/src/components 抽出）
  render-pixi/             PixiJS 高密度网格/时间轴/纹理池（ADR-0053）
  player-core/             播放核心：逐帧/阶梯/HLS 自适应/预览/字幕音轨（ADR-0057）
  media-client/            API client、TanStack Query keys、Space 上下文、任务状态（现 frontend/src/api 抽出）
  theme/                   主题 token、密度、端能力适配（现 frontend/src/theme 抽出）
  i18n/                    react-i18next 资源与命名空间
  mock/                    MSW handlers、mock 数据、HLS/缩略图 mock
  benchmark/               前端渲染与后端索引 Benchmark 工具
  eslint-config/           共享 ESLint strict-type-checked 配置
  typescript-config/       共享 TypeScript 配置
```

约束：

- `apps/*` 只能放可运行应用、端壳、工具站。
- `packages/*` 放共享源码和可复用工具。
- `apps/web` 不直接引用 `apps/wiki`。
- UI 控件源码在 `packages/ui`；`apps/wiki` 展示这些控件，`apps/web` 使用同一套控件。
- PixiJS 热区源码在 `packages/render-pixi`；React 壳层通过明确 API 嵌入，不把 Pixi 内部状态散落到页面。
- 播放核心源码在 `packages/player-core`（ADR-0057），多端复用同一核心；Web 后端仍用 mpegts.js。
- `apps/server` 内部维持单向分层，`space` / `taskqueue` / `ai` 为新增内部包，落位与依赖见 ADR-0055 / 0056 / 0059。
- 迁移全程保持 go:embed 单二进制、SQLite 数据、REST API 与配置向后兼容（ADR-0060）。

## 3. 前端技术栈

| 能力 | 技术 | 用法边界 |
|---|---|---|
| 包管理 | pnpm workspace | 根目录统一 lockfile 与 workspace 包解析 |
| 任务编排 | Turborepo | 统一 lint/typecheck/test/build/dev/benchmark |
| 构建 | Vite | web、wiki、mock-studio 默认构建器 |
| 路由 | React Router | Web/PWA 主端路由 |
| 服务端状态 | TanStack Query | API 请求、缓存、失效、分页、任务轮询 |
| 客户端状态 | Zustand | UI 状态、选择态、布局态、Pixi 控制态桥接 |
| 国际化 | react-i18next | 多端文本资源、命名空间和语言检测 |
| Mock | MSW | mock 先行，浏览器 worker 与测试 server 同源 handlers |
| 高密度渲染 | PixiJS | 媒体网格、时间轴、预览纹理和滚动热区 |

## 4. Wiki / UI 博物馆

`apps/wiki` 是 P0.5 到 P1 的前端验收入口：

- 展示 `packages/ui` 全部控件、状态、密度、主题、空态、错误态和代码片段。
- 展示 `packages/render-pixi` 的媒体网格、时间轴、纹理池、100 万素材 mock 样例。
- 展示 HLS 预览卡、缩略图状态、任务队列状态、AI 审核状态和 Space 权限状态。
- 所有新 UI 控件先进入 wiki 预览，再被 `apps/web` 组合使用。
- wiki 负责文档和交互预览，不作为运行时依赖被主项目 import。

## 5. Mock 先行

- `packages/mock` 持有 MSW handlers、数据生成器和场景定义。
- `apps/wiki`、`apps/web`、`apps/mock-studio` 共享同一套 mock。
- Mock 必须覆盖缩略图、HLS 预览、转码任务、AI 任务、Space 权限、分页和错误状态。
- 100 万前端素材数据与 1000 万后端索引数据必须能通过 seed 重建。

## 6. 静态检查门

| 语言 / 端 | 门禁 |
|---|---|
| Go | `gofmt`、`goimports`、`go vet`、`staticcheck`、`govulncheck`、`golangci-lint` 严格规则集，至少覆盖 errcheck、bodyclose、sqlclosecheck、revive、gosec、gocritic、unused、ineffassign、misspell、unparam |
| TypeScript / React | ESLint flat config，`typescript-eslint` strict-type-checked，React Hooks，React，Turbo，import/边界规则，Prettier 检查，`tsc --noEmit` |
| Rust | `cargo fmt --check`，`cargo clippy --all-targets --all-features -- -D warnings -W clippy::pedantic` |
| Kotlin / Android | detekt 全规则基线，ktlint/format，Gradle lint |
| Swift / iOS | SwiftLint strict + 11 个配置模板，SwiftFormat，Xcode build/analyze |
| 多端通用 | 每个 app/package 必须暴露 `lint`、`typecheck`、`test`、`build` 或等价任务；Turbo 统一编排 |

说明：

- 当前仓库实际启用的语言先接入对应门禁。
- 预留端语言在端目录创建前只冻结标准，不创建无代码空配置。
- 后续新增端目录时，必须先接入对应静态检查和构建任务，再进入功能开发。

## 7. 验收标准

- PRD 和 ROADMAP 明确 P0.5 架构与工具链冻结门。
- ADR-0054 记录 apps/packages 工作区、前端技术栈和最严静态检查门。
- 后续实现 spec 可以直接按本文件迁移目录和工具链，不再重新讨论技术栈。
- 未实际迁移代码前，`docs/ARCHITECTURE.md` 仍保持当前真貌，不把目标架构写成已完成现状。

## 8. 风险 / 待定

- pnpm/Turborepo/PixiJS/TanStack Query/Zustand/react-i18next/MSW 都是依赖变更，实际引入前必须再次确认具体版本和迁移批次。
- SwiftLint “11 个配置模板”的具体模板名需要在 iOS 端实现规格中列出并入库。
- Kotlin detekt 全规则可能需要初始 baseline；baseline 不能长期掩盖新问题。
- Go 最严 lint 可能暴露大量存量问题，需分批清零，不能和业务重构混在一个提交。
