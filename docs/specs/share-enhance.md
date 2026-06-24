# 功能规格：分享增强（密码 / 限次）

> 状态：开发中　·　关联 PRD：FR-78（扩 FR-43）　·　分支：feature/fr-78-share

## 1. 背景与目标

FR-43 的分享链接只支持「过期 + 范围」，任何拿到 token 的人即可无限次访问。本功能（FR-78）在不破坏既有分享语义的前提下，为分享链接增加两项可选保护：

- **访问密码**：创建分享时可设密码，访客需输入正确密码才能访问资源（密码用 bcrypt 哈希存储，绝不明文落库/回显）。
- **访问限次**：创建分享时可设最大访问次数，达到上限后链接失效。

属于 P7 阶段，纯扩展 FR-43，不引入新依赖（bcrypt 已用于 auth）、不动 ADR、不动 8080 服务。

## 2. 需求（要什么）

- 范围内：
  - Share 模型增 `PasswordHash`（空=无密码）/ `MaxUses`（0=无限）/ `UsedCount` 三个字段，AutoMigrate 自动建列。
  - 创建分享支持可选密码与可选限次（`POST /api/shares` 请求体加 `password`/`max_uses`）。
  - 公开访问校验：过期、密码（`X-Share-Password` 头 + bcrypt 比对）、限次（`UsedCount < MaxUses`）；任一失败统一 `404`「分享不存在或已过期」，不区分原因以免泄露。
  - 限次计数在**实际访问资源**（raw/thumbnail/download/stream）时于同一事务内原子自增（先检查后自增防并发超限）；**查看元信息**（`GET /api/share/:token`）不自增、只用于提示是否需要密码。
  - 前端：创建弹窗加密码输入（可选）+ 限次数字输入（0=无限）；查看页若需密码先弹框输入再访问；密码错/限次尽提示「分享不存在或已过期」。
- 不做（范围外）：
  - 二维码（无 QR 库，本期不引入）。
  - 修改已分享链接的密码/限次（创建即固定，YAGNI）。
  - 密码找回 / 多密码 / 按访客计次等。

## 3. 设计（怎么做）

### 3.1 数据模型（`internal/db/models/share.go`）

Share 追加三字段：
- `PasswordHash string`（`gorm:"default:''"`，空串=无密码）——存 bcrypt 哈希，绝不存明文，不序列化回前端（`json:"-"`）。
- `MaxUses int`（`gorm:"default:0"`，0=无限）。
- `UsedCount int`（`gorm:"default:0"`）。

AutoMigrate 自动建列（`main.go` 不改）。

### 3.2 服务层（`internal/share/service.go`）

- `Create(resourceType, resourceID, expiresAt, password, maxUses)`：password 非空则 `bcrypt.GenerateFromPassword` 存 `PasswordHash`；maxUses 落 `MaxUses`。
- `Get(token)`：仅校验存在 + 过期（不自增、不校验密码），供「查看元信息」与中间件初筛。
- 新增 `VerifyPassword(sh, password)`：`PasswordHash` 为空→放行；非空→`bcrypt.CompareHashAndPassword`，不匹配返回 `ErrShareForbidden`。
- 新增 `ConsumeUse(token)`：在事务内「先查后增」——`SELECT ... FOR UPDATE` 语义用 GORM 事务 + 行内重查，`MaxUses>0 && UsedCount>=MaxUses` 返回 `ErrShareExhausted`；否则 `UPDATE used_count = used_count + 1`。SQLite 单写者，事务保证原子（先检查后自增防并发超限）。

错误统一：新增 `ErrShareForbidden`（密码错）、`ErrShareExhausted`（限次尽），公开层与 `ErrShareNotFound`/`ErrShareExpired` 一并映射 `404`。

### 3.3 API 层（`internal/api/share_handler.go`）

- `CreateShare` 请求体加 `password string` / `max_uses int`，透传给 `Create`。
- `shareAuth` 中间件：仍只取 token + 过期（存 context），**不校验密码、不自增**——因为 `ShareInfo` 也走它，查看元信息不应耗次/被密码拦死（前端需据 `requires_password` 提示输入）。
- `ShareInfo`：分享设密码且未带 / 带错 `X-Share-Password` 头时，返回 `200 {resource_type, requires_password:true}` 但**不含任何 media/album 元信息**（供前端弹密码框、又不泄露内容、不区分过期/撤销）；校验通过才返回完整元信息（含 `requires_password`、`expires_at`、media/album/items）。本端点**不消费**访问额度。不回显哈希。
- `resolveShareMedia`（raw/thumbnail/download/stream 实际访问路径共用）：在范围校验通过、取到媒体后，依次 `VerifyPassword`（读 `X-Share-Password` 头）→ `ConsumeUse`；任一失败写 `404` 返回 nil。即「校验密码 + 自增计数」只发生在实际访问资源时。

