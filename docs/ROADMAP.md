# JianVideo v2 路线图

> 本路线图从 `0.21.0` 开始生效。`0.20.x` 之前的旧分期已废弃，历史内容仅保留在 [`docs/archive/PRD-v0.20-history.md`](archive/PRD-v0.20-history.md) 中查询。

## 1. 版本线规则

JianVideo v2 从 `0.21.0` 开始重新进入规格化路线。版本号始终使用三段式：

```text
MAJOR.MINOR.PATCH
```

阶段与 `MINOR` 绑定，同一条版本线内的所有 patch 都属于同一阶段。例如：

- `0.21.0`、`0.21.1`、`0.21.2` 都属于 P0。
- `0.22.0`、`0.22.1`、`0.22.2` 都属于 P1。
- `PATCH` 不限定范围，按修复、验收和小步收口需要递增。

`1.0.0` 之前都属于 `0.y.z` 预稳定阶段。三段版本号不是从 `1.0.0` 后才开始，而是从当前开始就使用。

**发布渠道（见 [ADR-0065](adr/0065-pre-1-0-formal-only-release-channels.md)）**：

- **`1.0.0` 之前**：所有公开版本一律为**正式版**（`v0.Y.Z`），走稳定 tag / `release.yml`；**不**推 `v0.Y.Z-rc.N`，也**不**在对外文案中把某次 `0.x` 称为 RC 或 GA。
- **自 `1.0.0` 起**：才区分 RC 与 GA——先 `v1.0.0-rc.N`（候选），验收通过后 `v1.0.0`（稳定）；之后严格按 SemVer：`1.0.1` 兼容修复、`1.1.0` 兼容新增、`2.0.0` 破坏性变更；预发布形如 `1.0.0-rc.1`、`1.1.0-rc.1`。

## 2. 新分期表

| 阶段 | 版本线 | 阶段目标 | 进入条件 | 退出条件 |
|---|---|---|---|---|
| P0 | `0.21.x` | 规格冻结与技术基线 | 接受旧版混乱状态，不再继续按旧分期追加需求 | v2 PRD、ROADMAP、ADR、P0 specs 完成；视频优先、自托管、Space、AI、PixiJS、索引和数据可信源边界明确 |
| P0.5 | `0.21.x` | 架构与工具链冻结门 | P0 产品和技术基线已确认 | apps/packages 工作区、pnpm/Turborepo、前端栈、wiki UI 博物馆、mock 先行和跨语言最严静态检查门冻结 |
| P1 | `0.22.x` | Mockup、UI 博物馆与 PixiJS 原型实现 | P0.5 架构和工具链冻结 | 可交互 mockup、UI 博物馆、100 万素材 PixiJS 原型、HLS 预览样例、前后端 Benchmark harness 可用 |
| P2 | `0.23.x` | 存储库、索引与转码队列 | P1 原型和性能基建可用 | 存储库扫描、多媒体库分型、Space 归属、关键数据库索引、多码率转码、视频 HLS 预览、任务队列、审计日志和 1000 万资产后端压测基线可用 |
| P3 | `0.24.x` | 播放体验（王牌） | P2 存储库、转码与队列基线稳定 | 逐帧/阶梯步进、HLS 自适应、进度条预览、字幕音轨、变速、续播、章节书签在 Web 达标，播放核心封装为 `packages/player-core` |
| P4 | `0.25.x` | 高密度媒体浏览器 | P3 播放核心稳定 | PixiJS 网格/时间轴、搜索筛选、播放列表、元数据展示、图片编辑与视频粗剪导出、批量操作和 100 万素材滚动验收通过 |
| P5 | `0.26.x` | Space、安全与多用户 | P4 浏览与编辑主体验稳定 | 10 到 50 用户、Space 权限、分享增强、删除恢复与回收站、审计回滚、家长控制、安全基线和写回边界可用 |
| P6 | `0.27.x` | AI 索引、搜索与审核 | Space 边界与任务队列稳定 | 人脸、OCR、对象/场景、视频理解、向量语义搜索、AI 去重、审核流和重建策略可用（默认关闭需配置） |
| P7 | `0.28.x` | 多端与交付质量门 | Web 主体验和 AI 管线稳定 | 多端 player-core 复用、投屏、部署与安装引导、错误上报与埋点、备份恢复、通知、i18n/a11y、E2E、Benchmark、发布包和一体化部署质量门收口 |
| P8 | `0.29.x` | 1.0 候选准备 | 主要功能线冻结 | 只接受阻断修复、兼容修复、数据安全修复、性能回归修复、文档与验收补强；本阶段 patch 仍为**正式版** `0.29.x`，并准备首次公开 RC `1.0.0-rc.1` |
| GA | `1.0.0` | 首个稳定正式版 | `1.0.0-rc.N` 验收通过 | 正式进入严格 SemVer；此后才长期使用 RC→GA 渠道 |

