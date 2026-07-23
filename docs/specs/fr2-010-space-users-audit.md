# 功能规格：Space、用户与审计边界

> 状态：二切 UI 已完成（首切后端 + 设置页用户/Space 管理；owner 转让 / OpenAPI 迁入仍待）　·　关联 PRD：FR2-010　·　阶段：P5 `0.26.x`　·　决策：[ADR-0056](../adr/0056-space-permission-model.md)　·　前置：[fr2-007](fr2-007-storage-index-space.md)、[fr2-040](fr2-040-audit-events.md)

## 0. 首切范围（本 PR 只做这些）

为控制首交付面，**首切**只做后端可验证闭环 + 最小管理 API；完整前端管理页可同 FR 二切。

| 切片 | 内容 | 首切 |
|------|------|------|
| A | `space_members` + 角色解析 + 服务层/中间件强制过滤 | ✅ |
| B | 用户创建/禁用/列表（默认 Space 的 owner 可管用户） | ✅ |
| C | Space 列表（当前用户可访问）+ 创建 Space + 添加/改角色/移除成员 | ✅ |
| D | 关键写操作挂 `RequireSpaceRole`（删媒体、扫描、建分享等抽样挂钩，非全量路由一天改完） | ✅ 抽样挂钩 + 矩阵单测 |
| E | 审计：用户/成员/Space 变更事件 | ✅ |
| F | 前端「用户与 Space」完整设置页 | ✅ 二切 |
| G | owner 转让、Space 归档、自助注册 | 二切 |

## 1. 背景与目标

P2 已落地默认 Space（`space-default`）、资源 `space_id` 与 **owner-only** 读写下沉。P5 要把「家庭/小团队 10～50 用户」做成产品能力：多用户账号、Space 成员与角色、删除恢复与写回边界仍按 Space 执行，审计继续以 FR2-040 为真源。

目标：

- 支持 10～50 活跃用户（同一实例）。
- Space 成为权限、删除、同步、AI 可见性、分享、审计、写回的唯一策略边界。
- 非成员不得跨 Space 隐式读到数据；非法 Space 头不得静默回退默认 Space（保持 P2 口径并扩展到成员角色）。
- 管理端可创建/禁用用户、管理 Space 与成员，关键动作写审计。

## 2. 需求（要什么）

### 2.1 范围内

- **用户**
  - 由**默认 Space 的 owner**（首用户）创建其他用户；自助注册默认关闭。
  - 字段：id、username、password_hash、status（`active`/`disabled`）、created_at。
  - 禁用用户：拒绝登录；已持 JWT 在鉴权时查 status 拒绝。
  - 改密：保留本人改密；管理员重置放二切（可用创建时发初始密码）。
- **Space**
  - 多 Space：创建（创建者为 owner + member 行）；列表仅返回调用者有成员关系的 Space。
  - `spaces.owner_user_id` 与 `space_members(role=owner)` 保持一致（创建时双写）。
  - `space_members`：`(space_id, user_id)` 唯一，`role` ∈ `owner` | `editor` | `viewer`。
  - API 继续 `X-JianVideo-Space-Id`；缺省默认 Space **仅当** 调用者仍是该 Space 成员。
- **权限矩阵（对齐 ADR-0056）**

  | 能力 | owner | editor | viewer |
  |------|--------|--------|--------|
  | 浏览 / 播放 | ✓ | ✓ | ✓ |
  | 扫描 / 库配置 / 批量写 | ✓ | ✓ | ✗ |
  | 删除 / 恢复 | ✓ | ✓ | ✗ |
  | 分享创建/撤销 | ✓ | ✓ | ✗ |
  | 成员与角色管理 | ✓ | ✗ | ✗ |
  | 危险写回原文件 | ✓ | ✓（FR2-033 再收紧可配置） | ✗ |

- **审计**
  - `user.created` / `user.disabled`、`space.created`、`space.member_added` / `member_role_changed` / `member_removed`。
  - `actor_id` 为真实用户 id。
- **兼容**
  - 单用户旧库：迁移回填默认 Space 一条 `space_members(owner)`；行为与现网一致。

### 2.2 不做（范围外）

- OAuth/OIDC/LDAP；单文件 ACL；联邦 Space；SIEM 导出。
- AI 可见性策略执行（P6）；会话设备表（FR2-062）。
- 首切不做完整设置 UI（二切）。

## 3. 设计（怎么做）

- 数据：`space_members` 表；`users` 增 `status`（默认 active）；迁移回填。
- 包：`internal/space`（成员/角色/可访问 Space 集合）；`auth` 扩展用户管理；api `RequireSpaceRole(min)`。
- 校验落点：服务层列表/详情强制 `space_id` + 成员集合；写操作中间件拒绝不足角色。
- 测试：矩阵单测 + 双用户跨 Space 404/403 + 升级迁移测试。

## 4. 任务拆分

- [x] 模型与迁移：`space_members`、`users.status`、单用户回填
- [x] `internal/space` 角色解析 + 单测
- [x] 鉴权：禁用用户拒绝登录；旧 JWT 返回 `401 USER_DISABLED`；读≥viewer / 写≥editor
- [x] 用户管理 API + 审计
- [x] Space/成员管理 API + 审计
- [x] 写操作由全局守卫按 HTTP 方法要求 editor+（抽样：paths/shares）
- [x] API 集成：viewer 只读、跨 Space 拒绝、禁用登录、旧 JWT `USER_DISABLED`、防自禁用
- [x] 文档：PRD→开发中、API/CHANGELOG 未发布段
- [x] 二切 UI：设置页「用户与 Space」——列/建用户、启停、列/建 Space、成员添加/改角色/移除；非默认 Space owner 列用户 403 时仅隐藏用户管理
- [ ] 二切：owner 转让；RequireSpaceRole 细挂 owner-only 路由；OpenAPI 迁入

## 5. 验收标准

- 第二用户为默认 Space 的 viewer：可读媒体列表/播放相关只读 API；删除/扫描/建分享 → 403。
- 用户 B 非 Space A 成员：带 Space A 头或直访 A 内 id → 403/404，响应体无资源字段泄露。
- 禁用用户后登录失败；旧 JWT 下一受保护请求失败。
- 旧库仅单用户：迁移后登录与默认 Space 扫描/列表行为不变。
- 相关 Go 单测全绿；至少 1 条双用户集成/e2e。

## 6. 风险 / 待定（请审核拍板）

1. **首切是否不做前端管理页**（推荐：是，API+测试验收）？
2. **创建用户权**：仅默认 Space owner（推荐）还是任意 Space owner？
3. **editor 写回**：按 ADR-0056 允许（推荐）；FR2-033 再加二次确认。
