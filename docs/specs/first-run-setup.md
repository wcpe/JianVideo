# FR-109 首次初始化引导

> 规格文档。对应 PRD §4 FR-109、验收 AC-18。扩 FR-13（单用户认证）。

## 需求（WHAT/WHY）

系统不再在启动时自动创建 `admin/admin` 默认账户（弱口令安全隐患）。改为：**系统中尚无任何用户时，首次访问引导用户配置用户名+密码创建首个账户并登录**；已有用户则照常进登录页。

## 设计（HOW）

### 后端
- **移除**启动建号：`internal/web/router.go` 删除 `svc.CreateDefaultUser()` 调用（不再自动建 admin/admin）。
- **auth.Service 新增**（`internal/auth/service.go`）：
  - `NeedsSetup() (bool, error)`：系统无任何用户时返回 true。
  - `Setup(username, password string) (*models.User, error)`：仅当无用户时创建首个账户（bcrypt 哈希）；已有用户返回错误「系统已初始化」；用户名/密码空返回错误。
  - `CreateDefaultUser`（admin/admin）**保留**，仅作测试播种用，不再被生产启动调用。
- **新增路由**（`/api/auth/` 前缀，免 APIGuard）：
  - `GET /api/auth/setup-status` → `{ "needs_setup": bool }`（公开）。
  - `POST /api/auth/setup` `{username,password}` → 成功则创建账户 + 签发 JWT 写 cookie（自动登录）+ 返回 `{username}`；已初始化返回 409。

### 前端
- `api/auth.ts`：`getSetupStatus()`、`setup(username,password)`（含 mock）。
- `stores/auth.ts`：状态加 `needsSetup`；`init()` 先查 setup-status，需初始化则置 `needsSetup=true` 并跳过 getMe；新增 `setup()` 动作（成功即认证态）。
- 路由（`App.tsx`）：新增 `/setup` 路由 + `RequireSetup` 守卫（仅 `needsSetup` 时可见，否则重定向 `/login`）；`ProtectedRoute`/`RequireAnon` 在 `needsSetup` 时重定向到 `/setup`。
- 新增 `SetupPage.tsx`：用户名+密码+确认密码表单，提交→`setup()`→进首页。

### 测试播种（取消默认建号的同步改动）
- e2e `newTestServer`（`e2e/server_test.go`）与自建 router 的 `playback_flow_test.go`/`share_flow_test.go`：在 `NewRouter` 后调用 `auth.NewService(sqlDB, secret).CreateDefaultUser()` 显式播种 admin，保持既有 admin/admin 登录用例不破。

## 任务
1. 后端：service 加 NeedsSetup/Setup（测试先行）。
2. 后端：router 删建号、加两路由（测试先行）。
3. e2e：搭建处播种 admin。
4. 前端：api/store/guard/SetupPage + 路由（测试先行）。

## 验收（AC-18）
全新库（无用户）首访→初始化引导页；设账号密码后以该账号登录成功、系统中无 admin/admin；已有用户库→直接登录页、不触发引导。需真机确认。
