# 功能规格：章节与书签

> 状态：开发中　·　关联 PRD：FR2-060　·　阶段：P3 `0.24.x`　·　分支：待定

## 1. 背景与目标

长视频需要通过章节快速理解结构并跳转，也需要由用户在关键时间点保存个人书签。两者来源和可信度不同：章节来自媒体文件内嵌元数据，应只读解析；书签是 Space 内单用户创建的业务数据，应保存到服务端并支持跨设备读取和修改。

目标：

- 从真实媒体文件解析内嵌章节，规范化后持久化，并在播放器中只读展示和跳转。
- 支持用户在当前播放时间创建手动书签；书签包含必填标题和可选备注，可编辑、删除和跳转。
- 章节与书签均按 Space 隔离；书签以服务端 SQLite 为真源，实现同一 Space 单用户跨端同步。
- 使用单调递增 `revision` 保护书签并发更新与删除，不以时间戳先后判断冲突。
- 以 Windows 已安装 PWA 连接真实服务、播放真实媒体的 headed 实跑作为 P3 验收项。
- 不为章节或书签生成视频截图、预览帧、缩略图或其他缓存图片。

已确认边界：内嵌章节只读解析；手动书签标题必填、备注可选；书签可编辑和删除；按 Space 单用户跨端同步；并发条件使用递增 `revision`；软删期间数据保留但普通查询不可见；物理清理级联归 FR2-054/P5；数据请求由 `media-client` 负责；不生成截图。

前置依赖：FR2-007（Space 归属）、FR2-017（版本化迁移）、FR2-030（ffprobe 元数据解析）、FR2-036（播放器核心）、FR2-040（审计事件）。

## 2. 需求（要什么）

### 2.1 内嵌章节

- 视频扫描、元数据刷新或 backfill 时，从 ffprobe 的真实 `chapters[]` 输出解析内嵌章节。
- 章节字段至少包括开始时间、结束时间、标题、序号、来源和来源指纹；时间统一存为整数毫秒，避免浮点累计误差。
- 章节按开始时间和源序号稳定排序；无标题时显示本地化回退名“章节 N”。
- 非法章节不入库：开始/结束时间非有限值、开始时间为负、结束时间不大于开始时间、开始时间超出已知媒体时长。
- 结束时间超过已知媒体时长时夹取到媒体时长；媒体时长未知时保留 ffprobe 的合法结束时间。
- 内嵌章节是原文件派生数据，只读展示；不提供创建、编辑、删除或回写原文件的章节 API。
- 文件内容、大小或 mtime 变化导致元数据 stale 后，章节随 FR2-030 刷新；同一来源刷新采用事务内整组替换，不能让客户端看到半套章节。
- 没有内嵌章节的媒体返回空列表，不根据固定间隔自动生成“伪章节”。

### 2.2 手动书签

- 用户可在当前播放时间创建书签，必填标题，可选备注。
- 用户可编辑书签标题、备注和时间位置，也可删除书签。
- 标题去除首尾空白后长度为 `1–120` 个 Unicode 字符；备注去除首尾空白后最多 `2000` 个 Unicode 字符，空字符串规范化为 `null`。
- 书签时间使用整数毫秒；必须大于等于 `0`，媒体时长已知时不得大于时长。
- 同一媒体允许多个书签位于同一时间点，也允许标题重复；服务端生成稳定不透明 ID 区分记录。
- 书签默认按 `position_ms ASC, created_at ASC, id ASC` 返回，播放器可直接按时间渲染标记。
- 删除为物理删除书签业务行，不删除、不修改原媒体文件；删除事件仍写审计记录。
- 本期不提供批量创建、批量删除、导入导出、公共书签、多人协作或书签分享。

### 2.3 Space 单用户跨端同步

