# ADR-0040：首次初始化引导取代默认账户创建

## 状态
已接受（部分取代 [ADR-0016](0016-single-user-auth.md) 中「首次启动自动创建默认用户 admin/admin」的决策；ADR-0016 的 JWT Cookie 认证方案其余部分继续有效）

## 背景
ADR-0016 确立单用户 JWT Cookie 认证，并规定「首次启动自动创建默认用户（admin/admin）」。该默认弱口令在用户未及时修改时构成安全隐患（暴露在公网/局域网即可被 admin/admin 登录），且产品无修改密码入口（见 FR-108）。需要在不破坏 ADR-0016 认证方案的前提下，消除默认弱口令。

## 决策
取消「启动自动创建 admin/admin」。改为**首次初始化引导**（FR-109）：
- 系统中尚无任何用户时，后端 `GET /api/auth/setup-status` 返回 `needs_setup=true`；前端将一切访问导向初始化引导页 `/setup`。
- 用户在引导页设置用户名+密码，经 `POST /api/auth/setup` 创建**首个**账户（bcrypt 哈希）并自动签发 JWT Cookie 登录。
- `POST /api/auth/setup` 仅在无用户时可用，已有用户返回 `409`（防重复初始化劫持）。
- 已有用户的实例行为不变：直接进登录页，不触发引导。
- `auth.Service.CreateDefaultUser`（admin/admin）保留，但**仅供测试播种**，不再被生产启动调用。

ADR-0016 的其余决策（Cookie JWT、HttpOnly+Secure、中间件校验、Bearer 兼容）保持不变。

## 理由
- 消除默认弱口令这一最常见的单用户部署安全隐患，符合「安全准则：不硬编码默认凭据」。
- 引导式初始化让首个账户由用户掌控，体验优于「先 admin/admin 再要求改密」。
- 仅取代 ADR-0016 的一个子决策，认证机制零改动，影响面最小。

## 影响
- 后端：`internal/web/router.go` 移除启动建号；`auth.Service` 新增 `NeedsSetup`/`Setup`；新增 `/api/auth/setup-status`、`/api/auth/setup` 两个免登端点。
- 前端：新增 `/setup` 路由、`SetupPage`、`RequireSetup` 守卫；`stores/auth` 增 `needsSetup` 与 `setup`；`ProtectedRoute`/`RequireAnon` 在需初始化时导向 `/setup`。
- 测试：依赖 admin/admin 的 e2e/集成在搭建后显式播种（`CreateDefaultUser`）。
- 向后兼容：已初始化（已有用户）的实例不受影响；仅全新实例的首次体验改变。
