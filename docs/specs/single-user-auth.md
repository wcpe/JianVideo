# 功能规格：单用户认证

> 状态：开发中 · 关联 PRD：FR-13 · 分支：feature/fr-13-auth

## 1. 背景与目标

FR-13 属于第一期（MVP）范围。系统为单用户模式，需要极简的账号 + 密码登录体系，保护媒体库和播放接口。单用户模式意味着无需注册、无需权限管理，首次启动时自动创建默认用户即可。

## 2. 需求（要什么）

- 首次启动若无用户，自动创建默认用户（admin/admin），密码使用 bcrypt 哈希存储
- 登录接口 `POST /api/auth/login`：验证用户名和密码，成功后设置 HttpOnly + Secure Cookie
- 登出接口 `POST /api/auth/logout`：清除 Cookie
- JWT HS256 签名，密钥从环境变量 `JWT_SECRET` 读取（默认值仅用于开发），有效期 72 小时
- Cookie 设置 `HttpOnly` + `Secure` 标志
- 认证中间件保护内部 API，支持 Cookie 和 `Authorization: Bearer` 两种令牌传递方式
- 默认用户创建幂等：重复调用不产生重复用户

范围外：
- 不做注册接口（单用户模式，密码修改由管理员手动操作数据库）
- 不做多用户/权限管理
- 不做令牌刷新/续期接口

## 3. 设计（怎么做）

### 模块结构

```
config/config.go          # 配置加载（含 JWTSecret）
internal/db/db.go         # SQLite 数据库连接 + schema 初始化
internal/db/models/user.go # User 模型 + CRUD
internal/auth/jwt.go      # JWT 生成与验证
internal/auth/middleware.go # Gin 认证中间件
internal/auth/service.go  # 登录逻辑 + 默认用户创建
internal/web/router.go    # 路由配置（登录/登出/受保护路由）
main.go                   # 入口
```

### 数据模型

复用 ARCHITECTURE.md §3 定义的 `users` 表：
- `id` INTEGER PK, `username` TEXT UNIQUE, `password_hash` TEXT, `created_at` DATETIME

### 认证流程

1. 用户提交 `username` + `password`
2. 按 `username` 查找用户，使用 `bcrypt.CompareHashAndPassword` 验证
3. 验证通过 → 生成 JWT（HS256, 72h）→ 设置 Cookie
4. 后续请求通过中间件验证 JWT

### 依赖

- `github.com/golang-jwt/jwt/v5` v5.2.1 — JWT 生成与验证
- `golang.org/x/crypto/bcrypt` — 密码哈希
- `github.com/gin-gonic/gin` v1.12.0 — HTTP 框架
- `github.com/mattn/go-sqlite3` v1.14.24 — SQLite 驱动

## 4. 任务拆分

- [x] 搭建项目骨架（go.mod, main.go, config, db, auth, web 模块）
- [x] User 模型与数据库操作
- [x] JWT 生成与验证
- [x] 认证中间件
- [x] 登录/登出路由
- [x] 默认用户自动创建
- [x] 测试覆盖（JWT、中间件、服务、路由）
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG

## 5. 验收标准

- **AC-13.1**：首次启动自动创建 admin 用户，密码 admin（bcrypt 哈希）
- **AC-13.2**：`POST /api/auth/login` 正确凭据返回 200 + `Set-Cookie` 头（HttpOnly + Secure）
- **AC-13.3**：错误凭据返回 401
- **AC-13.4**：`POST /api/auth/logout` 返回 204 + 清除 Cookie
- **AC-13.5**：受保护路由无令牌返回 401
- **AC-13.6**：受保护路由带有效 Cookie 返回 200
- **AC-13.7**：受保护路由带 `Authorization: Bearer` 令牌返回 200
- **AC-13.8**：过期/篡改令牌返回 401

## 6. 风险 / 待定

- 默认 JWT 密钥（`jianvideo-default-secret-change-me`）仅适用于开发，生产环境必须通过 `JWT_SECRET` 环境变量覆盖
- 单用户模式下密码修改需手动操作数据库，后续版本可考虑增加密码修改接口