## 3. 通用质量门

每个阶段退出前都必须满足以下门槛：

- PRD 中相关 FR2 状态可信，已交付能力标注交付版本，未完成能力不能写成已交付。
- 非平凡功能有 `docs/specs/` 规格，架构决策有 ADR，代码实际结构变化后同步 `ARCHITECTURE.md`。
- 用户可见变更进入 `CHANGELOG.md`，发版时再由发布流程切正式版本段。
- 涉及核心浏览、缩略图、HLS 预览、搜索、扫描、播放、转码或 AI 队列的改动必须有自动化测试。
- 涉及 PixiJS 滚动热区、大库查询、后端索引、向量索引或任务队列的改动必须附 Benchmark 结果。
- 所有 app/package 必须有对应质量门任务；当前语言栈用最严可执行门，未来端语言启用前必须先补门禁。
- 目录迁移、依赖升级、构建/发布变化不得破坏现有 SQLite 数据、配置、API、go:embed 单二进制部署和历史媒体库。
- 删除、迁移、写回、同步必须有原文件安全说明和事件日志策略。

## 4. P0：`0.21.x` 规格冻结与技术基线

目标：

- 废弃旧分期，阻止继续把周任务写进 PRD。
- 将 v2 定位调整为自托管视频素材与 AI 媒体中心。
- 明确 PixiJS 是核心高密度渲染方案，首个 mock UI 即测试 100 万素材。
- 明确存储库扫描、数据库索引、HLS 预览、转码队列和 AI 管线的优先级。
- 明确 Space 是权限、删除、同步、AI 可见性、分享、审计和写回的策略边界。
- 明确可信源与可重建数据边界。
- 明确 SQLite 大库上限、v0.20 数据迁移、备份恢复、i18n/a11y 和匿名分享防滥用等 1.0 风险。

交付物：

- [`docs/PRD.md`](PRD.md)
- [`docs/ROADMAP.md`](ROADMAP.md)
- [`docs/adr/0053-video-ai-pixijs-space-baseline.md`](adr/0053-video-ai-pixijs-space-baseline.md)
- [`docs/adr/0054-apps-workspace-toolchain-quality-gates.md`](adr/0054-apps-workspace-toolchain-quality-gates.md)
- [`docs/specs/v0.21-v2-restructure.md`](specs/v0.21-v2-restructure.md)
- [`docs/specs/fr2-002-workspace-toolchain-quality.md`](specs/fr2-002-workspace-toolchain-quality.md)
- [`docs/specs/fr2-003-performance-budget.md`](specs/fr2-003-performance-budget.md)
- [`docs/specs/fr2-004-compatibility-matrix.md`](specs/fr2-004-compatibility-matrix.md)
- [`docs/specs/fr2-005-wiki-ui-museum-mockup.md`](specs/fr2-005-wiki-ui-museum-mockup.md)

## 5. P0.5：`0.21.x` 架构与工具链冻结门

目标：

