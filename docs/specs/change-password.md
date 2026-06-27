# FR-108 登录用户修改密码

> 规格文档。对应 PRD §4 FR-108、验收 AC-17。扩 FR-13（单用户认证），复用 FR-109/ADR-0040 的设密能力。

## 需求（WHAT/WHY）
已登录用户可自助修改密码：校验当前密码后设置新密码，bcrypt 存储、即时生效。补齐「无修改密码入口」的缺口（与 FR-109 取消默认弱口令配套）。

## 设计（HOW）

### 后端
- `models.UpdateUserPassword(d, username, hash)`：更新指定用户密码哈希，用户不存在返回 `sql.ErrNoRows`。
- `auth.Service.ChangePassword(username, current, new)`：校验当前密码（bcrypt），不符返回哨兵错误 `ErrCurrentPasswordWrong`；新密码空或用户不存在返回错误；通过则 bcrypt 新密码并更新。
- 路由 `POST /api/me/password`（**受 APIGuard 保护**，不放 `/api/auth/` 豁免前缀）：用户名取自认证上下文；当前密码不符 → `401`，参数错误 → `400`，成功 → `204`。

### 前端
- `api/auth.ts`：`changePassword(old, new)` → `POST /api/me/password`（含 mock）。
- `SettingsPage` 新增「账户安全」卡（独立于运行期设置加载）：当前密码 / 新密码（≥6 位）/ 确认新密码；前端校验长度与一致性，成功 toast 并清空。

## 验收（AC-17）
错误当前密码改密被拒；正确当前密码改密成功后旧密码登录失败、新密码登录成功。需真机确认（已 API + UI 实机核验）。
