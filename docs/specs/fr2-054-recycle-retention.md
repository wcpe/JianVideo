# 功能规格：回收站保留期与自动清理

> 状态：开发中（首切后端 + 二切前端 UI 已落地；Space 级覆盖仍待）　·　关联 PRD：FR2-054　·　阶段：P5 `0.26.x`　·　前置：软删/清理实现（历史 [soft-delete-recycle](soft-delete-recycle.md) / [recycle-cleanup](recycle-cleanup.md) 仅作背景）；不依赖多角色，可与 FR2-010 并行

## 0. 首切范围（本 PR 只做这些）

| 切片 | 内容 | 首切 |
|------|------|------|
| A | 全局设置键 `recycle_retention_days`（默认 30；`0`=不自动清理）+ `recycle_auto_cleanup_enabled`（默认 true） | ✅ |
| B | `ListExpiredDeletedMediaInSpace` + 复用 `cleanupRecycleItem` 的有界自动清理（每轮上限 N，如 50） | ✅ |
| C | 任务 `recycle.retention.tick`：启动注册周期入队 / 可手动触发；缺盘符路径则跳过该项记失败，不整体拒绝整轮（与手动「全部清理」语义区分） | ✅ |
| D | dry-run API：`POST /api/library/recycle/auto-cleanup/preview` 返回将清理数与缺失盘符 | ✅ |
| E | 审计汇总 `recycle.auto_cleanup`（moved/failed/skipped）+ 单测 | ✅ |
| F | 前端设置保留期 + 回收站到期提示 | ✅ 二切 |
| G | Space 级覆盖（非仅全局默认） | 二切 |

**首切建议**：无前端；验收靠单测 + dry-run/实跑 API。

## 1. 背景与目标

已有：软删除、回收站列表/还原、手动「清理到盘符回收站目录」（`CleanupRecycleInSpace`，缺盘符**整体拒绝**）。P5 要补 **保留期** 与 **到期自动清理**，避免回收站无限膨胀。
## 2. 需求（要什么）

### 2.1 范围内

- Space 级设置（可有实例默认）：
  - `recycle_retention_days`：保留天数，默认 30；`0` 表示不自动清理（仅手动）。
  - `recycle_auto_cleanup_enabled`：总开关，默认 true（当 days>0）。
- 定时/周期任务（复用任务中心 FR2-037）：
  - 扫描 `deleted_at < now - retention` 的软删媒体。
  - 执行与手动清理相同的安全路径：未配置盘符回收站路径则**跳过该项并记审计/任务错误**，不静默硬删磁盘文件。
  - 批量有上限（如每轮 N 条），避免长事务。
- 管理 UI：设置页展示保留期；回收站页展示「将于何时被自动清理」的提示（按 deleted_at+retention 计算）。
- 审计：`recycle.auto_cleanup` 汇总事件（成功/跳过/失败计数）。

### 2.2 不做

- 按媒体类型不同保留期（二期）。
- 自动清空「盘符回收站目录」内文件（OS 回收站生命周期不归本系统）。
- 跨 Space统一强制策略（仅默认值继承）。

## 3. 设计（怎么做）

- settings：Space 覆盖 + 全局默认；解析失败回退默认。
- `library`/`storage`：抽取「单条软删项清理」供手动与自动共用。
- worker：`recycle.retention.tick` 周期入队（启动注册 + 可手动触发 dry-run）。
- dry-run API：返回将清理的数量与缺失盘符列表，不改数据。

## 4. 任务拆分

- [x] 设置键与校验（`recycle_retention_days` / `recycle_auto_cleanup_enabled` / interval）
- [x] 过期查询 + 有界自动清理（缺路径跳过单条，不整轮 abort）
- [x] 启动调度 tick（复用 ScanScheduler）+ 手动 preview/run
- [x] dry-run API + 审计汇总
- [x] 单测：到期选中、未到期跳过、缺路径跳过不硬删
- [x] 文档：PRD→开发中、API/CHANGELOG/ARCHITECTURE
- [x] 二切 UI：设置页保留期/开关/周期；回收站页策略摘要与行内到期提示
- [ ] 二切：Space 级覆盖；多 Space 调度遍历
## 5. 验收标准

- 将保留期设为 0 天且开关开：不自动清理。
- 构造 `deleted_at` 超期项：自动任务后该项离开回收站且文件进入配置的回收站目录（或因缺路径而保留并报错可观测）。
- dry-run 与实跑数量一致（在无并发变更时）。
- Space A 的保留期不影响 Space B。

## 6. 风险 / 待定

- 时钟回拨导致误清：使用 DB 时间或单调策略说明。
- SMB 无盘符项：与手动清理一致，跳过并告警。