- 冻结 apps/packages 工作区结构：所有可运行端放 `apps/*`，共享能力放 `packages/*`。
- 前端栈冻结为 pnpm、Turborepo、Vite、React Router、TanStack Query、Zustand、react-i18next、MSW、PixiJS。
- `apps/wiki` 作为 UI 博物馆和 mockup 入口，展示全部 UI 控件、PixiJS 样例、HLS 预览卡、任务队列状态和代码片段。
- UI 控件源码放 `packages/ui`，`apps/web` 与 `apps/wiki` 引用同一套组件，禁止 app-to-app 依赖。
- 静态检查冻结为最严档：Go 严格 lint/style/security、TypeScript strict-type-checked、Rust clippy pedantic、Kotlin detekt 全规则、SwiftLint strict + 11 个配置模板。

退出口径：

- `docs/specs/fr2-002-workspace-toolchain-quality.md` 明确目录结构、技术栈、wiki/UI 博物馆、mock 先行和静态检查门。
- 进入 P1 前仍不要求实际移动代码，但后续任何目录迁移或依赖引入必须按该规格执行。

## 6. P1：`0.22.x` Mockup、UI 博物馆与 PixiJS 原型实现

目标：

- 先做 mockup 与 UI 博物馆，再拼正式页面。
- UI 博物馆展示 React 壳层组件、主题 token、PixiJS 渲染样例、HLS 预览卡、任务队列状态和使用代码片段。
- PixiJS 原型必须覆盖 100 万素材网格/时间轴滚动、纹理池、缩略图占位、视频预览入口和拖拽定位。
- Benchmark harness 同时覆盖前端渲染和后端索引，不只测静态 UI。

退出口径：

- 100 万素材 mock UI 能在本地跑出帧耗时、长任务、内存、纹理数量和缩略图/HLS 预览请求报告。
- 后端能生成 100 万、500 万、1000 万资产级 mock 索引数据，并跑出关键查询与分页报告。
- UI 博物馆能独立预览组件和 PixiJS 样例。

规格：[fr2-005](specs/fr2-005-wiki-ui-museum-mockup.md)（UI 博物馆/mockup）、[fr2-006](specs/fr2-006-api-client-multiend.md)（API client 与多端基础）、[fr2-063](specs/fr2-063-pixijs-prototype-benchmark.md)（PixiJS 原型与前后端 Benchmark harness）、[fr2-003](specs/fr2-003-performance-budget.md)（性能预算口径）。

## 7. P2：`0.23.x` 存储库、索引与转码队列

目标：

- 存储库扫描取代 Web 上传作为主入口。
- 每个媒体资产归属 Space，并进入索引、任务、审计和 AI 可见性模型。
- 支持多媒体库分型（电影/剧集/家庭录像库），各自扫描、解析与命名规则。
- 建立关键数据库索引：Space + 媒体类型 + 时间 + 路径 + 状态 + 任务状态 + AI 结果状态。
- 建立多码率转码、视频 HLS 预览、通用任务队列、优先级、失败重试、任务监控和代理文件重建策略。
- 建立文件自带元数据解析、本地离线影视推断、分档缩略图与智能封面、文件级去重和存储/缓存管理。
- 建立全操作审计日志（事件真源）与外部工具（ffmpeg 等）自动下载/代理配置。
- 建立 v0.20 到 v2 的数据迁移与索引重建演练路径。

退出口径：

- 1000 万资产 mock 下关键查询、分页、筛选、入队和任务状态查询有 Benchmark。
- 多码率转码、HLS 预览和任务队列在 mock/测试素材下可运行。
- 原文件安全、代理文件可重建、审计事件日志边界清楚。

## 8. P3：`0.24.x` 播放体验（王牌）

目标：

- 逐帧前后步进与阶梯快进快退（1帧/0.5s/1s/5s/30s/1m），前后对称、帧准确、不触发 Network Error。
- HLS 多码率自适应播放；能直连的原文件优先直连；弱网平滑降档。
- 进度条悬停预览、字幕与音轨切换、变速与 A-B 循环、章节/书签、画中画与移动手势。
- 跨端续播与观看历史。
- 播放核心逻辑封装为 `packages/player-core`，供 P7 多端复用。

