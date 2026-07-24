# 功能规格：操作可回滚中心

> 状态：已交付@v0.26.0　·　关联 PRD：FR2-041　·　阶段：P5 `0.26.x`　·　前置：[fr2-040](fr2-040-audit-events.md)；写回恢复依赖 [fr2-033](fr2-033-metadata-writeback.md)

## 0. 首切范围（本 PR 只做这些）

| 切片 | 内容 | 首切 |
|------|------|------|
| A | `internal/rollback`：`ActionReverter` 注册表 | ✅ |
| B | 可回滚：`settings.updated`（非敏感标量）、`media.deleted`↔还原、`media.restored`↔再软删 | ✅ |
| C | API：`GET /api/rollback/events`（可回滚过滤）+ `POST /api/rollback/apply`（二次确认字段）+ 审计 | ✅ |
| D | 不可回滚：返回稳定 `reason_key`；无 before 老事件标不可回滚 | ✅ |
| E | 单测：设置回滚、软删对称、不可回滚提示 | ✅ |
| F | UI 时间线（审计页可回滚列表 + 二次确认） | ✅ |
| G | `metadata.writeback.succeeded` 快照恢复 | ✅（二切后端） |

## 1. 背景与目标

审计事件已是真源，但用户缺少「一键回到变更前」的产品入口。P5 提供**操作可回滚中心**：展示可回滚历史、二次确认执行回滚、对不可回滚项明确提示。
## 2. 需求（要什么）

### 2.1 范围内

- 页面/API：按 Space 列出近 N 天审计事件，过滤 `action in 可回滚集合`。
- **首批可回滚动作**（必须可自动逆操作或有快照）：
  - `settings.updated`（标量设置键，敏感键除外）
  - `media.renamed` / `media.moved`（库内路径索引，不碰磁盘失败则提示）
  - `media.deleted` → 调还原；`media.restored` → 再软删（对称）
  - `metadata.writeback.succeeded` → 从快照恢复原文件 + 可选恢复库内字段
- **不可回滚**（只展示，按钮禁用 + 原因）：
  - `cache.cleaned`、`migration.*`、硬删除清理、密码变更、成员移除后的数据已不可见等
- 回滚本身写审计 `rollback.applied` / `rollback.failed`，`metadata` 引用原事件 id。
- 二次确认：展示 before/after 摘要；危险级（写回恢复）额外确认。

### 2.2 不做

- 任意事件通用「时间旅行」。
- 跨 Space 批量回滚。
- 区块链式防篡改。

## 3. 设计（怎么做）

- `internal/rollback`：注册 `ActionReverter` 接口，按 action 分发。
- 查询复用 audit repository；权限 owner/editor。
- 前端：`/audit` 或独立 `/rollback` 页，复用事件时间线组件。

## 4. 任务拆分

- [x] Reverter 注册表 + settings / soft-delete 对称（`internal/rollback`）
- [x] API 列表 + apply + Space 隔离 + 审计（`GET /api/rollback/events`、`POST /api/rollback/apply`）
- [x] 单测：设置回滚、敏感/缺 before 不可回滚、软删对称、列表标注
- [x] 文档同步（API / ARCHITECTURE / PRD / CHANGELOG / 本规格）
- [x] 二切：写回快照恢复（`metadata.writeback.succeeded` → 快照覆盖原文件）
- [x] 二切：rename/move reverter（`media.renamed` / `media.moved`）
- [x] 二切：UI 时间线（审计页可回滚列表 + reason_key 提示 + 二次确认 apply）

## 5. 验收标准

- 对可回滚设置变更：回滚后读设置回到 before（首次写入 before 为空 map 时回滚为空串）。
- 软删回滚=还原；再回滚=软删。
- 写回成功事件：快照存在时可回滚覆盖原文件；缺 `snapshot_path`/`file_path` → `missing_snapshot`；快照文件丢失 → `snapshot_gone`；回滚后快照保留。
- 重命名/移动：有完整 before 时可改回旧文件名/旧目录；磁盘目标已存在或路径脱敏 → 明确失败 / `path_redacted`。
- 不可回滚事件返回稳定 `reason_key`（`not_registered` / `missing_before` / `sensitive_keys` / `no_revertable_keys` / `invalid_resource` / `missing_snapshot` / `snapshot_gone` / `path_redacted`）；UI 二切据此禁用按钮。
- 回滚失败写 `rollback.failed` 审计；业务逆操作失败不写 `rollback.applied`。

## 6. 风险 / 待定

- 重命名若磁盘文件已被外部移动：回滚走既有 Rename/Move，失败写 `rollback.failed`。
- before_json 缺失的老事件：标不可回滚。
- FR2-040 对 `C:/Users/<name>/` 脱敏后 `media.moved` 无法还原真实目录（`path_redacted`）；非 Users 路径与 `file_name` 回滚不受影响。
