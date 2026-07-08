# 功能规格：存储库、Space 归属与数据库索引基线

> 状态：已审核接受　·　关联 PRD：FR2-007　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

P2 要让“存储库扫描”取代 Web 上传成为主入口，并为 500 万到 1000 万资产建立可验证索引基线。ADR-0056 已要求 P2 先落最小 Space 归属，ADR-0058 要求通过 repository 层隔离数据访问，FR2-003 已给出后端查询与 Benchmark 预算。当前生产后端仍无 `spaces`、无 `space_id`、媒体列表仍以 offset 分页为主，无法证明 1000 万资产下的分页、过滤和任务查询达标。

目标：

- 新增默认 Space 与资源 `space_id` 最小归属。
- 为媒体、媒体库、任务、审计、缓存等 P2 资源建立 Space 感知索引基线。
- 收敛媒体列表查询、路径浏览、筛选排序到可 Benchmark 的 repository 接口。
- 以 1m/5m/10m 数据集对关键查询出本机 Benchmark 报告。

## 2. 需求（要什么）

- 新增 `spaces` 表，启动时确保存在默认 Space。
- P2 最小归属阶段记录默认 Space owner 的迁移线索：可在 `spaces.owner_user_id` 保存现有单用户，或预留 `space_members` 默认 owner 记录；完整角色矩阵仍归属 P5。
- `library_paths`、`media_files` 等现有资源新增非空 `space_id`，历史数据归入默认 Space。
- 所有媒体列表、详情、目录浏览、统计、扫描入口必须按 Space 过滤；缺失 Space header 使用默认 Space，非法、不存在或无权限 Space 不得跨 Space 返回数据，也不得静默回退默认 Space。
- 新增关键组合索引，覆盖：
  - Space + 媒体时间 + id 游标分页。
  - Space + 路径/库 + id。
  - Space + 类型/状态 + 时间 + id。
  - Space + 删除态。
  - Space + 任务状态查询（由 FR2-037 具体落表）。
- 媒体列表支持游标分页；旧 `page/page_size` 保持兼容但不得作为 10m 目标路径。
- 数据访问通过 repository 接口落地，不在业务服务里继续散落拼 SQL。
- Benchmark 覆盖 FR2-003 指定的 Space + 时间分页、路径前缀、筛选组合；任务查询门槛与 FR2-037 合批核销。
- 范围内：Space 最小归属、媒体/库 schema 与索引、媒体查询 repository、API Space 头兼容、Benchmark。
- 不做（范围外）：P5 完整多用户成员/角色矩阵、家长控制、分享权限增强、外部数据库接入。

## 3. 设计（怎么做）

Schema：

- `spaces`：`id`、`name`、`owner_user_id`（或等价默认 owner 归属字段）、`created_at`、`updated_at`。
- `library_paths.space_id`：默认 Space，非空，索引。
- `media_files.space_id`：默认 Space，非空，索引。
- 后续任务、审计、缓存表在各自 FR 中引用同一 Space ID，不重复定义 Space 模型。

Space 上下文：

- API 层读取 `X-JianVideo-Space-Id`；缺失时使用默认 Space，保持单用户兼容。
- 不存在、无权限或格式非法的 Space header 必须返回明确错误，不得静默回退默认 Space。
- 服务层方法显式接收 `spaceID` 或 Space-scoped query 参数。
- repository 层强制 Space 过滤，测试覆盖“跨 Space 查不到”。

Repository：

- 新增媒体查询 repository 接口，封装列表、详情、路径前缀、统计所需查询。
- SQLite/GORM 实现可以使用隔离后的 SQLite 优化，但必须标注方言边界。
- 业务层不直接持有 `*gorm.DB` 拼媒体索引 SQL。

分页：

- 新增 cursor 格式，至少包含排序时间与 id，向后兼容旧 page 响应。
- 前端旧页面可继续用 page；P2 Benchmark 和 v2 client 使用 cursor。

Benchmark：

- 复用 `packages/benchmark` 与 FR2-063 数据集生成思路，新增生产 schema 查询 harness。
- 报告进 `.tmp/benchmark/fr2-007/`，记录数据规模、索引列表、SQL/参数、p95/p99、扫描行数、是否达标。

## 4. 任务拆分

- [ ] 定义 `spaces` 与 `space_id` schema，补默认 Space 初始化。
- [ ] 为 `library_paths`、`media_files` 增加 Space 字段、约束和组合索引。
- [ ] 新增媒体查询 repository 接口与 SQLite/GORM 实现。
- [ ] API 层接入 `X-JianVideo-Space-Id`，缺省回退默认 Space。
- [ ] 媒体列表、详情、目录浏览、统计、扫描入口改为 Space scoped。
- [ ] 增加 cursor 分页查询与兼容响应字段。
- [ ] 建立 1m/5m/10m 本机 Benchmark harness 与报告输出。
- [ ] 补单元测试：Space 上下文解析、cursor 编解码、查询条件组合。
- [ ] 补集成测试：默认 Space 创建、跨 Space 隔离、索引存在、旧 page 兼容。
- [ ] 补 Benchmark 验收：FR2-003 后端门槛对照。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 新库启动后自动存在默认 Space；旧数据迁移后所有媒体和库路径都有非空 `space_id`。
- 缺失 Space header 使用默认 Space；不存在、无权限或格式非法的 Space header 返回错误且不得回退默认 Space。
- 默认 Space 记录能追溯现有单用户 owner，为 P5 `space_members` 迁移提供依据。
- 带 Space A 请求无法读到 Space B 的媒体列表、详情、目录浏览和统计结果。
- 关键组合索引存在，Benchmark 报告记录并证明目标查询使用对应索引或给出可解释替代。
- 5m/10m 数据集下，FR2-003 §4 中 Space + 时间分页、路径前缀、筛选组合达到或明确标注未达标阻塞；任务查询门槛与 FR2-037 合批核销。
- 旧 `GET /api/library/media?page=&page_size=` 仍可用；v2 cursor 查询可用。
- `go test`、Benchmark、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，真实 API 带/不带 Space header 的列表与详情隔离行为通过集成复验。

## 6. 风险 / 待定

- 已确认：默认 Space ID 使用稳定字符串，便于 mock/client 对齐；默认 owner 使用 `spaces.owner_user_id`。
- 已确认：旧 page API 返回 cursor 字段，但不要求旧前端消费。
- 若 10m 查询无法达 FR2-003 门槛，必须按 FR2-003 要求追加 ADR 决定索引策略，不得静默降低目标。
