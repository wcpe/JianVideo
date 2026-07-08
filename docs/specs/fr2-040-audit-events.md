# 功能规格：全操作审计日志

> 状态：已审核接受　·　关联 PRD：FR2-040　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

v2 把 Space、删除恢复、配置、任务、缓存清理、元数据回写和后续多用户都建立在可追溯操作之上。当前系统只有业务日志与部分软删除状态，没有可信审计事件真源。FR2-040 需要先建立审计事件模型，使后续 P2/P5 能力都能以同一事件口径追踪“谁在什么时间对什么对象做了什么，前后值是什么”。

目标：

- 建立 `audit_events` 表与审计服务，作为操作事件真源；必须审计的业务变更与审计事件在同一 SQLite 事务内提交，或至少在同一事务内写入 outbox 事件。
- 对配置、库、媒体移动/删除/重命名、元数据回写、任务操作、缓存清理、迁移等关键动作写事件。
- 提供 Space scoped 查询 API 和前端基础查询页。
- 对敏感字段统一脱敏，不把密码、令牌、代理凭据写入日志或事件。

## 2. 需求（要什么）

- 事件字段：
  - `id`、`scope`、`space_id`、`actor_type`、`actor_id`、`action`、`resource_type`、`resource_id`、`before_json`、`after_json`、`metadata_json`、`request_id`、`created_at`。
  - 媒体/库/任务等 Space 资源使用 `scope=space` 且 `space_id` 非空；工具下载、硬件重测、迁移等全局操作使用 `scope=system`，其最终 Space 归属以 ADR-0063 审核结果为准。
- 事件动作首批覆盖：
  - `settings.updated`
  - `library.created/updated/deleted`
  - `media.renamed/moved/deleted/restored`
  - `metadata.writeback.started/succeeded/failed`
  - `task.created/canceled/retried/failed/succeeded`
  - `cache.cleaned`
  - `migration.started/succeeded/failed`
- 查询 API 支持按 Space、动作、资源类型、资源 ID、时间范围分页。
- 必须审计的业务变更不得在审计事件缺失时提交成功；异步流程只能用于导出、展示缓存或通知，不负责补写事件真源。
- 失败业务不得写成功事件。
- 敏感值统一脱敏：密码、令牌、JWT、SMB 凭据、代理 userinfo、路径中可能含用户名的部分按规则处理。
- 审计事件不可通过普通清理删除；保留期与导出归属后续备份/灾难恢复，不在本规格实现。
- 范围内：事件表、服务、关键接入点、查询 API、基础 UI、脱敏工具、测试。
- 不做（范围外）：完整回滚中心、审计导出、不可篡改签名链、多用户角色权限、外部 SIEM。

## 3. 设计（怎么做）

后端：

- 新增 `internal/audit` 模块，提供 `Recorder` 接口：
  - `Record(ctx, EventInput) error`
  - `RecordTx(ctx, tx, EventInput) error`
  - `List(ctx, Query) (Page, error)`
- `audit` 只依赖 repository/db，不依赖业务模块；业务模块通过接口注入记录事件。
- actor 在 P2 单用户阶段使用当前认证用户或 `system`；P5 多用户落地后扩展为真实用户 ID。
- `before_json` / `after_json` 只记录业务必要字段，避免完整对象快照泄露敏感数据或造成表膨胀。
- 对已在业务事务内执行的配置、库、媒体、任务、缓存和迁移操作，业务 repository 必须在同一事务内调用 `RecordTx` 或写入同事务 outbox；审计写入失败时业务事务整体回滚。

脱敏：

- 集中实现 `audit.RedactValue` / `audit.RedactJSON`。
- 对 key 名包含 `password`、`secret`、`token`、`credential`、`proxy` userinfo 的字段做脱敏。
- 日志和 API 响应复用同一脱敏策略。

API：

- `GET /api/audit/events`
- 查询参数：`space_id`、`action`、`resource_type`、`resource_id`、`from`、`to`、`cursor`、`limit`。
- 响应使用 cursor 分页，按 `created_at desc, id desc` 排序。

前端：

- 在系统/管理区域提供审计查询页或 tab。
- 列表展示时间、动作、资源、操作者、摘要；详情展示脱敏后的 before/after。

## 4. 任务拆分

- [ ] 新增 `audit_events` model、repository、脱敏工具与服务接口。
- [ ] 实现审计查询 API 与 cursor 分页。
- [ ] 接入配置变更、库变更、媒体移动/删除/重命名/还原、元数据回写、任务操作、缓存清理、迁移事件。
- [ ] 前端增加审计事件列表与详情展示。
- [ ] 补单元测试：脱敏、事件输入校验、cursor 分页。
- [ ] 补集成测试：关键 API 成功后写事件，失败请求不写成功事件，跨 Space 隔离。
- [ ] 补 E2E：执行设置保存或媒体操作后能查到审计事件。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 配置变更、库变更、媒体移动/删除/重命名/还原、元数据回写、任务取消/重试、缓存清理、迁移至少各有一条集成测试证明会写审计事件。
- 模拟审计写入失败时，对应配置保存、媒体删除、迁移状态更新等必须审计的业务变更不得提交；若采用 outbox，必须证明同事务 outbox 事件存在。
- 审计查询按 Space 隔离，Space A 不能查到 Space B 事件。
- 系统级审计事件与 Space 级事件可明确区分；Space scoped 查询默认不返回 `scope=system` 事件，除非用户有系统管理上下文。
- 敏感字段不会以明文进入 `audit_events`、API 响应或日志。
- 审计 API 支持 cursor 分页与时间范围查询。
- 前端能查询并查看事件详情，涉及 UI 的 Playwright E2E 与截图归档 `.tmp/`。
- `go test`、前端测试、Playwright、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，执行真实设置保存并查询审计事件通过。

## 6. 风险 / 待定

- 已确认：审计事件可靠性、同事务写入/outbox、系统级事件 scope 与保留策略按 ADR-0063 落地。
- 已确认：本阶段不提供用户手动删除审计事件能力。
- 已确认：`before_json` / `after_json` 只记录变更字段和资源摘要，不记录完整对象。
- 已确认：系统级审计事件采用 ADR-0063 的 `scope=system + space_id NULL`。
- P2 仍是单用户，actor 只能记录现有用户或 system；P5 多用户需扩展 actor 语义但不改事件主键模型。
