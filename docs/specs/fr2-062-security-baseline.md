# 功能规格：安全基线

> 状态：开发中（首切后端+文档 + 二切登录 429 前端已落地；会话表/撤销仍待）　·　关联 PRD：FR2-062　·　阶段：P5 `0.26.x`　·　前置：[fr2-010](fr2-010-space-users-audit.md)（用户/status 已落地）

## 0. 切片范围

| 切片 | 内容 | 状态 |
|------|------|------|
| A | OPERATIONS：HTTPS/反代最小示例（Caddy + Nginx）+ 生产密钥清单（`JWT_SECRET`/`SMB_MASTER_PASSWORD`） | ✅ 首切 |
| B | 登录防爆破：按「用户名规范化 + 客户端 IP」滑动窗口（默认 10 次/10 分钟失败 → 锁 15 分钟，settings 可配）；失败响应统一「用户名或密码错误」；触发 429；成功清计数 | ✅ 首切 |
| C | 审计：`auth.login_failed` / `auth.login_locked`（IP 哈希脱敏） | ✅ 首切 |
| D | 单测：窗口计数、解锁、枚举一致响应、429 | ✅ 首切 |
| E | 会话表 `auth_sessions` + JWT `sid` + 列表/撤销 API + 改密撤其它会话 | ⏳ 待后端 |
| F | 前端会话管理 UI | ⏳ 依赖 E |
| G | 登录页 429 / `LOGIN_LOCKED` 区分展示（橙色锁定 + `Retry-After` 等待提示） | ✅ 二切 |

**首切建议**：文档 + 登录限流后端（无会话表）。**二切已落地 G**（会话表未就绪时先接防爆破 UI）；会话管理 F 仍依赖 E。

## 1. 背景与目标

自托管实例常暴露到公网或半公网。P5 需提供**可落地的安全基线**：运维文档（HTTPS/反代）+ 登录防爆破 + 会话与设备管理，敏感配置继续走环境变量、不入库。
## 2. 需求（要什么）

### 2.1 范围内

- **文档（OPERATIONS）**
  - HTTPS 终止于反代的推荐配置示例（Caddy / Nginx 各一份最小可用）。
  - 必须设置 `JWT_SECRET`、`SMB_MASTER_PASSWORD` 的生产检查清单。
  - 明确「应用本身可只听内网，TLS 在反代」的默认姿势。
- **登录防爆破**
  - 按「用户名 + 客户端 IP」滑动窗口限流（默认：10 次 / 10 分钟失败后锁 15 分钟，可配置）。
  - 失败响应不区分「用户不存在 / 密码错误」（防枚举）；限流触发返回 429 + 中文说明。
  - 成功登录清除该键计数；审计可记 `auth.login_failed` / `auth.login_locked`（脱敏 IP）。
- **会话与设备**
  - 登录签发会话记录：`session_id`、user_id、created_at、last_seen_at、user_agent、ip_hash、revoked_at。
  - JWT 声明含 `sid`；校验时会话必须存在且未撤销。
  - 用户可查看「我的设备/会话」并撤销；改密默认撤销其它会话（可保留当前）。
  - 管理员可撤销指定用户全部会话（依赖 FR2-010）。
- **配置**
  - 限流参数与会话 TTL 走 settings 或环境变量；密钥类仅环境变量。

### 2.2 不做

- WAF、验证码服务商对接、硬件 2FA（可列后续）。
- 自动申请证书（文档指引即可）。
- 完整异常登录地理位置情报。

## 3. 设计（怎么做）

- 存储：`auth_login_attempts`（或内存+可选持久化；多实例部署需 SQLite/表持久化，单二进制默认表）。
- 表 `auth_sessions`；auth 中间件查 sid。
- 中间件顺序：限流（login 路由）→ 验密 → 建会话 → 发 JWT。
- 前端：设置 → 安全 → 会话列表 / 撤销；登录页 429 提示。

## 4. 任务拆分

- [x] OPERATIONS HTTPS/反代与生产密钥清单
- [x] 登录限流（进程内滑动窗口）+ 单测（窗口/解锁/枚举一致/429）
- [x] 失败/锁定审计事件（脱敏 ip_hash）
- [x] 文档：PRD→开发中、API、CHANGELOG
- [x] 二切：登录页 429/`LOGIN_LOCKED` 橙色锁定 Alert + `Retry-After` 提示；`extractErrorCode`/`extractRetryAfterSeconds`；auth store `errorCode`/`loginRetryAfterSec`；MSW `locked` 演示；vitest
- [ ] 二切：会话表 + JWT sid + 撤销 API + 改密撤会话
- [ ] 二切：前端会话 UI；e2e 429 + 撤销

## 5. 验收标准

- 脚本化连续错误密码达到阈值后出现 429，且响应体不暴露用户是否存在。
- 撤销会话后携带旧 JWT 访问受保护 API 失败。
- 改密后其它会话失效（默认策略）。
- 文档可被运维按步骤完成反代 TLS。

## 6. 风险 / 待定

- 反代后真实 IP：需信任 `X-Forwarded-For` 的跳数配置，错误配置会导致限流键污染。
- 会话表膨胀：需 last_seen 更新节流与过期清理任务。
