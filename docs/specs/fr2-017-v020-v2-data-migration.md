# 功能规格：v0.20 到 v2 数据迁移与升级安全

> 规格状态：已完成　·　验收状态：已通过　·　关联 PRD：FR2-017　·　阶段：P2 `0.23.x`

## 1. 背景与目标

P2 引入 Space、通用任务、审计、缓存资产、媒体类型与库分型等 schema。v0.20 旧库不能只依赖无版本记录的 `AutoMigrate` 直接演进；升级必须可预检、可备份、可重入、可校验，并且不能修改原媒体文件。

目标：

- 以有序 migration registry 和 `schema_migrations` 作为 schema 演进真源。
- 支持真实形态的 v0.20 SQLite fixture 升级到当前 v2 P2 schema。
- 在任何迁移写入前完成 settings 风险预检和 SQLite 一致性备份。
- 保证 Runner 单步迁移的 `Up + Validate + succeeded 状态` 事务原子性。
- 提供 dry-run、失败诊断、备份恢复与中断重入路径。

## 2. 当前实现契约

### 2.1 Runner 与单步事务原子性

- `internal/migration.Runner` 按 migration ID 顺序执行 registry。
- 每步包含 `Description`、`SafeToRetry`、`Estimate`、`Up`、`Validate`。
- `running` 状态在单步事务前记录，用于识别进程中断。
- `Up`、`Validate` 和写入 `succeeded + validation_summary` 在**同一个 SQLite 事务**中执行；其中任一环节失败时，该步业务/schema 写入整体回滚。
- 事务失败后在事务外记录 `failed + error_summary`；若失败状态或失败审计自身写入失败，Runner 合并并返回全部错误，不覆盖原始迁移错误。
- 已成功且再次校验通过的步骤跳过；已成功但校验失败的步骤仅在 `SafeToRetry=true` 时重试。遗留 `running/failed` 步骤同样按 `SafeToRetry` 决定继续或阻断。
- 无待执行步骤时不重复创建备份、不重复应用 migration。

### 2.2 dry-run 与 settings 预检

- `DryRun` 返回每步 `id`、说明、预计影响行数、是否执行、是否已应用、`blockers` 和 `warnings`。
- dry-run 不写业务表，不创建或更新 `schema_migrations`、`audit_events`，也不创建备份。
- 真实 `Run` 必须先执行同一份 dry-run 预检；存在 blocker 时在备份和任何迁移写入前停止。warning 仅提示，不阻断迁移。
- settings 预检复用 FR2-024 registry：
  - 已登记 key 且值合法：通过。
  - 已登记 key 但值不符合类型/枚举/格式约束：blocker。
  - 未登记普通历史 key：warning，原样保留，不删除、不改写。
  - 未登记高风险 key：blocker，要求人工确认。
- 高风险未知 key 当前覆盖以下命名边界：JWT/secret/password/passwd/token；数据库或 SQLite 路径/文件；HTTP/HTTPS/server/listen/bind 端口。诊断只输出 key 名，不输出设置值。
- CLI dry-run 已通过 `-migration-dry-run` 落地：向 stdout 输出 JSON 计划后退出，不启动 HTTP 服务，不执行备份或迁移；仅 warning 时退出码为 0，存在 blocker 时输出同一计划并以非零退出码结束。

### 2.3 v0.20 数据与旧任务兼容

- fixture 使用 `internal/migration/testdata/v020_realistic.sql`，包含旧用户、媒体库、媒体、settings、相册、标签、扫描任务、转码任务、分享、健康问题和自定义后缀，不再使用只含少量核心表的最小占位库。
- 旧用户、媒体库、媒体、settings、相册关系、分享、健康问题及旧任务原始行在迁移后保持数据值不变；迁移只追加 v2 所需列、表、索引和兼容映射。
- 历史资源回填到 `space-default`；默认 Space owner 取旧库首个用户。
- 旧 `scan_tasks` / `transcode_tasks` 映射到通用 `tasks`：
  - `completed → succeeded`，`error → failed`，`pending/running` 保持语义。
  - 幂等键分别为 `scan:<legacy_id>`、`transcode:<legacy_id>`。
  - payload 记录 `legacy_table`、`legacy_id` 及原任务关键字段。
  - 使用 `NOT EXISTS` 防重，重复执行映射不会产生重复任务。
- 迁移不得读写原媒体内容；验收比较迁移前后的文件 SHA-256 与 mtime。

### 2.4 自动备份、完整性校验与恢复