- 章节和书签 API 必须消费统一 `X-JianVideo-Space-Id` 上下文，并在 repository 层同时按 `space_id` 与 `media_id` 过滤。
- P3 仍是单用户 owner-only：书签不新增 `user_id` 或成员权限模型；同一 Space 的当前 owner 在不同浏览器/PWA 设备看到同一份书签。
- 书签真源在服务端 SQLite，不允许只写 localStorage、IndexedDB 或单设备播放器状态。
- 创建、更新或删除成功后，当前客户端立即更新并使对应查询失效；其他已打开客户端在页面重新聚焦、重新进入播放页或手动刷新时重新拉取。
- “跨端同步”不要求 WebSocket 实时推送或离线双向合并；离线写入和冲突合并留后续规格。网络失败时不得先显示为已永久保存。
- API 返回从 `1` 开始、每次成功更新后单调递增的整数 `revision`；更新和删除接口使用请求中的 `revision` 作为并发前置条件，不使用 `updated_at` 比较。记录已被其他端修改或删除时返回 `409 BOOKMARK_CONFLICT` 和当前服务端记录或删除状态，避免静默覆盖。

### 2.4 播放器交互

- 播放器进度条以不同视觉样式展示章节边界和手动书签标记，二者不得混淆。
- 章节列表展示标题和开始时间，点击后跳转到章节开始位置；只读状态应有明确语义，不出现编辑按钮。
- 书签列表展示标题、时间和可选备注；支持跳转、编辑和删除。
- 新建书签默认取用户触发时的当前播放位置；创建表单必须允许修改标题和备注，不能无标题直接保存。
- 删除操作需明确指向书签标题和时间，成功后立即从进度条与列表移除；失败时恢复原状态并显示中文错误。
- 章节或书签跳转复用 player-core 的 seek，不创建第二套播放状态。
- player-core 可维护端无关的 `Chapter` / `Bookmark` 控制状态，用于时间轴标记、当前项选择和跳转意图；章节/书签的查询、创建、更新、删除、Space 上下文、缓存失效和冲突响应仍由 `packages/media-client` 负责，player-core 不发网络请求。
- `media-client` 返回的数据由端壳映射为 player-core 输入，核心状态不得携带请求对象、查询缓存实例、HTTP 错误或鉴权信息。
- UI 只使用时间点、标题和备注；不请求、不生成、不展示章节截图或书签截图。

### 2.5 范围外

- 不编辑或写回媒体文件内嵌章节。
- 不手工创建“章节”；用户创建的是书签，不转换为章节。
- 不从语音、字幕、镜头切分或 AI 自动生成章节。
- 不生成截图、预览帧、缩略图、sprite、VTT 或 `cache_assets` 记录。
- 不做多人书签、书签可见性、评论、分享、实时推送和离线冲突合并。

## 3. 设计（怎么做）

### 3.1 数据模型

`media_chapters`：

- `id`：服务端主键。
- `space_id`：非空，关联 Space。
- `media_id`：非空，关联媒体。
- `source`：首版固定为 `embedded`。
- `source_index`：ffprobe 原始章节序号，用于稳定排序和诊断。
- `start_ms`、`end_ms`：非空整数毫秒。
- `title`：非空规范化标题，缺失时为“章节 N”。
- `language`：可空，保留可识别的章节语言标签。
- `source_fingerprint`：非空，基于媒体文件指纹和规范化章节关键字段生成，用于幂等刷新。
- `parsed_at`、`created_at`、`updated_at`。
- 唯一约束：`UNIQUE(space_id, media_id, source, source_index)`。
- 查询索引：`(space_id, media_id, start_ms, source_index)`。

`media_bookmarks`：