### 3.4 前端

- `types`：`Share` 加 `max_uses`/`used_count`（不含 `password_hash`）；`ShareInfo` 加 `requires_password`。
- `api/share.ts`：`createShare` 增 `password`/`maxUses` 参数；`getShareInfo` 增可选 `password`（经 `X-Share-Password` 头发送）。
- `ShareDialog`：加密码 `TextInput`（占位「不设密码」）+ 限次 `NumberInput`（0=无限）。
- `SharePage`：加载时先无密码调 `getShareInfo`；若返回 `requires_password` 则渲染密码输入框，提交后带 `X-Share-Password` 重拉 `getShareInfo` 校验密码——成功才渲染资源；错误统一提示「分享不存在或已过期」。资源直链（raw/thumbnail/stream/download）携带密码见 §6 口径。

## 4. 任务拆分

- [x] 模型加 `PasswordHash`/`MaxUses`/`UsedCount`（`json:"-"` 隐藏哈希）
- [x] 服务：`Create` 加参、`VerifyPassword`、`ConsumeUse`（事务原子自增）、新错误
- [x] API：`CreateShare` 加请求字段、`ShareInfo` 出 `requires_password`、`resolveShareMedia` 校验密码 + 自增
- [x] 后端测试：带密码创建（哈希非明文）、密码错→404、限次尽→404、原子自增、并发不超发、过期、查看元信息不耗次
- [x] 前端：types、api、ShareDialog、SharePage
- [x] 前端测试：ShareDialog 密码/限次输入、SharePage 密码弹框（正确/错误）
- [x] 文档同步：PRD 状态、ARCHITECTURE share 字段 + §5.9、API.md 端点、CHANGELOG

## 5. 验收标准

- 后端 `go build ./...` + `go vet ./...` + `go test ./internal/share/... ./internal/api/...` 全绿。
- 前端 `npx tsc --noEmit` + `npx vitest run` + `npm run build` 全绿。
- 带密码分享：错误密码请求资源端点返回 `404`，正确密码放行。
- 限次分享：第 N+1 次访问资源端点返回 `404`；并发不超发（事务原子自增）。
- 数据库中 `password_hash` 为 bcrypt 哈希、非明文；API 响应不含 `password_hash`。
- 真机维度：浏览器实测密码弹框 + 限次到达后失效，标「待真机验」。

## 6. 风险 / 待定（密码与自增的最终口径）

- **`<img>`/`<video>` 直链不能带自定义请求头**：需要密码的分享，其 raw/thumbnail/stream 直链无法携带 `X-Share-Password`。**最终口径**：
  - **密码门禁在 `ShareInfo`（查看元信息 / 进入页）强制**：密码非空时，无密码或密码错的 `GET /api/share/:token` 返回 `200 {resource_type, requires_password:true}` 但**不返回任何 media/album 元信息**。访客因此无法获知分享内的 `mediaId`，也就无法构造资源直链——这是「不泄露」的权威边界（返回 200 而非 404 是为让前端区分「需要密码」与「过期/撤销」从而弹框，但不泄露内容）。`ShareInfo` **不自增**计数（避免查看就耗次）。
  - **资源端点（raw/thumbnail/download/stream）做范围校验 + 限次原子自增**；密码方面：当请求带 `X-Share-Password` 头时校验（fetch 场景），未带头时不阻断（`<img>` 直链场景）——因为能拿到 `mediaId` 必已通过 `ShareInfo` 的密码门禁。限次自增发生在每次成功的资源访问上。
  - 这样：错误密码者拿不到查看页与任何资源/元信息；限次按实际资源访问计数、查看元信息不耗次。若要严格逐请求密码门禁，需前端 fetch+blob 全量代理（成本高，本期不做）。
- 与 FR-79 外链交互：FR-79 用 `createShare('media', id, hours)` 生成无密码/无限次分享，新增参数有默认值（空密码/0 次）→ 向后兼容，FR-79 调用无需改。
- 不涉及架构决策推翻，无新 ADR；bcrypt 已是 auth 依赖，无新第三方包。