- 仅在存在待执行步骤时创建迁移前备份。
- 默认备份目录为数据库文件同目录下的 `backups/`。
- 文件命名：`<数据库名>-before-v2-<UTC时间戳>.sqlite`；同秒重名时追加序号。
- 备份通过当前数据库连接执行 SQLite `VACUUM INTO`，不是在服务运行时直接复制 `.db` 文件，因此能得到不依赖 WAL/SHM 文件拼接的一致快照。
- 备份创建后重新打开副本并执行 `PRAGMA integrity_check`；结果不是 `ok` 时停止，且不开始 migration。
- 备份路径、大小和完整性结果写入 Runner 结果、`schema_migrations.backup_path` 和迁移审计元数据。
- migration 失败时保留备份，不自动删除，也不自动覆盖当前数据库。单步事务写入自动回滚；若需回退整个升级，由运维人员停服后使用迁移前备份恢复数据库。
- 恢复后必须再次运行 `PRAGMA integrity_check`，再使用旧二进制启动；不得仅替换主 `.db` 而保留新库的 `-wal` / `-shm` 文件。

### 2.5 校验与审计

- 校验默认 Space、owner 和所有要求 Space scoped 的表均已完整回填。
- 校验 FR2-007 关键索引及活跃媒体、媒体库筛选、格式筛选、回收站查询 smoke。
- 校验通用任务表不存在旧状态，Space 任务均有 `space_id`，旧任务幂等键无重复。
- 迁移开始、成功、失败写 `scope=system` 的审计事件，`space_id` 为空。

## 3. 实施任务

- [x] 建立 `schema_migrations` 与有序 migration registry。
- [x] 实现 Runner 单步 `Up + Validate + succeeded 状态` 事务原子性、失败状态记录与安全重入。
- [x] 实现 settings blocker/warning 预检、高风险未知 key 阻断和敏感值不泄露。
- [x] 实现 `-migration-dry-run` JSON CLI，验证仅 warning 成功退出、blocker 非零退出且全程无写入、不启动服务。
- [x] 使用真实 v0.20 fixture 覆盖用户、媒体库、媒体、settings、相册、标签、旧任务、分享、健康问题与自定义后缀。
- [x] 实现默认 Space/owner 回填、关键索引创建与查询 smoke。
- [x] 实现旧扫描/转码任务到通用任务的幂等映射。
- [x] 实现迁移前 `VACUUM INTO` 备份、完整性校验、失败保留与停服恢复流程。
- [x] 验证迁移不修改原媒体 SHA-256 与 mtime。
- [x] 完成 Go 单二进制从真实 v0.20 旧库启动迁移测试。
- [x] 完成 1m / 5m / 10m SQLite 迁移规模基准与完整性验收。
- [x] 同步 PRD、ARCHITECTURE、API、OPERATIONS 与 CHANGELOG。

## 4. 验收证据

自动化测试覆盖：

- Runner 单步事务原子性：`Up`、事务内 `Validate`、成功状态写入任一失败均回滚。
- `running/failed/succeeded` 重入分支和不可安全重试阻断。
- blocker 在备份、元数据表和业务写入前停止；warning 不阻断。
- settings 已知非法值、普通未知 key、高风险未知 key及诊断不泄露 value。
- dry-run CLI 输出有效 JSON、按 blocker 决定退出码、不监听端口，且业务表、迁移元数据、审计和备份目录均无写入。
- 真实 v0.20 fixture 全量升级、旧数据保持、默认 Space 回填、旧任务幂等映射、查询 smoke。
- 备份失败前置停止；迁移失败后备份仍可打开；复制备份恢复后旧数据快照一致。
- 原媒体 SHA-256 与 mtime 不变。
- 构建 Go 单二进制，以真实 v0.20 fixture 启动，完成迁移、创建完整性通过的备份并开始监听。

规模基准报告位于 `.tmp/benchmark/fr2-017/`，不入库。测试环境为 Windows 11 Pro for Workstations、AMD Ryzen 9 3950X、63.91 GiB 内存、SQLite 3.45.1；结果如下：

| 数据规模 | 一致性备份 | Runner 完整迁移 | 备份/主库 integrity | 迁移前后计数 | 媒体指纹 | 缺失关键索引 | 结论 |
|---|---:|---:|---|---|---|---:|---|
| 1m | 2.526s | 57.197s | `ok / ok` | 1,000,000 / 1,000,000 | 通过 | 0 | 通过 |
| 5m | 8.540s | 216.203s | `ok / ok` | 5,000,000 / 5,000,000 | 通过 | 0 | 通过 |
| 10m | 18.245s | 596.426s | `ok / ok` | 10,000,000 / 10,000,000 | 通过 | 0 | 通过 |

完整迁移耗时包含 dry-run、备份、迁移元数据、审计及全部 `Up/Validate`。三档数据均无媒体计数变化、无媒体指纹变化、无缺失 Space 归属、无关键索引缺失；备份库与迁移后主库的 `PRAGMA integrity_check` 均为 `ok`。

## 5. 范围外

- 跨数据库迁移或外部 PostgreSQL。
- 搬迁、重命名或改写原媒体文件。
- 自动上传备份到云端。
- 自动选择并覆盖当前数据库完成整库恢复。
- 完整灾难恢复中心；FR2-018 继续承接更广泛的导出与灾难恢复能力。