- `id`：服务端生成的稳定不透明 ID。
- `space_id`：非空，关联 Space。
- `media_id`：非空，关联媒体。
- `position_ms`：非空整数毫秒。
- `title`：非空，规范化后 `1–120` 字符。
- `note`：可空，最多 `2000` 字符。
- `revision`：非空正整数，创建时为 `1`，每次成功更新在同一事务内递增 `1`，作为更新和删除的并发前置条件。
- `created_at`、`updated_at`：用于展示、排序和审计，不参与并发比较。
- 查询索引：`(space_id, media_id, position_ms, created_at)`。
- 不建立 `(media_id, position_ms)` 唯一约束，允许同时间多个书签。
- 不包含 `screenshot_asset_id`、`thumbnail_url`、`image_blob` 等图片字段。

章节属于可重建派生数据；书签属于用户可信元数据，不得随缓存清理删除。P3 只要求媒体软删并进入回收站期间保留章节和书签，同时从普通媒体、章节和书签查询中不可见。媒体最终物理清理时如何级联删除章节和书签归 FR2-054/P5 定义与验收，本规格只建立其所需的 `space_id`、`media_id` 归属、外键和索引等前置数据约束。

### 3.2 章节解析与刷新

- 扩展 FR2-030 的规范化 ffprobe 解析，将 `chapters[]` 转换为章节候选。
- 优先读取 `start_time` / `end_time`；缺失时使用 `time_base` 与整数 `start` / `end` 计算，计算后统一四舍五入为毫秒。
- 标题优先读取章节 tags 中的 `title`；语言读取 `language`，其他 tags 不进入首版结构化字段。
- 对候选执行合法性校验、时长夹取、标题回退和稳定排序。
- 以 `(space_id, media_id, source)` 为刷新边界，在一个数据库事务中删除旧章节并写入新章节；解析失败时保留旧的最近成功章节并将元数据标记 stale/失败，不先删后留空。
- 章节刷新不得修改媒体原文件，也不得触发 FR2-029 预览帧或 FR2-059 封面任务。

### 3.3 API

章节：

- `GET /api/library/media/:id/chapters`
  - 响应：`{"items":[{"id":"...","source":"embedded","source_index":0,"start_ms":0,"end_ms":5000,"title":"开场","language":"zh"}],"stale":false,"parsed_at":"..."}`。
  - 无章节时返回 `200` 和空 `items`。
  - 不提供章节 POST、PUT、PATCH、DELETE 端点。

书签：

- `GET /api/library/media/:id/bookmarks`
  - 响应：`{"items":[{"id":"...","position_ms":12500,"title":"关键论点","note":"稍后复看","revision":1,"created_at":"...","updated_at":"..."}]}`。
- `POST /api/library/media/:id/bookmarks`
  - 请求：`{"position_ms":12500,"title":"关键论点","note":"稍后复看"}`。
  - 成功返回 `201` 和完整书签，初始 `revision=1`。
- `PUT /api/library/media/:id/bookmarks/:bookmark_id`
  - 请求：`{"position_ms":13000,"title":"修正后的标题","note":null,"revision":1}`。
  - 全量更新可编辑字段；服务端以 `space_id + media_id + bookmark_id + revision` 原子匹配并把 `revision` 递增 `1`，前置条件不匹配时返回 `409 BOOKMARK_CONFLICT`。
- `DELETE /api/library/media/:id/bookmarks/:bookmark_id`
  - 请求携带当前 `revision` 作为查询参数或请求体前置条件；服务端原子匹配后删除，成功返回 `204`，冲突返回 `409 BOOKMARK_CONFLICT`。

通用错误：

- `400 BOOKMARK_INVALID_POSITION`：时间非法或超出已知时长。
- `400 BOOKMARK_TITLE_REQUIRED`：标题为空。
- `400 BOOKMARK_TITLE_TOO_LONG` / `BOOKMARK_NOTE_TOO_LONG`：文本超限。
- `404 MEDIA_NOT_FOUND` / `BOOKMARK_NOT_FOUND`：当前 Space 内资源不存在。
- `409 BOOKMARK_CONFLICT`：其他客户端已修改或删除。

