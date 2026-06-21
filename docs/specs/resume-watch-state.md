# 功能规格：续播与观看状态

> 状态：开发中　·　关联 PRD：FR-44　·　分支：feature/fr-44-watch

## 1. 背景与目标

为视频媒体提供「续播」与「观看状态」能力，属于 P2 播放体验增强。用户中断播放后再次进入同一视频，应从上次位置继续；视频看完后自动标记「已看」；首页提供「继续观看」区块，集中展示有进度、尚未看完的视频，方便快速回到正在追的内容。基础数据模型（`media_files.LastPosition` / `Watched` / `LastWatchedAt` 字段）已由 foundation 提交备好，本特性只补业务逻辑与 UI。

注意：本特性记录的是「用户观看位置」（每媒体一份、持久化于数据库），与既有 `internal/playback` 维护的转码/缓冲会话进度（`/api/play/:id/progress`、`/buffer`）是两回事，互不复用、互不覆盖。

## 2. 需求（要什么）

- 上报观看位置：播放中定期把当前播放位置（秒）上报后端并持久化到 `media_files.LastPosition`，同时刷新 `LastWatchedAt`。
- 续播定位：再次进入同一视频时，若 `LastPosition` 在有效区间内（>0 且距片尾足够远）则定位到该位置继续播放。
- 标记已看：视频接近播放结束时标记 `Watched=true`；标记已看时清零 `LastPosition`（已看完不再续播）。
- 继续观看列表：查询「有进度（LastPosition>0）且未看完（Watched=false）」的媒体，按 `LastWatchedAt` 倒序，供首页区块展示。
- 范围内：本地与已索引的视频媒体；单用户（无多账号区分）。
- 不做（范围外）：跨设备同步、按账号区分观看记录、图片的「观看」状态、观看历史明细/统计、自动播放下一集、清除单条/全部观看记录的管理 UI。

## 3. 设计（怎么做）

复用既有分层：`api`（HTTP）→ `library.Service`（业务）→ `db`（GORM）。观看状态作用于 `media_files` 表，归属 `library.Service`（与收藏/标签同源），不进 `playback`（避免与转码会话状态混用）。无新架构决策，不写 ADR。

### 数据模型（foundation 已建，不改结构）

- `media_files.last_position`（float64，秒）：上次播放位置。
- `media_files.watched`（bool）：是否已看完。
- `media_files.last_watched_at`（*time.Time）：最近一次观看时间，用于「继续观看」排序。

### 后端服务（`internal/library` 新增 `watch_state.go`）

- `UpdateWatchPosition(id int64, position float64) (*MediaFile, error)`：写入 `LastPosition` 与 `LastWatchedAt`；负值归零；媒体不存在报错。
- `MarkWatched(id int64) (*MediaFile, error)`：置 `Watched=true`、`LastPosition=0`、刷新 `LastWatchedAt`。
- `ListContinueWatching(limit int) ([]MediaFile, error)`：查 `last_position > 0 AND watched = 0 AND deleted_at IS NULL`，按 `last_watched_at DESC` 取前 limit 条。

### 后端端点（`internal/api/watch_handler.go`）

为避免与既有 `/api/play/:id/progress`（转码进度）混淆，观看位置端点用独立路径：

- `PUT  /api/play/:id/position`，体 `{"position": 12.5}`，返回更新后的媒体对象。
- `PUT  /api/play/:id/watched`，标记已看，返回更新后的媒体对象。
- `GET  /api/library/continue-watching?limit=N`，返回 `{"items": [...]}`（默认 limit=12，上限 50）。

### 前端（`frontend/src`）

- 类型：`MediaFile` 增 `last_position` / `watched` / `last_watched_at`（均可选，旧数据缺省）。
- `api/library.ts`：补 `updateWatchPosition` / `markWatched` / `getContinueWatching` 的 real + mock 双实现与导出（沿用 `VITE_USE_MOCK` 切换）。
- `VideoPlayer`：新增可选 `initialPosition`（挂载后 seek 一次）、`onPositionReport(pos)`（定期回调，节流 ~10s）、`onEnded()`（接近结束回调）。保持组件无网络副作用，上报由页面接。
- `PlayPage`：拉取媒体详情拿 `last_position` 传入 `initialPosition`；接 `onPositionReport` → `updateWatchPosition`，`onEnded` → `markWatched`。
- `TimelinePage`（首页 `/`）：顶部新增「继续观看」区块，调用 `getContinueWatching` 展示缩略图卡片，点击进入续播；列表为空时不渲染该区块。

## 4. 任务拆分

- [x] service 层 `watch_state.go` + 单测（上报位置、标记已看清零、继续观看查询与排序/过滤）
- [x] handler + 路由 + 端点测试（位置上报、标记已看、继续观看列表）
- [x] 前端 api 双实现 + 类型 + 测试
- [x] VideoPlayer 续播/上报/结束回调 + PlayPage 接线 + 测试
- [x] 首页「继续观看」区块 + 测试
- [x] 文档同步：PRD 状态、API.md、ARCHITECTURE.md、CHANGELOG

## 5. 验收标准

- 上报位置后 `media_files.last_position` 与 `last_watched_at` 正确更新；负值归零（service + handler 测试覆盖）。
- 标记已看后 `watched=true` 且 `last_position=0`（service + handler 测试覆盖）。
- `GET /api/library/continue-watching` 仅返回有进度且未看完的媒体，按最近观看倒序，受 limit 限制（service + handler 测试覆盖）。
- 前端：进入有 `last_position` 的视频从该位置续播；播放中定期上报位置；接近结束标记已看；首页「继续观看」区块展示并可点击进入（组件 + 测试覆盖）。
- 后端 `go test ./internal/library/... ./internal/api/...` 全绿；前端 `npm run build` 与 `npm run test` 全绿。
- 手动验收（需用户确认）：真实播放某视频中途离开，再次进入从上次位置续播；播放到结尾后该视频标记已看并从「继续观看」消失。

## 6. 风险 / 待定

- 「接近结束」阈值：剩余时长 < 一个固定秒数（如 15s）或已播 ≥ 95% 视为看完，取较宽松者，避免快进到尾导致误判；具体阈值在实现中固定为常量。
- 「续播有效区间」：`LastPosition <= 1s` 视为从头播放（忽略）；过于接近片尾的位置不回跳（由标记已看清零保证）。
- 上报频率取 ~10s 节流 + 暂停/离开时补一次，平衡持久化频次与精度；纯前端节流，不引入后端限流。
