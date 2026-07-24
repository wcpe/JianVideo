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

## 当前 v2 P2 规格

- [`fr2-007-storage-index-space.md`](fr2-007-storage-index-space.md)：存储库、Space 归属与数据库索引基线。
- [`fr2-008-hls-transcode-queue.md`](fr2-008-hls-transcode-queue.md)：视频 HLS 预览与转码任务队列。
- [`fr2-017-v020-v2-data-migration.md`](fr2-017-v020-v2-data-migration.md)：v0.20 到 v2 数据迁移与升级安全。
- [`fr2-022-tool-download-proxy.md`](fr2-022-tool-download-proxy.md)：外部工具自动下载与代理。
- [`fr2-024-config-layer-boundary.md`](fr2-024-config-layer-boundary.md)：配置分层边界。
- [`fr2-025-media-types-extension-config.md`](fr2-025-media-types-extension-config.md)：媒体类型与扫描后缀可配置。
- [`fr2-026-abr-transcode-playback.md`](fr2-026-abr-transcode-playback.md)：多码率自动转码与自适应播放。
- [`fr2-027-scan-watch-incremental.md`](fr2-027-scan-watch-incremental.md)：定期扫描、增量更新与目录事件监听。
- [`fr2-028-thumbnail-tiers.md`](fr2-028-thumbnail-tiers.md)：分档缩略图生成。
- [`fr2-030-embedded-metadata.md`](fr2-030-embedded-metadata.md)：文件自带元数据解析。
- [`fr2-031-offline-title-inference.md`](fr2-031-offline-title-inference.md)：本地离线影视信息推断。
- [`fr2-037-task-queue-center.md`](fr2-037-task-queue-center.md)：通用异步任务队列中心。
- [`fr2-040-audit-events.md`](fr2-040-audit-events.md)：全操作审计日志。
- [`fr2-048-cache-management.md`](fr2-048-cache-management.md)：存储与缓存管理。
- [`fr2-052-library-kinds.md`](fr2-052-library-kinds.md)：多媒体库分型。
- [`fr2-056-hwaccel-management.md`](fr2-056-hwaccel-management.md)：硬件转码加速管理面板。
- [`fr2-059-smart-cover-poster.md`](fr2-059-smart-cover-poster.md)：智能封面/海报。
- [`fr2-061-file-hash-dedup.md`](fr2-061-file-hash-dedup.md)：文件级/哈希去重。

## 当前 v2 P3 规格

- [`fr2-029-timeline-preview.md`](fr2-029-timeline-preview.md)：视频进度条悬停预览。
- [`fr2-034-frame-stepping.md`](fr2-034-frame-stepping.md)：逐帧前后步进。
- [`fr2-035-tiered-seek.md`](fr2-035-tiered-seek.md)：阶梯快进快退。
- [`fr2-036-player-core.md`](fr2-036-player-core.md)：可复用播放核心。
- [`fr2-044-subtitle-audio-tracks.md`](fr2-044-subtitle-audio-tracks.md)：字幕与多音轨。
- [`fr2-045-cross-device-watch-history.md`](fr2-045-cross-device-watch-history.md)：跨设备续播与观看历史。
- [`fr2-057-quality-rate-ab-loop.md`](fr2-057-quality-rate-ab-loop.md)：手动清晰度、省流量、变速与 A-B 循环。
- [`fr2-058-pip-background-mobile-gestures.md`](fr2-058-pip-background-mobile-gestures.md)：画中画、后台音频与移动手势。
- [`fr2-060-chapters-bookmarks.md`](fr2-060-chapters-bookmarks.md)：章节与书签。

## 架构对齐（参考 JianArtifact，与产品 P0–P7 正交）

