# ADR-0016：单用户认证

## 状态
已接受（「首次启动自动创建默认用户 admin/admin」一条已被 [ADR-0040](0040-first-run-setup.md) 取代为首次初始化引导；本 ADR 其余决策继续有效）

## 背景
JianVideo 是单用户私有视频媒体服务器，需要保护媒体库和播放接口不被未授权访问。候选方案包括：基于 Cookie 的 JWT 会话、HTTP Basic Auth、OAuth2、API Key。需要权衡安全性、实现复杂度和浏览器兼容性。

## 决策
采用基于 Cookie 的 JWT（HS256）认证方案。登录成功后通过 `Set-Cookie` 设置 `HttpOnly` + `Secure` 的 JWT Cookie，后续请求由 Gin 中间件验证。同时支持 `Authorization: Bearer` 头部传递令牌。首次启动自动创建默认用户（admin/admin）。

## 理由
- **单用户极简**：无需注册、权限管理、令牌刷新等复杂机制
- **HttpOnly Cookie**：有效防止 XSS 攻击窃取令牌
- **Secure Cookie**：确保仅 HTTPS 传输（开发环境需注意）
- **JWT 无状态**：无需服务端存储会话，适合单文件部署
- **bcrypt 哈希**：密码安全存储，抗彩虹表攻击
- **Go 生态成熟**：`golang-jwt/jwt/v5` 和 `golang.org/x/crypto/bcrypt` 是业界标准库

## 后果
- 单用户模式下密码修改需手动操作数据库
- 默认 JWT 密钥需要在生产环境通过环境变量覆盖
- 单用户模式不需要令牌刷新，72 小时有效期足够
- Secure Cookie 在 HTTP（非 HTTPS）环境下不生效，开发时需注意

## 备选方案
- **HTTP Basic Auth**：简单但每次请求都传密码，不如 Cookie 安全，且浏览器原生弹窗体验差
- **API Key**：适合机器间通信，不适合浏览器用户交互
- **OAuth2**：对单用户场景过度设计，引入外部依赖