退出口径：

- 逐帧、阶梯、自适应、进度预览在 Web/PWA 达标；player-core 可被其它端复用。
- 追播、并发 Seek、弱网降档等高风险路径有自动化测试。
- Desktop/Android/iOS 的播放达标在 P7 多端阶段用同一 player-core 收口。

## 9. P4：`0.25.x` 高密度媒体浏览器

目标：

- 用 PixiJS 实现视频素材浏览主体验：网格、时间轴、筛选、预览、拖拽定位。
- React 只承载壳层、表单、浮层和工具面板，不进入核心滚动热区。
- 全局搜索与多维筛选排序、播放列表/合集与剧集连播、元数据与标签展示、批量操作。
- 图片高级编辑器（系统内覆盖）与视频片段粗剪导出，均走任务队列、不改原文件。
- 缩略图、HLS 预览、纹理池、可见窗口和过期请求取消策略稳定。

最低性能口径：

- 100 万媒体 mock：连续滚动必须输出可解释的帧耗时、长任务、纹理池、内存和请求报告。
- 500 万到 1000 万资产：前端不要求全量驻留，只能通过窗口化查询和增量加载访问。
- 拖动定位、搜索、筛选和时间跳转必须和后端分页/索引协同，不能靠前端全量数组过滤。

## 10. P5：`0.26.x` Space、安全与多用户

目标：

- 支持 10 到 50 用户。
- Space 管权限、删除、同步、AI 可见性、分享、审计和写回。
- 删除、迁移、写回、同步必须可追溯、可恢复或显式提示风险；提供操作可回滚中心。
- 分享增强：密码/有效期/禁下载，默认只读且按 Space 范围校验。
- 匿名分享不得触发高成本转码、AI 推理或批量重建任务。
- 回收站保留期与自动清理、家长控制/内容分级、安全基线（HTTPS/反代/防爆破/会话设备管理）。

## 11. P6：`0.27.x` AI 索引、搜索与审核

目标：

- 人脸、OCR、对象/场景、视频理解、向量语义搜索、AI 去重和审核流进入主线。
- 模型、索引、推理节点和重建策略可替换、可审计；未配置模型与向量库时 AI 默认关闭。
- AI 中间结果是可重建数据，人工元数据和事件日志优先。

## 12. P7：`0.28.x` 多端与交付质量门

目标：

- Web、Desktop、Android、iOS、Android TV、安卓车机共用同一后端、API client、权限模型和 player-core。
- TV/车机优先浏览、预览和播放，复杂管理留给 Web/Desktop。
- Docker Compose 部署与首次安装引导、功能首用提示、设置拆分（系统状态/系统设置）。
- 多端错误日志自动上报与展示中心、应用内埋点、通知中心、投屏/Cast。
- 可信元数据备份/恢复、i18n 文本资源和 Web 主端无障碍质量门。
- 一体化部署、版本、更新、资源嵌入、E2E、Benchmark 和发布包质量门稳定。

## 13. P8：`0.29.x`（1.0 候选准备，正式版阶段线）

目标：

- 冻结大功能，只接受阻断修复、兼容修复、数据安全修复、性能回归修复和验收补强。
- 阶段内发布仍为正式版 `0.29.x`（不发 `0.29.x-rc.N`）。
- 准备并推送首次公开 RC：`1.0.0-rc.1`（自 `1.0.0` 起才启用 RC 渠道，见 ADR-0065）。
- `1.0.0-rc.N` 验收中发现的阻断问题可回 `0.29.x` 正式 patch 修复，再递增 `1.0.0-rc.N`，直到满足 GA 条件。

## 14. GA：`1.0.0`

目标：

- 在 final RC 通过后发布首个稳定正式版 `1.0.0`。
- 之后严格采用 SemVer，并可继续对后续 minor/major 使用 RC→GA：
  - patch：兼容修复。
  - minor：兼容功能新增。
  - major：破坏性变更。