- [`fr2-064-arch-alignment-blueprint.md`](fr2-064-arch-alignment-blueprint.md)：对齐蓝图索引与依赖序（FR2-064）。
- [`fr2-065-root-hygiene.md`](fr2-065-root-hygiene.md)：根目录 allowlist 与卫生门、默认 `data/`（FR2-065，对齐-A）。
- [`fr2-066-apps-web-migration.md`](fr2-066-apps-web-migration.md)：`frontend/` → `apps/web`（FR2-066）。
- [`fr2-067-apps-server-migration.md`](fr2-067-apps-server-migration.md)：业务 Go → `apps/server`（FR2-067）。
- [`fr2-068-toolchain-entrypoint.md`](fr2-068-toolchain-entrypoint.md)：Makefile + Taskfile 入口（FR2-068）。
- [`fr2-069-post-migration-docs.md`](fr2-069-post-migration-docs.md)：迁移后真貌文档（FR2-069）。
- [`fr2-070-backend-layering-repository.md`](fr2-070-backend-layering-repository.md)：repository 分层、保留 GORM（FR2-070，对齐-B）。
- [`fr2-071-openapi-contract.md`](fr2-071-openapi-contract.md)：OpenAPI 契约闭环（FR2-071，对齐-C）。
- [`fr2-072-deploy-skeleton.md`](fr2-072-deploy-skeleton.md)：`deploy/` 骨架（FR2-072，对齐-D）。

## P5：`0.26.x` Space、安全与多用户

> 阶段计划见 Dynamic Spec `spec://index.md`（叙述者工作区）与 [`docs/ROADMAP.md`](../ROADMAP.md) §10。  
> 旧 `share-*.md` / `soft-delete-recycle.md` / `recycle-cleanup.md` 为 v0.20 历史，仅背景引用。

- [`fr2-010-space-users-audit.md`](fr2-010-space-users-audit.md)：Space / 多用户 / 成员角色与审计边界（FR2-010）。
- [`fr2-062-security-baseline.md`](fr2-062-security-baseline.md)：HTTPS/反代指引、登录防爆破、会话与设备（FR2-062）。
- [`fr2-054-recycle-retention.md`](fr2-054-recycle-retention.md)：回收站保留期与自动清理（FR2-054）。
- [`fr2-055-share-enhance.md`](fr2-055-share-enhance.md)：分享增强（禁下载、Space、匿名成本门，FR2-055）。
- [`fr2-033-metadata-writeback.md`](fr2-033-metadata-writeback.md)：危险元数据回写（快照/队列/二次确认，FR2-033）。
- [`fr2-041-rollback-center.md`](fr2-041-rollback-center.md)：操作可回滚中心（FR2-041）。
- [`fr2-051-parental-controls.md`](fr2-051-parental-controls.md)：家长控制与内容分级（FR2-051）。

推荐依赖序：`010 → (062 ∥ 055 ∥ 051)`；`054` 可与 `010` 并行；`033 → 041`。

## P6：`0.27.x` AI 索引、搜索与审核

> 阶段目标见 [`docs/ROADMAP.md`](../ROADMAP.md) §11；契约见 [ADR-0059](../adr/0059-ai-pipeline-vector-index.md)。

- [`fr2-011-ai-pipeline.md`](fr2-011-ai-pipeline.md)：AI 可替换管线——模型注册、推理节点、结果表、默认关闭门、重建（FR2-011）。
- [`fr2-012-ai-search-review.md`](fr2-012-ai-search-review.md)：向量语义搜索、结构化 AI 能力、AI 去重与审核流（FR2-012）。

推荐依赖序：`011 → 012`（012 的 embedding/搜索/审核建立在 011 管线与关闭门之上）。

## 怎么用

1. 复制 `_template.md` 到 `docs/specs/<feature>.md`。
2. 填需求 / 设计 / 任务 / 验收。
3. 按 `sdd-develop-feature` 技能实现，对着 spec 的任务与验收推进。
4. 交付后归并回项目级文档（见上）。

> spec 是 🌡 中频文档：功能开发期动，交付后基本不动。涉及架构决策时在 spec 里**引用** ADR，不重复决策正文。
