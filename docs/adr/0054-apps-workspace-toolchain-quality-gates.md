# ADR-0054：采用 apps/packages 工作区、前端技术栈与最严静态检查门

## 状态
已接受

## 背景

用户要求在 P0 和 P1 之间先完成项目架构与工具链冻结：

- 所有可运行目标都放在 `apps/*` 下。
- 使用 pnpm workspace 与 Turborepo 管理前端/工具链任务。
- 前端栈重选为 Vite、React Router、TanStack Query、Zustand、react-i18next、MSW。
- 增加 wiki 子项目作为 UI 博物馆，所有 UI 控件先在博物馆展示和 mock，再给主项目引用。
- 静态检查维持最严档，覆盖 Go、Rust、TypeScript、Kotlin、Swift 等多端语言。

ADR-0052 已确立 apps/packages 方向，但未冻结具体前端栈、wiki 形态和跨语言质量门，因此由本 ADR 取代。

## 决策

从 `v0.21.x` 的 P0.5 架构与工具链冻结门开始，目标仓库结构调整为：

```text
apps/
  server/        Go 一体化服务端，承载 API、索引、任务队列、转码、AI 调度与静态资源服务
  web/           Web/PWA 主端，Vite + React Router
  wiki/          UI 博物馆与 mockup 站，展示全部 UI 控件、PixiJS 样例、HLS 预览卡和代码片段
  mock-studio/   Mock 场景、百万素材数据集和交互脚本工作台
  desktop/       桌面壳预留
  android/       Android 端预留
  ios/           iOS 端预留
  tv/            Android TV 端预留
  automotive/    安卓车机端预留
packages/
  ui/            共享 UI 控件源码；web/wiki/端壳从这里引用
  render-pixi/   PixiJS 高密度媒体网格、时间轴、纹理池和交互层
  media-client/  API client、TanStack Query keys、Space 上下文、任务状态类型
  theme/         主题 token、密度、颜色、端能力适配
  i18n/          react-i18next 资源、命名空间和语言检测策略
  mock/          MSW handlers、mock 数据生成、HLS/缩略图 mock
  benchmark/     前端渲染与后端索引 Benchmark 工具
  eslint-config/ 共享 ESLint strict-type-checked 配置
  typescript-config/共享 TypeScript 配置
```

前端技术栈冻结为：

| 能力 | 技术 |
|---|---|
| 包管理 | pnpm workspace |
| 任务编排 | Turborepo |
| Web 构建 | Vite |
| 路由 | React Router |
| 服务端状态 | TanStack Query |
| 客户端状态 | Zustand |
| 国际化 | react-i18next |
| Mock | MSW，mock 先行 |
| 高密度渲染 | PixiJS |
| UI 博物馆 | `apps/wiki` + `packages/ui` |

UI 控件源码放在 `packages/ui`，`apps/wiki` 是组件治理和预览入口，`apps/web` 引用同一套 `packages/ui`。不让 `apps/web` 直接依赖 `apps/wiki`，避免 app-to-app 循环依赖。

静态检查门冻结为：

| 语言 / 端 | 最严检查门 |
|---|---|
| Go | `gofmt`、`goimports`、`go vet`、`staticcheck`、`govulncheck`、`golangci-lint` 严格规则集（含 errcheck、bodyclose、sqlclosecheck、revive、gosec、gocritic、unused、ineffassign、misspell、unparam） |
| TypeScript / React | ESLint flat config + `typescript-eslint` strict-type-checked、React Hooks、React、Turbo、import/边界规则、Prettier 检查、`tsc --noEmit` |
| Rust | `cargo fmt --check`、`cargo clippy --all-targets --all-features -- -D warnings -W clippy::pedantic` |
| Kotlin / Android | detekt 全规则基线、ktlint/format 检查、Gradle lint |
| Swift / iOS | SwiftLint strict + 11 个配置模板、SwiftFormat、Xcode build/analyze |
| 多端通用 | 每个 app/package 必须暴露 `lint`、`typecheck`、`test`、`build` 或等价任务，Turbo 统一编排 |

## 理由

- `apps/*` + `packages/*` 结构能让多端应用、wiki、mock-studio 和共享库分清边界。
- pnpm/Turborepo 适合多 app、多 package 的任务编排，且能把 lint/typecheck/build/test 作为统一质量门。
- Vite + React Router 更贴近当前应用形态，比切到 Next.js 风险小。
- TanStack Query 与 Zustand 能明确服务端状态和客户端 UI 状态边界，避免页面内散落请求和全局状态混用。
- react-i18next 适合从 P0.5 开始建立多端文本资源边界。
- MSW mock 先行能让 UI 博物馆、mockup 和 Benchmark 在后端实现前先跑起来。
- UI 控件源码放 `packages/ui`，wiki 只负责展示和治理，可以避免 `apps/web` 反向依赖另一个 app。

## 后果

- `docs/PRD.md` 与 `docs/ROADMAP.md` 增加 P0.5 架构与工具链冻结门，仍归属 `0.21.x`，不新增新的 minor 版本线。
- 后续实际移动目录、引入 pnpm/Turbo/PixiJS/TanStack Query/Zustand/react-i18next/MSW 或改工具链配置，必须单独走实现 spec，并按依赖管理要求确认。
- `apps/wiki` 成为 UI 控件、mockup、PixiJS 样例、HLS 预览卡和代码片段的验收入口。
- `apps/web` 不直接 import `apps/wiki`；两者通过 `packages/ui`、`packages/theme`、`packages/render-pixi`、`packages/media-client` 共享能力。
- 当前 `docs/ARCHITECTURE.md` 在代码迁移完成前仍描述现状；目标结构以本 ADR、PRD 和 specs 为准。

## 备选方案

- **继续只保留 `frontend/` 单应用**：短期省事，但无法承载多端、wiki、mock-studio 和共享质量门。
- **把 UI 控件源码放在 `apps/wiki` 让主项目直接引用**：贴近字面要求，但会形成 app-to-app 依赖，后续构建和部署边界混乱。
- **直接采用 Next.js/Turbo 全套**：与完整多应用模板更接近，但当前主端仍适合 Vite，切 Next.js 会同时改变路由、构建和部署。
- **只做前端静态检查，不做跨语言质量门**：短期简单，但不符合多端路线和“最严档”要求。