所有路径先执行现有认证和 Space owner 守卫。跨 Space 请求不得通过猜测媒体 ID 或书签 ID 读取、修改或删除其他 Space 数据。媒体处于软删除/回收站状态时，普通章节和书签端点按统一 `MEDIA_NOT_FOUND` 语义不可见。

### 3.4 player-core 与 media-client 边界

- `packages/media-client` 定义章节/书签 API 类型、query key、查询与 mutation，并负责携带 Space 上下文、处理 `revision` 冲突、更新缓存和触发失效重拉。
- player-core 可定义端无关的 `Chapter` / `Bookmark` 控制状态和命令，但只消费调用方已取得的数据；不得依赖 `packages/media-client`、发起请求或持有 TanStack Query 状态。
- `frontend` 壳层连接两者：把 `media-client` 结果映射为核心输入，把创建、编辑、删除意图交回 `media-client`，把章节/书签跳转意图映射为 player-core 的 seek 命令。
- 网络失败、`409 BOOKMARK_CONFLICT` 和重新拉取状态由数据层与壳层呈现，不写入 player-core 的播放错误状态，也不得伪装为媒体网络错误。

### 3.5 迁移与 backfill

- 通过 FR2-017 migration registry 新增有序迁移，创建 `media_chapters`、`media_bookmarks`、归属外键/约束和索引，并为 FR2-054/P5 的物理清理事务提供可校验的数据关联；P3 不在本规格验收物理清理级联执行。
- 迁移只改 SQLite schema，不在应用启动迁移事务中遍历媒体、不调用 ffprobe、不生成章节或书签，避免大库升级被外部 IO 阻塞。
- 历史数据库迁移后书签表为空，不从 localStorage 或观看位置猜测生成书签。
- 历史媒体章节通过幂等 backfill 补齐：
  - 现有 `media_metadata.raw_json` 已包含有效 ffprobe `chapters[]` 时，可直接规范化入库。
  - 原始元数据缺失、stale 或不含章节信息时，复用 FR2-030 `metadata.backfill` / `metadata.parse` 任务读取真实媒体。
  - backfill 按 Space 分批、有进度、可取消和重试，不阻塞普通播放。
- 重复执行 migration 或 backfill 不产生重复章节，不修改已有书签。
- v0.23.x 真实数据库 fixture 升级后，原有媒体、Space、元数据和其他业务行计数保持，新增表与索引校验通过。

### 3.6 审计

- 书签创建、编辑、删除分别写 `bookmark.created`、`bookmark.updated`、`bookmark.deleted`，包含 `space_id`、`media_id`、`bookmark_id`、时间位置和标题摘要。
- 审计不保存完整备注，避免将用户长文本或潜在敏感内容复制到事件日志。
- 章节自动解析属于派生任务结果，不为每个章节写审计事件；解析失败进入任务错误和元数据状态。

### 3.7 真实 chapter fixture

必须提交一个体积受控、许可清晰、可由真实 ffprobe 读取的媒体 fixture，而不是只提交 mock JSON：

- 建议路径：`internal/library/testdata/chapters/embedded-chapters-three.mkv`。
- 内容：短时长有声或无声视频，至少 3 个内嵌章节，时间边界固定且互不重叠；标题至少包含一个中文标题和一个 ASCII 标题。
- 期望章节：
  - `0–5000ms`：`开场`。
  - `5000–10000ms`：`Main`。
  - `10000ms–媒体结束`：`结尾`。
- fixture 旁的正式测试数据说明记录来源/生成方式、许可、ffmpeg/ffprobe 版本、生成命令、文件时长和 SHA-256；不得依赖测试运行时联网下载。
- 集成测试必须实际执行项目配置的 ffprobe 解析该文件，并断言章节数量、标题、顺序和毫秒边界；mock ffprobe JSON 只能补充错误与边界单测，不能替代真实 fixture。
- fixture 不附带章节截图，测试也不生成或持久化截图。

