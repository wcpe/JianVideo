# 功能规格：文件级/哈希去重

> 状态：已交付@v0.23.0　·　关联 PRD：FR2-061　·　阶段：P2 `0.23.x`　·　分支：codex/fr2-061-file-hash-dedup

## 1. 背景与目标

现有去重基于缩略图 dHash，适合近似视觉重复，但 FR2-061 要求“文件级/哈希去重”，即非 AI 的重复文件检测与处理。P2 需要建立内容 hash、size+hash 索引、任务队列化和 Space 隔离，避免 1000 万资产下 O(n²) 聚类不可用。

目标：

- 建立文件内容 hash 真源，用于精确重复检测。
- 支持批量 hash backfill、增量刷新和重复分组查询。
- 去重处理复用软删除/回收站，不直接物理删除原文件。

前置依赖：FR2-007（Space/索引）、FR2-017（迁移框架）、FR2-037（任务队列）、FR2-040（审计核心）、FR2-027（扫描变更 hook）。

## 2. 需求（要什么）

- 对媒体原文件计算内容 hash，首批算法使用 SHA-256。
- 使用 size 预筛 + hash 分组，避免全量两两比较。
- 文件大小或 mtime 变化后 hash 标记 stale 并重新计算。
- hash 计算走 FR2-037 任务队列，支持进度、取消、重试。
- 重复组按 Space 隔离，跨 Space 不合并；精确重复查询必须排除已软删、`missing` 与 `content_hash_stale=true` 的媒体。
- 前端展示重复组、路径、大小、库、时间；用户选择项后走批量软删。
- dHash 感知重复保留为“相似重复”，本规格新增“精确重复”，不混为一类。
- 范围内：内容 hash schema、计算任务、重复查询 API、前端基础处理、测试/Benchmark。
- 不做（范围外）：AI 相似度、跨 Space 合并、自动删除、二进制差异分析。

## 3. 设计（怎么做）

Schema：

- `media_files.content_hash`、`content_hash_algo`、`content_hash_computed_at`、`content_hash_stale`。
- 索引：`(space_id, file_size, content_hash)`，并保留 `content_hash_stale` 查询索引。
- `media_hash_groups` 保存每个 Space 的内容哈希重复组快照，回填完成后重建，查询时仍回连 `media_files` 排除软删、missing 与 stale 项。

计算：

- 流式读取文件计算 SHA-256，不一次性加载大文件。
- SMB 文件遵循现有文件访问能力；失败记录错误摘要，不写 hash。
- 对 size 相同但 hash 空的候选优先排队。

API：

- `POST /api/library/file-hashes/backfill`
- `GET /api/library/duplicates/exact`
- 现有 dHash 接口保留并补 Space scoped 过滤，前端区分“精确重复”和“相似重复”。

处理：

- 用户选中重复项后复用批量软删，不物理删除。
- 删除事件写审计。

## 4. 任务拆分

- [x] 增加内容 hash 字段、索引与迁移。
- [x] 实现流式 SHA-256 计算与 stale 判断。
- [x] 接入扫描变更和 backfill 任务队列。
- [x] 新增精确重复查询 API。
- [x] 前端去重页区分精确重复与相似重复。
- [x] 接入批量软删和审计。
- [x] 补单元测试：hash 计算、size+hash 分组、stale 判断。
- [x] 补集成测试：重复文件分组、大文件流式、SMB/不可读失败。
- [x] 补 Benchmark：1m/5m/10m hash 分组查询延迟。
- [x] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 两个内容完全相同的文件在同 Space 下进入同一精确重复组。
- 同 size 不同 hash 不会被误判重复。
- 文件变化后 hash stale 并可重新计算。
- 精确重复查询只包含未软删、非 `missing` 且 `content_hash_stale=false` 的媒体；相似重复旧接口也必须 Space scoped。
- hash backfill 有任务进度、可取消、失败可重试。
- 重复处理只软删选中项，不动未选项和源文件物理删除。
- Benchmark 使用 1m/5m/10m 数据集、固定 seed、明确命令与报告路径，记录 p95/p99；精确重复分组查询需达到 FR2-003 筛选组合门槛，未达标即阻塞并需追加索引/ADR。
- `go test`、集成测试、去重页 E2E、`pnpm run quality` 全绿。

## 6. 风险 / 待定

- 已确认：首批精确 hash 坚持 SHA-256，正确性优先；性能不足再评估 xxhash 等依赖。
- 大库首次 backfill IO 成本高，必须由任务队列限速并可暂停/取消。
