# 功能规格（specs）

非平凡功能在动手前先写一份**工作规格**：一个功能一个文件 `docs/specs/<feature>.md`，把"要什么 / 怎么做 / 任务 / 验收"集中一处，再实现。模板见 [`_template.md`](_template.md)。

## 何时写

- **写**：新增一个非平凡功能 / 能力，或任何够得上一个分支 / PR 的功能。
- **不写**：小改动、bug 修复、重构、依赖升级——走 PRD 状态列 + 对应技能即可。别为每个小改动建 spec（简单优先）。

## 与项目级文档的分工（别双源打架）

- `docs/PRD.md`：需求登记册——该功能在 PRD 是**一行 FR2 + 阶段 + 状态**。
- `docs/ROADMAP.md`：阶段版本线——该功能归属哪个 P 阶段与 `0.y.x` 版本线。
- `docs/specs/<feature>.md`：该功能**开发期的详细工作规格**（比 PRD 那行细）。
- 交付后：持久真相归并回 PRD（FR2 状态标 `已交付@vX.Y.Z`）+ `ARCHITECTURE.md`（更新到现状）+ ADR（若有架构决策）；spec 留作该功能的历史记录，基本不再改。

## v0.20 旧规格状态

`docs/specs/` 中仍保留一些 `FR-<数字>` 旧编号规格，它们属于 `v0.20.x` 之前的历史记录，只能用于查询当时为什么这么做，不能作为 v2 新路线的排期依据。v2 新工作必须：

- 使用 `FR2-NNN` 编号，并在 `docs/PRD.md` 登记阶段与状态。
- 文件名优先使用 `fr2-<编号>-<能力名>.md` 或清晰的能力名。
- 若需要复用旧规格，只能在新 spec 里引用旧文件作为背景，不直接续写旧 FR 规格。
- 旧规格是否迁入归档目录，必须由 P0 的旧能力矩阵统一决定，避免零散移动导致链接失效。

## 当前 v2 P0 规格

- [`v0.21-v2-restructure.md`](v0.21-v2-restructure.md)：v2 文档、架构边界和 P0 工作入口。
- [`fr2-002-workspace-toolchain-quality.md`](fr2-002-workspace-toolchain-quality.md)：P0.5 工作区、前端技术栈、wiki UI 博物馆、mock 先行和最严质量门。
- [`fr2-003-performance-budget.md`](fr2-003-performance-budget.md)：大库性能预算与 Benchmark 口径。
- [`fr2-004-compatibility-matrix.md`](fr2-004-compatibility-matrix.md)：兼容迁移与旧能力矩阵。
- [`fr2-005-wiki-ui-museum-mockup.md`](fr2-005-wiki-ui-museum-mockup.md)：Wiki UI 博物馆、mockup 先行和交互预览验收。

## 当前 v2 P1 规格

- [`fr2-006-api-client-multiend.md`](fr2-006-api-client-multiend.md)：统一媒体 API client、TanStack Query keys、Space 上下文、任务状态与端能力检测（mock 先行）。
- [`fr2-063-pixijs-prototype-benchmark.md`](fr2-063-pixijs-prototype-benchmark.md)：PixiJS 100 万素材原型与前后端 Benchmark harness、mock 索引数据与 HLS 预览样例。

## 怎么用

1. 复制 `_template.md` 到 `docs/specs/<feature>.md`。
2. 填需求 / 设计 / 任务 / 验收。
3. 按 `sdd-develop-feature` 技能实现，对着 spec 的任务与验收推进。
4. 交付后归并回项目级文档（见上）。

> spec 是 🌡 中频文档：功能开发期动，交付后基本不动。涉及架构决策时在 spec 里**引用** ADR，不重复决策正文。