## 4. 任务拆分

- [ ] 新增章节与书签 schema、约束、索引和版本化迁移。
- [ ] 扩展 ffprobe 规范化解析，支持真实内嵌章节、合法性校验和事务整组刷新。
- [ ] 将章节提取接入扫描刷新和 metadata backfill。
- [ ] 实现只读章节查询 API，不注册章节写端点。
- [ ] 实现书签列表、创建、编辑、删除 API、递增 `revision` 并发冲突和 Space 隔离。
- [ ] 接入书签审计事件，不复制完整备注。
- [ ] 由 `media-client` 承担章节/书签请求、缓存失效和冲突处理，player-core 只维护端无关 `Chapter` / `Bookmark` 控制状态与 seek 意图。
- [ ] 播放器展示章节边界、章节列表、书签标记和书签 CRUD。
- [ ] 验证媒体软删期间章节/书签保留且普通查询不可见；物理清理级联留给 FR2-054/P5。
- [ ] 提交真实内嵌章节媒体 fixture 及其来源、生成方式、版本和 SHA-256 说明。
- [ ] 补单元测试：章节时间换算/校验/标题回退、书签文本与时间校验、冲突判断。
- [ ] 补集成测试：真实 ffprobe fixture、迁移、backfill 幂等、刷新失败保留旧章节、Space 隔离、审计。
- [ ] 补 E2E：章节跳转、书签创建/编辑/删除、两个浏览器上下文跨端刷新同步、软删不可见和无截图请求。
- [ ] 完成 Windows 已安装 PWA 的真实媒体 headed 验收，覆盖章节展示/跳转、书签 CRUD、刷新后持久化和冲突重拉。
- [ ] 文档同步：实现完成后更新 PRD 状态、ARCHITECTURE、API 和 CHANGELOG。

## 5. 验收标准

### 5.1 章节解析与只读边界

- 真实 chapter fixture 经项目配置的 ffprobe 解析后，返回 3 个章节，标题、顺序、开始/结束毫秒与 fixture 期望一致。
- 不含章节的真实媒体返回空列表，不自动生成固定间隔章节。
- 非法时间被拒绝，超过已知媒体时长的结束时间被夹取；标题缺失时稳定回退为“章节 N”。
- 同一媒体重复解析或 backfill 不产生重复记录；文件变化后的成功刷新原子替换整组章节。
- ffprobe 失败或刷新事务失败时保留上次成功章节并标记 stale/错误，不出现半套或意外清空。
- 路由表不存在章节创建、编辑、删除端点；前端章节 UI 不出现编辑或删除控件。
- 章节解析不修改原媒体 SHA-256 与 mtime。

### 5.2 书签 CRUD 与同步

- 在当前播放位置创建带标题、可选备注的书签后，刷新页面仍存在，进度条和列表位置一致。
- 标题空白、标题/备注超限、负时间和超出已知时长的时间均被拒绝，数据库不产生脏记录。
- 创建书签时 `revision=1`；每次成功编辑后 `revision` 恰好递增 `1`，删除后列表和进度条标记消失；`updated_at` 变化只用于展示和审计。
- 两个独立浏览器上下文模拟不同设备：设备 A 创建/编辑/删除后，设备 B 在重新聚焦或刷新播放页后读取到服务端最新结果。
- 设备 B 使用旧 `revision` 覆盖或删除设备 A 已更新的记录时返回 `409 BOOKMARK_CONFLICT`，服务端新值不被静默覆盖；测试不得使用 `updated_at` 先后关系判断冲突。
- Space A 无法读取、修改或删除 Space B 的章节和书签；非法或无权限 Space 按统一错误契约拒绝。
- 章节/书签请求、Space 上下文、缓存失效和冲突重拉均经 `media-client`；player-core 的 `Chapter` / `Bookmark` 状态不依赖网络、HTTP 或查询缓存类型，数据错误不进入播放错误状态。
- 书签创建、编辑、删除均有审计事件，事件不包含完整备注。

