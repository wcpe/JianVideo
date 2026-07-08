# 功能规格：v0.20 到 v2 数据迁移与升级安全

> 状态：已审核接受　·　关联 PRD：FR2-017　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

P2 会引入 Space、任务队列、审计、缓存资产、媒体类型、库分型等新 schema。当前项目主要依赖 GORM `AutoMigrate` 和少量迁移测试，缺少版本化 schema migration、默认 Space 回填、索引重建、旧库 fixture 演练和中断恢复策略。对于已有 v0.20 用户，升级必须可验证、可重入、可备份，不能把历史媒体库、配置或原文件置于风险中。

目标：

- 建立版本化数据库迁移机制和迁移状态真源。
- 支持 v0.20 旧库升级到 v2 P2 schema，含默认 Space 回填与索引重建。
- 提供 dry-run、备份、执行、校验、中断重入与失败报告。
- 迁移过程不移动、不修改原媒体文件。

## 2. 需求（要什么）

- 新增 `schema_migrations` 表，记录迁移 ID、状态、开始/完成时间、错误摘要、校验摘要。
- 每个 schema 变更以有序 migration 表达，不再只依赖隐式 AutoMigrate；migration 可调用 GORM Migrator/AutoMigrate，raw SQL 仅用于隔离标注的 SQLite 优化。
- 升级前创建 SQLite 备份文件；备份路径、大小、校验结果写入迁移事件。
- 迁移步骤至少覆盖：
  - 创建默认 Space。
  - 为现有 `library_paths`、`media_files` 回填 `space_id`。
  - 创建或更新关键索引。
  - 迁移旧扫描/转码任务到统一任务模型或建立兼容映射。
  - 保留现有 settings，并按 FR2-024 registry 校验已知 key。
- dry-run 输出将执行的步骤、预计影响行数和阻塞项，不写业务表，也不写 `schema_migrations` 或审计表。
- 迁移可重入：中断后再次启动能识别已完成步骤并继续或安全失败。
- 迁移完成后运行校验：行数、非空约束、索引存在、Space 回填、关键 API smoke。
- 范围内：后端迁移框架、旧库 fixture、备份与校验、迁移 API/CLI 或启动流程、审计事件。
- 不做（范围外）：跨数据库迁移、外部 PgSQL、原媒体文件搬迁、自动上传备份到云、完整灾难恢复中心。

## 3. 设计（怎么做）

迁移框架：

- 新增 `internal/migration` 模块，提供 migration registry。
- migration ID 使用递增字符串，例如 `20260708_0001_create_spaces`。
- 每个 migration 包含 `Up`、`Validate`、`Description`、`SafeToRetry`。
- `schema_migrations` 记录每步状态：`pending/running/succeeded/failed`。

执行时机：

- 服务启动时检测 schema 版本；若需要迁移且配置允许自动迁移，则先备份再执行。
- 若检测到高风险或 dry-run 失败，服务进入受限模式或拒绝启动并输出中文错误。
- 默认对已知安全迁移自动备份后执行；dry-run 检测高风险时拒绝正常启动并提示手动处理。

备份：

- 对 SQLite 主库、WAL、SHM 采用一致性备份策略：优先使用 SQLite backup API 或 `VACUUM INTO`；离线复制前必须 checkpoint 并确认无写入连接。
- 备份文件放入数据目录的 `backups/`，命名含版本和时间戳。
- 备份不入库、不上传。
- 备份完成后必须能打开并通过 `PRAGMA integrity_check`，失败则不得继续迁移。

校验：

- 默认 Space 存在。
- 所有必须 Space scoped 的现有表 `space_id` 非空。
- 关键索引存在。
- 旧 settings 无未知高风险 key；未知 key 按 FR2-024 策略报告。
- 媒体总数、库路径总数与迁移前一致。

审计：

- 迁移开始、成功、失败写 FR2-040 事件。

## 4. 任务拆分

- [ ] 设计并实现 `schema_migrations` 与 migration registry。
- [ ] 实现 SQLite 一致性备份、dry-run、执行、校验与恢复演练流程。
- [ ] 编写默认 Space 与 `space_id` 回填迁移。
- [ ] 编写关键索引创建/校验迁移。
- [ ] 编写旧 settings 校验与兼容迁移。
- [ ] 编写旧任务队列迁移或兼容映射步骤。
- [ ] 准备 v0.20 fixture DB，用于迁移集成测试。
- [ ] 接入迁移审计事件。
- [ ] 补单元测试：迁移排序、重入、失败状态、校验摘要。
- [ ] 补集成测试：fixture 备份、dry-run、执行、中断重入、失败回滚说明。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG、升级说明。

## 5. 验收标准

- v0.20 fixture DB 能 dry-run，输出步骤和影响范围且不改业务表、`schema_migrations` 或审计表。
- v0.20 fixture DB 执行迁移后，媒体/库路径数量不变，默认 Space 和 `space_id` 回填完整。
- 迁移前自动生成可用 SQLite 备份，备份能打开且 `PRAGMA integrity_check` 通过；失败时不删除备份。
- 模拟中断后再次执行能重入，已完成步骤不重复破坏数据。
- 迁移完成后关键索引存在，FR2-007 查询 smoke 通过。
- 迁移开始、成功、失败都有审计事件。
- `go test`、迁移集成测试、`pnpm run quality` 全绿。
- Go 单二进制在临时复制的旧库上启动并完成迁移复验通过。

## 6. 风险 / 待定

- 已确认：版本化迁移框架按 ADR-0062 落地，改变当前主要依赖 GORM `AutoMigrate` 的升级模型。
- 已确认：默认自动备份后执行可重入迁移；若 dry-run 检测高风险则拒绝正常启动并提示手动处理。
- 已确认：旧 `scan_tasks` / `transcode_tasks` 迁移到 `tasks`，并保留兼容查询窗口。
- SQLite 大库备份可能耗时较长，需要在验收中记录 1m/5m/10m 规模下备份和索引重建耗时。
