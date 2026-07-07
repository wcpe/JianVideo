# 功能规格：PixiJS 高密度渲染原型与前后端 Benchmark harness

> 状态：已实现　·　关联 PRD：FR2-063　·　阶段：P1 `0.22.x`　·　分支：未指定

## 1. 背景与目标

进入 P1 后，v2 要求在写正式高密度浏览器之前，先用**可复跑的证据**验证 P0 定下的性能预算，而不是等页面拼完再补性能。[ADR-0053](../adr/0053-video-ai-pixijs-space-baseline.md) 已把 PixiJS 定为 100 万级媒体网格/时间轴滚动热区的默认渲染路径，[ADR-0054](../adr/0054-apps-workspace-toolchain-quality-gates.md) 已冻结 `apps/*` + `packages/*` 工作区与 `packages/render-pixi`、`packages/benchmark`、`packages/mock`、`apps/mock-studio` 的落位，[fr2-003](fr2-003-performance-budget.md) 已给出前端帧耗时/内存与后端查询延迟的具体口径与数据集分档。本规格把这些决策落成一个 P1 核心工程原型。

目标：

- 在 `packages/render-pixi` 交付 100 万素材的 PixiJS 网格/时间轴原型，验证 [ADR-0053] 的 PixiJS-first 热区边界。
- 交付前端 Benchmark harness，连续滚动输出 [fr2-003] 规定的前端指标，可解释、可复跑。
- 交付后端 100 万/500 万/1000 万 mock 索引数据生成与查询 Benchmark，对照 [fr2-003] 后端门出报告。
- 交付 HLS 预览样例，验证视频预览与滚动解耦。
- 全程 mock 先行（MSW / seed 数据），不依赖真实存储库、真实转码或真实多端。

## 2. 需求（要什么）

范围内：

- **render-pixi 100 万素材原型**：网格视图 + 时间轴视图，纹理池、窗口化/可见窗口（可视区 + overscan）、缩略图占位、快速滚动时过期缩略图请求可取消或丢弃结果。对齐 [ADR-0053] 热区边界，React 只做壳层，Pixi 状态与 React 控制态分层，滚动时 React 壳层不随每帧重渲染。
- **前端 Benchmark harness**：驱动连续滚动脚本，输出 [fr2-003] §3/§6 规定的前端指标——帧耗时 p95/p99、10s 滚动窗口内的长任务、Pixi 对象数/纹理数量/纹理内存估计与 JS heap、缩略图/HLS 请求数量。报告可解释、可复跑，附环境元数据（浏览器、GPU、DPR、构建模式、数据集、seed）。
- **后端 mock 索引数据生成**：按 [fr2-003] §2 的四档数据集口径确定性 seed 出 `media-index-1m`（100 万）、`media-index-5m`（500 万）、`media-index-10m`（1000 万），承载在 `apps/mock-studio`；同一 seed 生成相同媒体 ID、Space、路径、日期、时长、状态、AI 标记与缩略图/HLS 状态。
- **后端查询 Benchmark**：覆盖分页、路径前缀、筛选组合、任务队列查询，对照 [fr2-003] §4 阈值出报告（含 SQL/参数、耗时分位数、扫描行数）。
- **HLS 预览样例**：视频预览卡 + 缩略图轨样例，hover/选中时才请求轻量预览元数据或片段（mock），不在滚动时自动播放大量视频。

不做（范围外）：

- 不实现正式高密度媒体浏览器页面（属 P4 / FR2-009）。
- 不接入真实存储库扫描、真实索引与真实转码（属 P2）。
- 不做真实多端复用与投屏（属 P7）。
- 不在本规格里决定 SQLite 是否足够或最终索引/数据库替代方案；那由本原型 Benchmark 结果与后续 ADR 决定。

## 3. 设计（怎么做）

落位遵循 [ADR-0054](../adr/0054-apps-workspace-toolchain-quality-gates.md) 的目标结构，本规格阶段以 mock 先行为前提，不搬真实业务代码：