### 5.3 迁移与数据安全

- 空库迁移和 v0.23.x 真实数据库 fixture 升级均成功，新增表、唯一约束、外键和索引存在。
- schema migration 不调用 ffprobe、不遍历媒体文件；大库章节补齐只通过可观察、可取消、可重试的 backfill 任务执行。
- migration/backfill 重跑幂等，不重复章节、不删除或改写已有书签。
- 媒体处于回收站时章节和书签记录保留，普通媒体/章节/书签查询不可见。
- P3 只校验 FR2-054/P5 物理清理所需的归属外键、索引和事务关联前置条件，不在本规格执行或验收最终物理清理级联。

### 5.4 无截图约束

- 章节和书签 schema、API 响应、前端请求中不存在截图、缩略图或图片资产字段。
- 创建、编辑、删除书签以及章节 backfill 不调用截图/缩略图任务，不新增 `cache_assets` 记录，不写媒体图片文件。
- E2E 网络断言证明章节/书签交互期间没有为本功能发起截图、sprite 或预览帧生成请求。

### 5.5 Windows 已安装 PWA 真实媒体验收

- 在 Windows 11 实体机安装 JianVideo PWA，并从系统入口以 `display: standalone` 启动；连接真实 Go 单二进制服务，不使用 mock API 或仅内存数据。
- 导入可由项目 ffprobe 真实解析的内嵌章节媒体，确认安装态 PWA 展示真实章节边界和标题，点击章节后 player-core 定位到对应开始时间。
- 在真实媒体上创建、编辑、移动和删除书签；刷新播放页、关闭并重新启动 PWA 后，未删除书签仍从服务端恢复，时间轴标记与列表一致。
- 使用第二浏览器上下文更新同一书签后，PWA 以旧 `revision` 提交时显示 `409 BOOKMARK_CONFLICT` 对应中文提示，并重新拉取服务端当前记录，不污染 player-core 播放错误状态。
- 已安装 PWA 实跑证据记录 Windows、浏览器/PWA 版本、服务版本、媒体 fixture、操作步骤、结果及 headed 截图或录屏；该真机门不能由 jsdom、headless 或普通浏览器标签页替代。

### 5.6 质量门

- Go 单元/集成测试、真实 ffprobe fixture 测试、迁移测试、前端组件测试、Playwright 专项和项目质量门全绿。
- Go 单二进制 serve 后，真实 fixture 入库、章节展示与跳转、书签 CRUD、第二浏览器上下文刷新同步及已安装 PWA 实跑通过。

## 6. 风险 / 待定

- 已确认：内嵌章节只读解析，不提供章节编辑或原文件回写。
- 已确认：手动书签包含必填标题和可选备注，可编辑、删除，并按 Space 单用户跨端同步。
- 已确认：章节和书签都不生成截图；未来若要章节预览图，必须单独立项并复用 FR2-029/048 的可重建缓存边界。
- 容器和编码器对章节时间基准、标题 tags 的支持不完全一致，必须以真实 fixture 加错误样本覆盖，不能只依赖 mock JSON。
- 本期跨端同步是服务端真源加重新拉取，不包含实时推送、离线编辑或复杂合并；单调递增 `revision` 冲突检测用于避免静默覆盖，`updated_at` 不参与并发比较。
- 章节是派生数据、书签是用户可信元数据，两者生命周期不同；缓存清理只能影响可重建章节衍生缓存，不能删除书签。
- P3 只冻结软删期间记录保留且普通查询不可见的语义；最终物理清理级联的事务顺序与失败恢复由 FR2-054/P5 定义，但必须复用本规格建立的归属约束。
- player-core 只持有端无关 `Chapter` / `Bookmark` 控制状态，数据请求始终由 `media-client` 负责，避免把网络生命周期耦合进播放状态机。
