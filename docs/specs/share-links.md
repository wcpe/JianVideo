# 功能规格：分享链接

> 状态：开发中　·　关联 PRD：FR-43　·　依赖：FR-13（鉴权，已修复）、FR-40（相册）、FR-42（下载）

## 1. 背景与目标

当前所有 `/api/*`（除 `/api/auth/*`）都被 `auth.APIGuard` 强制鉴权（FR-13）。本功能（P2）开一道**受控例外**：用户为指定媒体或相册生成一个**不可猜的 token 分享链接**，免登访客凭 token 只读访问被分享内容（图片在线查看 + 视频在线播放 + 原文件下载），带**过期**与**范围**（哪一个媒体 / 哪一个相册）。

安全是第一考量：token 仅授予「该 token 范围内的资源」的只读访问，越权访问其它媒体一律拒绝；token 加密随机不可枚举；过期即失效。

## 2. 需求（要什么）

- 范围内：
  - 新增 `shares` 表与 `internal/share` 服务：token（加密随机主键）、resource_type（`media`/`album`）、resource_id、expires_at（可空＝永不过期）、created_at。
  - **管理端点（鉴权后，`/api/shares`）**：
    - `POST /api/shares` 创建分享（体 `{resource_type, resource_id, expires_in_hours?}`，校验资源存在；`expires_in_hours>0` 设过期、否则永不过期），返回 token 与元信息。
    - `GET /api/shares` 列出分享（含过期状态）。
    - `DELETE /api/shares/:token` 撤销分享。
  - **公开端点（免登，`/api/share/:token/...`，APIGuard 豁免，经 `shareAuth` 中间件校验 token + 过期）**：
    - `GET /api/share/:token` 分享元信息：媒体分享返回该媒体；相册分享返回相册与成员列表。
    - `GET /api/share/:token/media/:mediaId/raw` 图片在线查看（复用图片 raw）。
    - `GET /api/share/:token/media/:mediaId/thumbnail` 缩略图（相册网格用）。
    - `GET /api/share/:token/media/:mediaId/download` 原文件下载（复用 FR-42）。
    - `GET /api/share/:token/media/:mediaId/stream` 视频在线播放（渐进式流，复用 `playback.StreamFile`）。
    - 每个 `:mediaId` 端点都做**范围校验**：mediaId 必须 == 被分享媒体，或 ∈ 被分享相册成员，否则 `404`。
  - 前端：
    - 管理：媒体播放页 / 相册页加「分享」入口，弹窗选过期时长、生成链接、复制；可在分享管理处撤销（最小实现：生成 + 复制 + 撤销）。
    - 公开查看页 `/s/:token`（免登路由，不触发登录重定向）：按类型展示图片 / 相册网格 / 视频播放器，提供下载。
- 不做（范围外）：
  - 把转码 / HLS 管线开放给免登访客：**安全边界**——公开播放只走渐进式 `StreamFile`（原文件 + Range），不暴露 ffmpeg 转码（防匿名触发转码的资源滥用 / DoS）。需转码才能在浏览器播放的格式（如部分 mkv/hevc）可能无法在线播放，访客可下载原文件。仅本地文件；`smb://` 不支持。
  - 分享可写 / 评论 / 密码保护 / 访问统计（未要求，避免镀金）。
  - 字幕、Seek 上报等播放增强端点的公开镜像（公开播放用原生渐进流即可）。

## 3. 设计（怎么做）

- 数据模型 `internal/db/models/share.go`：`Share{Token(主键), ResourceType, ResourceID, ExpiresAt *time.Time, CreatedAt}`；`resource_type` 常量集中定义（`media`/`album`）。加入 `main.go` AutoMigrate。
- 服务 `internal/share/service.go`（依赖 `db`，与 `settings` 同层）：
  - `Create(resourceType, resourceID, expiresAt *time.Time)`：`crypto/rand` 生成 32 字节 token（hex），校验 resource_type 合法，落库。
  - `Get(token)`：取 token；不存在 → `ErrShareNotFound`；已过期 → `ErrShareExpired`（公开层统一映射 `404`，不区分以免信息泄露）。
  - `List()` / `Revoke(token)`。
  - 不在 share 服务做资源存在性 / 范围判断（那依赖 library），由 api 层用 library 服务判定，保持 share 服务纯粹、无跨模块耦合。
- 范围校验（api 层）：`shareAllowsMedia(share, mediaID)`——media 分享比对 ID；album 分享查 `library.IsMediaInAlbum(albumID, mediaID)`（新增）。
- 鉴权豁免：`auth.APIGuard` 增豁免前缀 `/api/share/`（公开只读，路由内经 `shareAuth` 自校验 token）；管理端点 `/api/shares`（复数）不被该前缀匹配，仍受 APIGuard 保护。
- 中间件 `shareAuth`：取 `:token` → `share.Get` → 失败 `404` → 成功把 share 存入 `gin.Context`。
- 复用既有服务逻辑：抽出薄函数 `writeRawImage(c, mf)`、`writeDownloadFile(c, mf)`、`writeThumbnail(c, mediaID)` 供鉴权版与分享版共用（精准重构，不改鉴权版行为）；视频流复用 `playback.StreamFile`，HLS 不公开。
- 装配：`main.go` 建 `share.NewService(db)` 注入 Handler（`WithShareService`）；`web.NewRouter` 注册公开 `/api/share/:token` 组（带 pbSvc 用于流）。
- 前端：`api/share.ts` 管理 + 公开 API；播放页 / 相册页加「分享」弹窗；新增免登 `/s/:token` 查看页（App 路由层对该前缀跳过登录守卫）。

## 4. 验收标准

- 创建分享返回不可猜 token；`GET /api/share/:token` 免登返回被分享媒体 / 相册成员。
- **范围隔离**（安全核心）：用 A 的 token 访问不属于其范围的 mediaId → `404`；过期 token → `404`；撤销后 → `404`；伪造 token → `404`。
- 管理端点 `/api/shares` 未鉴权 → `401`（仍受 APIGuard）；公开端点 `/api/share/:token` 免登可达（豁免生效），且 `/api/shares` 不被豁免误伤。
- 图片在线查看、视频渐进式在线播放、原文件下载经 token 正常工作；`smb://` 流被拒。
- 后端 `go build ./...`、受影响包 `go test`（含范围隔离与豁免边界用例）、`go test -race` 全绿；前端 `npm run build`/`npm run test` 全绿。

## 5. 风险 / 待定

- 安全边界：豁免前缀必须精确（`/api/share/` 带尾斜杠，不误伤 `/api/shares`）；范围校验必须覆盖每个 `:mediaId` 公开端点，缺一即越权。以测试锁定。
- token 不可枚举：`crypto/rand` 32 字节；不用自增 ID / 时间戳。
- 公开播放只走渐进流（不转码），是刻意的安全/资源取舍；需转码格式在线播放受限，访客可下载。
- 前端公开查看页必须绕过登录守卫，否则免登访客被重定向到登录页。