- `packages/render-pixi`：PixiJS 网格/时间轴渲染核心——容器层级、纹理池与 LRU、可见窗口与 overscan、对象池复用/销毁、缩略图占位与过期请求取消。React 侧只暴露壳层挂载点。
- `packages/benchmark`：前端渲染与后端索引两类 Benchmark 工具。前端侧提供连续滚动/快速跳时间/Space 切换/筛选/HLS 预览等操作脚本，通过 `requestAnimationFrame`、`PerformanceObserver`（longtask）、`performance.memory`（可用时）与 Pixi 运行时计数采集指标，输出分位数报告；后端侧提供查询 harness 与报告格式。
- `packages/mock`：MSW handlers 与 seed 数据生成，供 render-pixi、Benchmark、wiki、mock-studio 共享同一套。
- `apps/mock-studio`：承载三档 mock 索引数据集与交互脚本工作台，执行后端 seed 与查询 Benchmark。
- `apps/wiki`：作为 UI 博物馆预览入口，独立展示 PixiJS 网格/时间轴样例与 HLS 预览卡（引用 [fr2-005](fr2-005-wiki-ui-museum-mockup.md)）。

指标与阈值口径全部**引用** [fr2-003](fr2-003-performance-budget.md)，不在本规格复制其数字表：前端对照 §3 表，后端对照 §4 表，报告输出项对照 §6，数据集分档对照 §2。前端不持有 100 万完整对象数组，只持有可见窗口、索引游标、轻量缓存与选中状态；筛选/排序/Space 权限/AI 状态过滤优先在后端索引层（此阶段为 mock 后端）完成。

## 4. 任务拆分

- [x] 在 `packages/mock` 实现三档索引数据集的确定性 seed 生成（`media-index-1m/5m/10m`），字段覆盖 Space、路径、时间、类型、时长、转码状态、AI 状态、缩略图/HLS 状态。
- [x] 在 `packages/render-pixi` 实现网格 + 时间轴原型：纹理池、可见窗口 + overscan、缩略图占位、过期请求取消，React 只做壳层。
- [x] 在 `packages/benchmark` 实现前端 Benchmark harness：滚动脚本 + 指标采集 + 分位数报告，输出 [fr2-003] §3/§6 指标。
- [x] 在 `apps/mock-studio` 组织后端查询 Benchmark：分页、路径前缀、筛选组合、任务队列查询，出对照 [fr2-003] §4 的报告。
- [x] 实现 HLS 预览样例：视频预览卡 + 缩略图轨（mock），预览与滚动解耦并限流可取消。
- [ ] 在 `apps/wiki` 接入 PixiJS 样例页与 HLS 预览卡，独立可预览。
- [x] 产出一份本机 Benchmark 实测报告（进 `.tmp/` 或 CI artifact，不入库），标注是否达标与超限项映射。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 100 万 mock UI 连续滚动能跑出可解释的帧耗时/长任务/纹理/内存/请求报告，并**达到 [fr2-003](fr2-003-performance-budget.md) §3 前端门**：连续滚动帧耗时 p95 ≤ 16.7ms、p99 ≤ 33ms，10s 滚动窗口内无 >200ms 主线程长任务，初始可交互 ≤ 3s（不等待全量数据或纹理）。
- 后端能 seed 100 万/500 万/1000 万数据并跑出关键查询报告，**对照 [fr2-003] §4 后端门**：Space + 时间分页 `5m` p95 ≤ 200ms、`10m` p95 ≤ 500ms；Space + 路径前缀、筛选组合、任务队列查询分别对照 §4 表的 `5m`/`10m` 门槛，超限项须可解释。
- `apps/wiki` 能独立（不依赖真实后端）预览 PixiJS 网格/时间轴样例与 HLS 预览卡。
- Benchmark 报告可复跑、可解释：至少附环境元数据、初始化耗时、帧耗时分位数、长任务、JS heap、Pixi 对象数、纹理数量与纹理内存估计、缩略图/HLS 请求数量（前端），以及数据库大小、索引列表、数据规模、查询 SQL/参数、耗时分位数、扫描行数（后端）。
- **需实测验收项**：性能是否达标以**本机 Benchmark 实测报告**为准，单元/e2e 全绿不替代它；须由用户在目标机器上复跑并确认帧耗时/长任务与后端查询分位数满足上述 [fr2-003] 门，超限项能映射到 FR2、spec 或 ADR。

## 6. 风险 / 待定

- [fr2-003](fr2-003-performance-budget.md) §3 里 GPU/纹理内存的绝对上限与缩略图缓存张数仍为空泛口径，需要本原型的 render-pixi + Benchmark 实测调出具体数值，再回填到 [fr2-003] 并在需要时以 ADR 固化。
- 若 `10m` 数据集在 SQLite 或现有索引策略下无法满足 [fr2-003] §4 后端门，须按 [ADR-0053] 后果条款另写 ADR 决定索引/数据库替代方案，不在代码里静默降级。
- PixiJS 及相关依赖（Benchmark/mock 工具链）的实际引入须按依赖管理要求先确认版本与批次，本规格阶段不静默安装。
