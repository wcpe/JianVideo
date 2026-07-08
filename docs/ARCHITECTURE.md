# 架构设计：轻量级单用户视频媒体服务器

> 系统当前真貌（HOW）。始终原地更新到现状；结构 / 机制变了就改它。

## 1. 定位与边界

一款单用户私有视频媒体服务器，将分散在多个硬盘或 NAS 中的视频和图片汇聚到一个 Web 媒体库，通过浏览器直接播放所有格式的视频（不兼容格式自动转码为 HLS/TS 流），并支持图片预览和硬件加速转码降低 CPU 负载。

**边界**：
- 系统仅服务单用户，无多租户、权限管理。
- 前端编译产物通过 `go:embed` 内嵌于 Go 二进制，Web 服务器由 Go 统一承载。
- 不依赖外部数据库服务，元数据使用 SQLite（WAL 模式）本地存储。
- 不依赖外部消息队列、缓存或容器编排。
- FFmpeg / FFprobe 作为**外部进程**调用（`os/exec`），转码与探测本身不经 CGO、不内嵌编解码逻辑（见 §5.3）；CGO 仅用于 SQLite 驱动（`mattn/go-sqlite3`）与 `-tags ffmpeg` 构建下可选的硬件编码器检测（`avcodec_find_encoder_by_name`，见 §5.6）。
- 支持全部硬件加速编码器（NVIDIA NVENC、Intel QSV、AMD AMF、VAAPI、VideoToolbox、Vulkan）与软件编码，按 per-codec（H.264/H.265/AV1/VP9）逐编码实测能力（见 [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)）。
- 硬件加速能力以编码器实测为单一真源，结果按 ffmpeg 版本持久化于 SQLite（启动后台预热、可手动重测），通过 `GET /api/transcode/hwaccel` 接口暴露。

## 2. 模块与依赖

```
┌─────────────────────────────────────────────────┐
│                    main.go                       │
│  ┌───────────┐  ┌──────────┐  ┌──────────────┐ │
│  │  Web 服务  │  │ 媒体库   │  │  转码管理器   │ │
│  │ (HTTP API) │  │ 管理器   │  │              │ │
│  └─────┬─────┘  └────┬─────┘  └──────┬───────┘ │
│        │             │               │          │
│  ┌─────┴─────┐  ┌────┴─────┐  ┌─────┴───────┐ │
│  │ 认证中间件 │  │ 文件监听  │  │ FFmpeg 进程  │ │
│  │           │  │ (fsnotify)│  │  池化管理    │ │
│  └───────────┘  └──────────┘  └─────────────┘ │
│        │             │               │          │
│  ┌─────┴─────────────┴───────────────┴───────┐ │
│  │              SQLite (WAL) 元数据库          │ │
│  └───────────────────────────────────────────┘ │
│        │             │                          │
│  ┌─────┴─────┐  ┌────┴─────┐                   │
│  │ go:embed  │  │ SMB 客户端│                   │
│  │ 前端静态资源│  │ (cifs)   │                   │
│  └───────────┘  └──────────┘                   │
└─────────────────────────────────────────────────┘
```

**模块职责**：

| 模块 | 职责 | 依赖方向 |
|---|---|---|
| `web` | HTTP API 服务、静态文件服务、认证中间件 | → `library`, `transcoder` |
| `api` | API 路由注册、请求处理器（轻量委托） | → `library`, `playback` |
| `library` | 媒体库管理、目录注册、异步递归扫描与进度状态、扫描任务队列（持久化 + 单 worker 串行 + 重启恢复，FR-29）、定时扫描调度（可配置周期，FR-28）、图片/视频后缀策略、文件索引、媒体文件 CRUD、目录浏览、缩略图生成、媒体时间与 EXIF 提取（图片用 `imagemeta`，视频用 ffprobe） | → `db` |
| `playback` | 播放进度追踪、Range 请求处理、会话管理 | → `db`, `library` |
| `player` | HLS 切片写入、m3u8 索引管理、master playlist 生成 | → `library` |
| `transcoder` | FFmpeg 转码管道、多码率转码（MultiPipeline）、硬件加速检测/选择、流式输出、字幕转换（SRT/ASS→WebVTT、字幕文件查找）、转码预设存储与预生成队列（持久化 + 单 worker 串行 + 重启恢复，FR-77） | → `db` |
| `watcher` | 文件系统事件监听（fsnotify） | → `library` |
| `auth` | 单用户登录/会话管理（JWT + bcrypt） | → `db` |
| `settings` | 运行期键值设置读写（按 key 读/写、批量 upsert），为回收站、定时扫描提供配置真源 | → `db` |
| `share` | 分享链接 token 生命周期与过期（FR-43）；只管 token，资源存在性/范围判定由 api 层用 `library` 完成，无跨模块耦合 | → `db` |
| `migration` | 版本化 SQLite schema 迁移、dry-run 计划、迁移前备份、`schema_migrations` 状态、默认 Space 回填、关键索引校验与系统级审计事件（FR2-017） | → `db`, `models` |
| `db` | SQLite 数据库初始化、GORM 元数据 CRUD | 无业务依赖 |
| `config` | 配置加载（环境变量优先） | 无业务依赖 |
| `netproxy` | 后端出站 HTTP 全局可热更代理 holder（FR-80，`SetProxy`/`ProxyFunc`，原子并发安全） | 无业务依赖 |
| `dblog` | 可运行时切级别的 GORM 日志器（FR-110，`SetEnabled` 原子开关：默认安静、开启 Info 级） | 仅依赖 gorm logger |

**依赖方向**：`web` → `api` → `library` / `playback` / `player` / `transcoder` → `db`，严格单向，禁止反向。`config` 和 `auth` 为横切关注点。

### 2.1 代码目录结构

> 当前 v0.20 单体真貌。apps/packages 目标结构见 [ADR-0054](adr/0054-apps-workspace-toolchain-quality-gates.md) 与 [`docs/specs/fr2-002-workspace-toolchain-quality.md`](specs/fr2-002-workspace-toolchain-quality.md)，代码迁移完成后再回写本节。

P0.5 工作区基线已落地首批前端应用与共享包：`apps/wiki` 是独立 Vite React UI 博物馆，不依赖真实后端；它只从 `packages/ui`、`packages/theme`、`packages/mock` 与 `packages/render-pixi` 读取 UI 描述、主题、mock 场景和 PixiJS 指标入口。`packages/ui` 暴露首批 UI 预览描述与 snippet，`packages/theme` 暴露 wiki 可切换主题 / 密度配置，`packages/mock` 暴露可 seed 重建的 mock 场景。`apps/web` 不得直接依赖 `apps/wiki`，未来主端只通过 `packages/*` 共享能力。

```text
jianvideo/
├── main.go                    程序入口：装配各模块、启动 HTTP 服务
├── VERSION                    版本号唯一真源
├── go.mod / go.sum            Go 依赖清单
├── Makefile                   构建 / 测试脚本入口
├── config/
│   └── config.go              配置加载（环境变量优先）
├── internal/                  后端业务模块（禁被仓库外导入）
│   ├── api/                   API 路由注册与请求处理器（轻量委托）
│   ├── auth/                  单用户登录 / 会话（JWT + bcrypt）
│   ├── config/               运行期配置辅助
│   ├── db/                    SQLite 初始化与 GORM CRUD
│   │   └── models/            GORM 数据模型
│   ├── dblog/                 可运行时切级别的 GORM 日志器
│   ├── library/              媒体库、扫描队列、缩略图、EXIF / 时间提取
│   ├── migration/            版本化 schema 迁移、备份、dry-run 与校验
│   ├── metrics/              指标采样与持久化
│   ├── netproxy/             出站 HTTP 全局可热更代理
│   ├── playback/             播放进度、Range 请求、会话
│   ├── player/               HLS 切片写入与 m3u8 管理
│   ├── settings/             运行期键值设置真源
│   ├── share/                分享链接 token 生命周期
│   ├── smb/                  SMB(CIFS) 客户端
│   ├── transcoder/          FFmpeg 转码、多码率、硬件加速、字幕、预生成队列
│   ├── update/              自更新
│   ├── watcher/             文件系统事件监听（fsnotify）
│   └── web/                 HTTP 服务、静态资源服务、认证中间件
├── frontend/                  React + TypeScript + Vite 前端
│   ├── src/
│   │   ├── api/               后端 API 客户端
│   │   ├── components/        UI 组件
│   │   ├── pages/             页面
│   │   ├── hooks/             React hooks
│   │   ├── stores/            前端状态
│   │   ├── mocks/             MSW mock
│   │   ├── utils/ types/ data/ assets/   工具 / 类型 / 静态数据 / 资源
│   │   ├── theme.ts           主题
│   │   └── *.test.ts          前端单元测试
│   ├── public/               公共静态资源
│   └── dist/                 构建产物（`go:embed` 内嵌后即运行时真源）
├── e2e/                       端到端测试（Go 后端流程 + Playwright 浏览器）
├── scripts/                   构建 / 版本 / changelog 脚本
├── docs/                      PRD / ROADMAP / ARCHITECTURE / ADR / specs / API
└── 运行期生成（可重建、不入库）
    ├── jianvideo.db           SQLite 元数据库（WAL）
    ├── hls/                   HLS 切片输出
    ├── thumbnails/            缩略图缓存
    └── image_cache/           图片缓存
```

### 2.2 v2 API client 与 mock 先行基础（FR2-006）

`packages/media-client` 是 ADR-0054 目标工作区中的多端数据访问层，当前已落地 mock 先行切片：统一 `createApiClient` 请求入口、可配置 timeout/retry、`ApiError` 错误规范化、`X-JianVideo-Space-Id` Space 上下文头、Bearer 鉴权头、媒体分页 / 详情查询、任务详情查询、任务轮询间隔和 TanStack Query key 工厂。任务状态按 ADR-0055 使用 `pending` / `running` / `succeeded` / `failed` / `canceled`，并兼容旧状态 `completed`→`succeeded`、`error`→`failed`。

`packages/mock` 暴露纯 TypeScript `createMockFetch` 与 `handleMockApiRequest`，作为后续 MSW browser worker / 测试 server 的同源 handler 基础。当前 mock 覆盖 `/api/v2/media`、`/api/v2/media/:id`、`/api/v2/tasks/:id`，并按 `X-JianVideo-Space-Id` 过滤媒体与任务。该切片不接真实后端、不改 Go 单体运行时；真实后端接入仍归属 P2。

端能力检测由 `media-client` 的纯函数输出 `platform`（Web/Desktop/Mobile/TV/车机）、`pointer`、`touch` 与 `network`，`packages/theme` 只消费该结果决定密度，不重复探测平台。`apps/wiki` 通过独立 client demo 展示媒体列表、详情、分页、任务轮询与 Space 切换。

### 2.3 P1 PixiJS 与 Benchmark 原型包

FR2-063 当前先落在 `apps/*` + `packages/*` 工作区的原型层，不接入真实后端索引或转码管线：

| 包 / 应用 | 当前职责 |
|---|---|
| `packages/render-pixi` | 提供百万素材窗口化计算、网格 overscan、纹理池 LRU、HLS 预览触发判定、Pixi 指标快照与真实 `pixi.js` 预览挂载 API；React 侧只持有挂载点与控制态。 |
| `packages/mock` | 提供 `media-index-1m` / `media-index-5m` / `media-index-10m` 的确定性 seed 数据源与窗口查询；按位置即时生成记录，窗口查询只保留返回窗口对象。 |
| `packages/benchmark` | 提供 FR2-003 前端阈值判定、后端查询阈值判定、Go/SQLite 真实索引查询 harness 与 Markdown summary 输出；报告产物写入 `.tmp/benchmark/fr2-063/`，不入库。 |
| `apps/mock-studio` | 暴露 FR2-063 Benchmark 工作台入口、真实 PixiJS/WebGL 预览画布、Canvas 非空 E2E 验证与 HLS 预览请求计数，消费 mock 场景、render-pixi 与 benchmark 能力；headless WebGL 不可用时才退回 Canvas fallback 并在 `.tmp` 报告标注。 |

## 3. 数据模型

### 核心实体

**媒体库目录（library_paths）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| path | TEXT UNIQUE | 目录绝对路径（本地或 SMB UNC 路径） |
| type | TEXT | 目录类型：`local` 或 `smb` |
| label | TEXT | 用户自定义标签 |
| enabled | INTEGER | 是否启用（0/1） |
| created_at | DATETIME | 添加时间 |

**媒体文件（media_files）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| library_id | INTEGER FK | 所属媒体库目录 |
| file_path | TEXT, INDEX | 文件完整路径（file_path 索引加速目录浏览前缀查询） |
| file_name | TEXT | 真实文件名（与磁盘一致） |
| display_name | TEXT | 库内显示名（FR-30），空则展示回退 file_name，不影响磁盘文件名 |
| file_size | INTEGER | 文件大小（字节） |
| format | TEXT | 容器格式（mp4/mkv/avi 等） |
| video_codec | TEXT | 视频编码格式 |
| audio_codec | TEXT | 音频编码格式 |
| duration | REAL | 时长（秒） |
| width | INTEGER | 视频宽度 |
| height | INTEGER | 视频高度 |
| bitrate | INTEGER | 总码率 |
| subtitle_tracks | TEXT | 字幕轨道信息（JSON） |
| added_at | DATETIME | 入库时间 |
| modified_at | DATETIME | 文件最后修改时间 |
| favorite | INTEGER | 收藏标记（FR-41），0/1 |
| dhash | INTEGER | 感知哈希（FR-70）：基于缩略图的 64 位 dHash，0 表示未计算 |
| last_position | REAL | 上次播放位置（秒，FR-44），用于续播 |
| watched | INTEGER | 是否已看完（FR-44），0/1 |
| last_watched_at | DATETIME | 最近一次观看时间（FR-44），用于「继续观看」排序 |
| last_viewed_at | DATETIME | 最近一次打开（查看/播放）时间（FR-120），用于时间轴「最近查看」排序；区别于 `last_watched_at`（仅视频播放进度），覆盖图片 + 视频 |
| view_count | INTEGER | 观看次数（FR-75）：每「看完」一次 +1，位置上报不计数，供观看统计聚合 |
| display_name | TEXT | 系统内显示名（FR-30），空则回退 `file_name` |
| deleted_at | DATETIME, INDEX | 软删时间（FR-25）；非空表示已进回收站，源文件不动 |
| media_time | DATETIME, INDEX | 媒体时间（FR-31），多层降级解析，供时间轴排序 |
| media_time_source | TEXT | 媒体时间来源（FR-31）：exif/filename/created/modified |
| camera / lens / aperture / shutter | TEXT | EXIF 明细（FR-31）：相机/镜头/光圈/快门 |
| iso | INTEGER | EXIF 感光度（FR-31） |
| gps_lat / gps_lon | REAL | EXIF GPS 坐标（FR-31） |

> 注：本表列出 `media_files` 与当前已实现能力相关的字段。
>
> 观看状态（`last_position`/`watched`/`last_watched_at`，FR-44）记录的是「用户观看位置」，作用于 `media_files`、归属 `library` 模块，与 `playback` 模块维护的转码/缓冲会话进度是两套独立状态，互不复用、互不覆盖。
>
> 观看热力与统计（FR-75）：`view_count` 列由 `MarkWatched` 在置 `watched`/清零续播位置的**同一次 UPDATE** 内用 `view_count + 1` 原子自增——「看完计一次」，位置上报（`UpdateWatchPosition`）不计数，避免约 10s 一次的位置上报重复累加。`library.GetWatchStats()` 纯查询/聚合现有列（全程 `deleted_at IS NULL`）产出已看/未看计数、最近观看时间线（`last_watched_at` 按本地时区 `strftime` 天分桶）、续播位置热力（`last_position/duration` 比例分 10 档，`duration>0`）、各库/各格式已看分布、观看次数 Top N，经 `GET /api/library/stats` 返回，供观看统计页（`/stats`）自建（无图表库）可视化。不新建观看明细表（YAGNI、守真源不变量），统计是 `media_files` 现有列的只读派生。
>
> 软删除与回收站（FR-25）：删除媒体仅置 `deleted_at`，不物理删除记录、不删除磁盘源文件。`deleted_at` 为普通索引列（非 GORM 软删约定），故服务层在常规列表/计数手工加 `deleted_at IS NULL`（`ListMediaFilesFiltered`、`ListLibraryPathViews` 等），回收站列表查 `deleted_at IS NOT NULL`，还原清空该列。批量软删（FR-69）：`BatchDeleteMediaFiles(ids)` 在单事务内对 `id IN (?) AND deleted_at IS NULL` 一次 `UPDATE` 置 `deleted_at`，复用单条软删语义、跳过不存在/已软删 id，返回受影响行数；供时间轴与目录浏览的列表多选批量删除（进回收站、可还原）消费。
>
> 回收站清理（FR-26）：`CleanupRecycle(drivePaths)` 把全部软删项的磁盘源文件移动到其所在盘符对应的回收站目录、按 `deleted_at` 日期分子目录，移动成功后删除 `media_files` 记录（先移动成功、后删记录保证一致）。盘符→目录映射由 `api` 层从设置键 `recycle_bin_paths`（JSON）解析后传入，`library` 服务不依赖 `settings`、不解析 JSON（职责单一）。校验先行：存在任一软删项所在盘符（含 SMB / 无盘符）未配置则整体拒绝（`ErrRecycleBinPathUnset` → HTTP 409），不移动任何文件。
>
> 媒体时间与 EXIF（FR-31）：入库点 `CreateMediaFile` 统一富化元数据——图片用 `imagemeta`（纯 Go）提取拍摄时间与相机/镜头/光圈/快门/ISO/GPS，视频用 ffprobe 读 `creation_time`；再按 EXIF → 文件名日期 → 文件创建时间 → 修改时间的降级链定出 `media_time` 与 `media_time_source`。时间轴列表按 `COALESCE(media_time, added_at)` 排序。
>
> 感知哈希去重（FR-70）：`dhash` 列存基于缩略图（320 宽 JPEG）的 64 位差分哈希（dHash），由 `library` 模块纯 Go 标准库实现（缩放 9×8 灰度 + 逐行相邻像素差分，不引第三方图像哈希库），抽成纯函数 `computeDHash`/`hammingDistance`/`clusterByHamming` 便于穷举测试。`ComputeMissingDHashes` 为「未软删且 `dhash=0`」的媒体有界并发补算（缩略图缺失先同步生成一次再算，单条失败仅记 WARN 跳过），`dhash=0` 兼作「未计算」哨兵，故二次扫描跳过已算项（幂等；featureless 纯色图哈希恰为 0 会被重算，开销极小、无害）。`FindDuplicateGroups(threshold)` 查「未软删且 `dhash!=0`」的媒体，按汉明距离 ≤ 阈值（默认 10）经并查集聚类为重复组（仅返回 ≥2 项的组，组内/组间稳定有序）。图片与视频都参与（视频用其缩略图单帧，代表性弱，属已知局限）。重复项页选中多余项后复用 FR-69 批量软删（进回收站、可还原），**不新建去重持久表、不持久化「已忽略重复对」**（YAGNI、守真源不变量）。

**媒体后缀配置（media_extensions）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| library_id | INTEGER FK | 所属媒体库目录 |
| extension | TEXT | 不带点的小写后缀 |
| type | TEXT | 媒体类型：`video` 或 `image` |
| is_builtin | INTEGER | 是否内置后缀（0/1；内置后缀运行时返回，不持久化） |
| created_at | DATETIME | 添加时间 |

唯一键为 `(library_id, extension)`。删除 `library_paths` 时，服务层在同一事务中删除该目录关联的 `media_files` 与自定义 `media_extensions`。

**播放 / 转码会话（内存，非持久化表）**

播放会话**不落库**——由 `playback.Service` 以内存 map（`sessions[media_id]`，`sync.RWMutex` 保护）维护，进程重启即丢失（[ADR-0036](adr/0036-codec-negotiation.md)：避免镀金、不新建持久表）。会话结构 `models.PlaybackSession` 字段：播放位置（`current_position`）/ 时长 / 文件大小 / 缓冲区间（`buffered_ranges`），以及 FR-53 协商出的 `target_codec`（h264/h265/av1/vp9）与 `output_path`（ts/fmp4）。

> 注：早期文档曾描述持久表 `transcode_sessions`（pid/status/hw_accel 等），但该表从未落地（不在 `main.go` AutoMigrate 列表）；会话以上述内存形式为单一真源。

**用户（users）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键（单用户固定 id=1） |
| username | TEXT | 用户名 |
| password_hash | TEXT | 密码哈希（bcrypt） |
| created_at | DATETIME | 创建时间 |

**运行期设置（settings）**

| 字段 | 类型 | 说明 |
|---|---|---|
| key | TEXT PK | 设置键（如 `scan_interval`、`recycle_bin_paths`） |
| value | TEXT | 设置值，统一字符串存储；结构化值（如每盘符回收站路径）以 JSON 字符串存于单 key |
| updated_at | DATETIME | 最后更新时间 |

**Schema 迁移状态（schema_migrations）** — FR2-017

| 字段 | 类型 | 说明 |
|---|---|---|
| id | TEXT PK | 稳定递增 migration ID |
| description | TEXT | 迁移说明 |
| status | TEXT | `pending` / `running` / `succeeded` / `failed` |
| safe_to_retry | INTEGER | 是否允许失败后安全重试 |
| started_at / completed_at | DATETIME | 开始 / 完成时间 |
| error_summary | TEXT | 失败摘要 |
| validation_summary | TEXT | 校验摘要 |
| backup_path | TEXT | 本轮迁移前 SQLite 备份路径 |
| created_at / updated_at | DATETIME | 记录创建 / 更新时间 |

**Space（spaces）** — FR2-017 最小归属切片

| 字段 | 类型 | 说明 |
|---|---|---|
| id | TEXT PK | Space ID；历史数据迁入 `space-default` |
| name | TEXT | Space 名称 |
| created_at | DATETIME | 创建时间 |

FR2-017 迁移会给既有 `library_paths` 与 `media_files` 增加 `space_id`，把历史记录回填到默认 Space，并创建 `idx_library_paths_space_id`、`idx_media_files_space_id`、`idx_media_files_space_library_added`。完整成员、角色与权限矩阵仍按 ADR-0056 在后续 Space 能力中落地。

**审计事件（audit_events）** — FR2-017 最小系统级事件切片

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| scope | TEXT | `system` 或 `space`；迁移事件使用 `system` |
| space_id | TEXT NULL | `scope=system` 时为空 |
| event_type | TEXT | 事件类型，如 `migration.started` |
| migration_id | TEXT | 关联 migration ID；整轮事件可为空 |
| message | TEXT | 中文事件摘要 |
| metadata_json | TEXT | 备份路径、大小、校验结果等脱敏元数据 |
| created_at | DATETIME | 事件时间 |

**相册（albums）** — FR-40

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| name | TEXT | 相册名称（必填） |
| description | TEXT | 描述（可选） |
| cover_media_id | INTEGER | 封面媒体 ID（可选，本期不强制设置） |
| created_at | DATETIME | 创建时间 |
| updated_at | DATETIME | 更新时间 |

**相册成员（album_items）** — FR-40

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| album_id | INTEGER FK | 所属相册 |
| media_id | INTEGER FK | 关联媒体文件（可跨多个媒体库目录） |
| added_at | DATETIME | 加入时间 |

相册是物理目录之外的逻辑集合，支持跨目录手动归类媒体。唯一索引 `(album_id, media_id)` 保证同一媒体在同一相册内不重复，重复加入做幂等处理。删除相册时，服务层在同一事务中删除该相册的 `album_items`，但**不删除源文件与 `media_files` 记录**（真源不变量见 `.claude/rules`）。

**标签（tags）（FR-41）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| name | TEXT, UNIQUE | 标签名（去首尾空白后唯一） |
| created_at | DATETIME | 创建时间 |

**标签映射（tag_mappings）（FR-41）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| tag_id | INTEGER FK | 关联标签 |
| media_id | INTEGER FK | 关联媒体文件 |

媒体与标签为多对多关系，`(tag_id, media_id)` 唯一索引保证去重。按标签筛选媒体走 `tag_mappings` 子查询。

**扫描任务（scan_tasks）（FR-29）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| library_id | INTEGER, INDEX | 待扫描媒体库目录 |
| scan_type | TEXT | 扫描类型：`full` / `incremental`（当前 worker 统一按全量执行，差异留 FR-27） |
| status | TEXT, INDEX | 任务状态：`pending` / `running` / `completed` / `error` |
| scanned_files | INTEGER | 完成时记录的入库文件数 |
| total_files | INTEGER | 待扫描文件总数 |
| error | TEXT | 出错信息（status=error 时） |
| created_at | DATETIME | 入队时间 |
| started_at | DATETIME NULL | 开始执行时间 |
| completed_at | DATETIME NULL | 结束时间 |

扫描任务队列以本表为持久化真源，由单 worker 串行执行；服务重启时把残留 `running` 重置为 `pending` 重新入队（见 §5.1）。

**分享链接（shares）（FR-43；密码/限次列见 FR-78）**

| 字段 | 类型 | 说明 |
|---|---|---|
| token | TEXT PK | 加密随机不可枚举令牌（`crypto/rand` 32 字节 hex） |
| resource_type | TEXT | 分享资源类型：`media` / `album` |
| resource_id | INTEGER, INDEX | 被分享的媒体 ID 或相册 ID |
| expires_at | DATETIME NULL | 过期时间；空表示永不过期 |
| password_hash | TEXT | 访问密码的 bcrypt 哈希（FR-78）；空串=无密码，绝不存明文、不回显前端 |
| max_uses | INTEGER | 最大访问次数（FR-78）；0=无限 |
| used_count | INTEGER | 已访问次数（FR-78），实际访问资源时原子自增 |
| created_at | DATETIME | 创建时间 |

公开访问以 token 为唯一凭据，过期/撤销即失效；密码 / 限次校验、范围与安全边界见 §5.9。

**媒体健康问题（media_health_issues）（FR-73）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| media_id | INTEGER, INDEX | 关联的媒体文件 ID |
| issue_type | TEXT, INDEX | 问题类型：`broken`（视频损坏） / `zero_byte`（0 字节） / `missing`（源文件丢失） / `no_thumbnail`（缩略图无法生成） |
| detail | TEXT | 问题细节（如 ffprobe / 缩略图错误尾部） |
| checked_at | DATETIME | 本轮巡检判定时刻 |

本表是健康巡检的**只读报告快照**，独立于 `media_files`（不在其上加列）：每轮巡检先清空全表再写入当轮问题，**绝不改写 `media_files.deleted_at`**（软删真源归 FR-25/27）。巡检由独立后台 goroutine + 状态单例（`HealthScanStatus`）驱动、单飞执行，不复用扫描任务队列（见 §5.1、[ADR-0038](adr/0038-media-health-inspection.md)）。

**转码预设（transcode_presets）（FR-77）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| name | TEXT | 预设名 |
| codec | TEXT | 目标编码：`h264` / `h265` / `av1` / `vp9`（管线可参数化输出编码） |
| width | INTEGER | 目标宽度，`0` 表示沿用源宽 |
| height | INTEGER | 目标高度，`0` 表示沿用源高 |
| created_at / updated_at | DATETIME | 创建 / 更新时间 |

预设为可复用的转码模板。MVP 仅含编码与分辨率维度，不含 bitrate / 多码率档位。注意：本期 `width/height` 仅作预设定义元数据落库与展示，预生成执行只按 `codec` 预热切片、不进 ffmpeg 缩放参数（现有 `PreSliceWithCodec` 不支持任意缩放，见 §5、[ADR-0039](adr/0039-transcode-pregeneration-queue.md)）。

**转码预生成任务（transcode_tasks）（FR-77）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| media_id | INTEGER, INDEX | 待预生成的媒体文件 ID |
| preset_id | INTEGER, INDEX | 来源预设 ID |
| codec / width / height | TEXT / INTEGER | 入队时刻预设的快照（任务执行不强依赖预设此后是否被改/删） |
| status | TEXT, INDEX | 任务状态：`pending` / `running` / `completed` / `error` |
| error | TEXT | 出错信息（status=error 时） |
| created_at | DATETIME | 入队时间 |
| started_at | DATETIME NULL | 开始执行时间 |
| completed_at | DATETIME NULL | 结束时间 |

预生成队列以本表为持久化真源、单 worker 串行执行；服务重启时把残留 `running` 重置为 `pending` 重新入队（见 §5.1）。

## 4. 接口

对外接口为 RESTful HTTP API，前端通过 `go:embed` 内嵌的静态资源提供。详细契约见 `docs/API.md`。

**接口概览**：

| 分组 | 前缀 | 说明 |
|---|---|---|
| 认证 | `/api/auth` | 登录、登出、会话校验 |
| 媒体库 | `/api/library` | 目录增删、媒体文件列表、搜索、异步扫描与进度 SSE、扫描任务队列与列表（FR-29）、媒体健康巡检与问题清单（FR-73）、目录浏览（含聚合虚拟根 FR-66）、图片 raw 预览、缩略图、原文件下载（FR-42）、后缀配置（列/增/删，删自定义不删内置 FR-64）、继续观看列表（FR-44）、那年今日回忆列表（FR-72）、最近查看记录与列表（FR-120）、软删除/回收站与还原（FR-25）、批量软删（FR-69）、回收站清理（FR-26）、媒体库概览汇总（聚合总量/视频图片拆分/总大小时长/各库明细 FR-117）、媒体增长趋势（按天新增 count/size/duration，供统计页趋势 FR-118） |
| 相册 | `/api/albums` | 相册增删、跨目录成员增删与成员浏览（FR-40） |
| 播放 | `/api/play` | 视频流播放、Seek、转码控制、观看位置上报与已看标记（FR-44） |
| 转码 | `/api/transcode` | 硬件加速能力查询、转码预设 CRUD 与预生成队列入队/列任务（FR-77） |
| 配置 | `/api/config` | 系统配置读取 |
| 设置 | `/api/settings` | 运行期键值设置读取与批量写入 |
| 分享管理 | `/api/shares` | 创建/列出/撤销分享链接（鉴权后，FR-43） |
| 公开分享 | `/api/share/:token` | 免登只读访问被分享媒体/相册：元信息、图片 raw、缩略图、原文件下载、视频渐进式播放（APIGuard 豁免，经 shareAuth + 范围校验，FR-43） |

### 5.0 目录浏览

- `GET /api/library/browse`：按 `parent_path`（+ 可选 `sort`）浏览**真实路径树**，跨所有库按 `file_path` 前缀聚合（`library_id` 已弃用、仍接受但被忽略）
- 一次 SQL 查询（`file_path LIKE 'P/%'`，跨库）+ Go 层去重聚合下一级子目录
- 面包屑由后端按路径分隔符拆分构建；Windows 盘符路径保持 `D:/...` 形式，不额外加 `/D:`
- `file_path` 索引确保前缀查询性能满足 NFR-08（500ms 内响应）
- 前端 `/browse` 为资源管理器布局（FR-121）：左导航树（懒展开、自动展开当前路径祖先链）+ 可点地址栏（路径段）+ 工具栏（批量动作 + 视图模式 + 排序）+ 名称/修改日期/类型/大小详情列 + 状态栏；已移除全局页级面包屑 `PageBreadcrumbs`
- 存储库管理页（`/library-manager`）只展示存储库卡片（扫描进度 + 已索引媒体数量 + 后缀管理 FR-64），不内嵌媒体文件列表；卡片以一行 2-3 个的 `SimpleGrid` 网格布局（FR-65，`cols={{base:1,sm:2,lg:3}}`，卡内信息与操作纵向堆叠），点击卡片携起始 `path` 跳转 `/browse` 定位到该库根目录（导航按真实路径，FR-121）。`GET /api/library/paths` 每项附带 `media_count`（按 `library_id` 一次 `GROUP BY` 统计、排除软删），避免按库 N+1 计数
- **真实路径树（FR-121，[ADR-0046](adr/0046-realpath-tree-directory-browse.md) 取代 [ADR-0037](adr/0037-aggregate-directory-browse.md)）**：`parent_path` 取哨兵 `__root__` 时返回各**盘符/共享根**（由各启用库 `path` 推导卷根：本地 `D:/...`→`D:`、UNC `//host/share/...`→`//host/share`，去重排序）；其余 `parent_path` 按真实路径**跨所有库**前缀聚合（子目录 = `file_path LIKE 'P/%'` 的下一级去重、文件 = 目录恰为 P 的项，均排除软删，不再按 `library_id` 收窄）。有路径包含关系的库在公共上级自然合并为单一树（`D:\1` 与 `D:\1\2` → `D:→1→2`；库 `D:\` 则整盘可浏览）。`DirInfo.library_id` 对子目录不再填，文件保留各自 `library_id` 供删除/下载等操作；`sort`（name/size/type/time）服务端排序。库注册真源 `library_paths`、媒体真源 `media_files` 不变

## 5. 关键机制

### 5.1 媒体库扫描与后缀策略

- 本地目录注册时必须校验路径存在且为目录，入库前转为绝对路径并统一为正斜杠。
- `ScanLibraryWithType(libraryID, dirPath, dirType, mode)` 按 `LibraryPath.type` 分发：`local` 使用 `filepath.WalkDir` 递归扫描，`smb` 使用 SMB 客户端遍历共享目录。
- 扫描模式（FR-27）：`mode=incremental`（增量更新，缺省）只索引新增文件；`mode=full`（全量扫描）在入库后追加对账——以本次遍历到的现存路径集合为基准，库内未软删（`deleted_at IS NULL`）且不在集合中的记录经一条 `UPDATE` 标记软删进回收站（复用 FR-25），不物理删除、不动磁盘。遍历整体出错时放弃对账以免误删；对账仅本地扫描启用，SMB 轮询为增量语义（远程列举不保证完整）。
- 媒体识别统一由 `library.Service` 维护：内置视频后缀和图片后缀始终可用，自定义后缀通过 `media_extensions.library_id` 绑定到单个 `LibraryPath`。
- 扫描入库按 `library_id + file_path` 去重，重复扫描不会重复写入。
- 图片文件可通过 `GET /api/library/media/:id/raw` 提供本地预览；视频文件继续走播放链路。HEIC/RAW（cr2/nef/arw/dng/rw2 等）浏览器无法直接渲染，`raw` 端点经外部 ImageMagick（`magick`）转成 JPEG 后返回，结果缓存于数据目录下 `image_cache/`（按「源路径 + 源修改时间」hash 命名，二次命中不重转）；magick 不可用返回 `503`、转换失败返回 `500`，均记中文日志（FR-37，见 ADR-0030）。
- 异步扫描：`POST /api/library/scan/:id` 经 `Service.StartAsyncScan` 在后台 goroutine 执行，接口立即返回不阻塞主线程；进度由 `scan_status.go` 维护的全局 `ScanStatus`（`sync.RWMutex` 并发安全，同一时刻仅跟踪一个扫描任务）记录，经 `GET /api/library/scan/progress` SSE 端点每 500ms 推送，`completed`/`error` 后关闭连接。`ScanLibraryWithType` 仍保留同步签名供 watcher 与扫描队列调用。

### 5.1.2 扫描任务队列（FR-29）

- `library.TaskQueue`（`task_queue.go`）以 SQLite `scan_tasks` 表为持久化真源，单 worker goroutine 串行执行入队任务。触发扫描（`POST /api/library/scan/:id`）改为 `Enqueue` 建 `pending` 任务，立即返回任务 ID；worker 取最早 `pending` 置 `running`、调注入的执行函数（`Service.ScanLibraryWithType`，函数注入便于测试替身、不在队列重写扫描逻辑），按结果置 `completed`（记 `scanned_files`）或 `error`（记错误）。
- 串行调度用条件信号（容量 1 的 channel）唤醒 worker，队列空时阻塞等待不空转；扫描执行（高开销 IO）在锁外。扫描目标参数（path/dirType）为过程态，存内存映射不入库（path 真源仍是 `library_paths`）。
- 重启恢复：启动时 `RecoverRunning()` 把残留 `running` 任务重置为 `pending`（按 `library_id` 反查目录重建执行目标）后再启动 worker 重新执行；目录已失效的任务标记为 `error` 而非永久卡 `pending`。
- 进度桥接：因单 worker 串行，全局 `ScanStatus` 始终对应当前 `running` 任务；`GET /api/library/scan/tasks` 把实时 `ScanStatus` 的 `scanned_files`/`total_files` 覆盖到 `running` 任务返回，已完成任务用其持久化进度，避免 worker 每 tick 写库。前端页眉据此常驻展示进行中任务。

### 5.1.3 定时扫描调度（FR-28）

- `library.ScanScheduler`（`scheduler.go`）是纯定时组件，经函数注入解耦（周期 `intervalFn`、触发 `triggerFn`），便于无依赖的 `-race` 单测；不重写扫描执行与入队，到点经 FR-29 队列入队即可。
- 周期来自 `settings.scan_interval`（秒，FR-24 真源），`<=0`/非法视为关闭。调度循环每轮先读周期：关闭则阻塞等待重载/停止（不空转），否则 `time.NewTimer(周期)` 到点触发；等满一个周期才首次触发（不在启动/重启时立即扫，避免扫描风暴）。
- 触发动作：枚举启用媒体库经 `TaskQueue.EnqueueScheduled` 入队**增量**扫描，逐库跳过禁用库与已有活动（`pending`/`running`）任务的库以防积压。定时只做增量；全量对账经手动 `mode=full` 触发（FR-27）。
- 热生效：设置页保存 `scan_interval` 后，`PUT /api/settings` 经 Handler 的设置变更回调调用 `ScanScheduler.Reload()` 非阻塞唤醒循环重排，新周期即时生效、无需重启。

### 5.1.4 媒体健康巡检（FR-73）

- `library.HealthService`（`health.go`）后台只读巡检全部未软删媒体，问题写入独立的 `media_health_issues` 表（见 §3）。**不复用 `TaskQueue`**：巡检是单次全局操作、每轮清空重写问题表，语义与「按库排队的扫描任务」不同；改用独立后台 goroutine + 单飞标志 + 状态单例 `HealthScanStatus`（`health_status.go`，结构参照 `ScanStatus`），决策见 [ADR-0038](adr/0038-media-health-inspection.md)。
- 触发：`POST /api/library/health/scan` 调 `StartScan()` 起一轮后台巡检；已有巡检在跑（`running`）时直接返回不并发第二轮。进度经 `GET /api/library/health/status` 查询（`idle`/`scanning`/`completed`/`error` + `total`/`checked`/`issue_count`），问题清单经 `GET /api/library/health/issues` 查询（附媒体基本信息）。
- 判定核心 `classifyMediaIssues(mf, checks)` 是无副作用纯函数（外部依赖 ffprobe / 文件系统 / 缩略图生成经函数注入，便于穷举单测），逐项判：0 字节（`file_size==0`）、源文件丢失（`os.Stat` 失败，**排除 `smb://`** 远程路径不误判）、视频损坏（`ProbeVideoHealth` 跑 ffprobe 判可解析性）、缩略图无法生成（`TryGenerateThumbnail` 同步生成捕获错误）。`ProbeVideoHealth` 与 `TryGenerateThumbnail` 是**新增**入口、返回 ok/err，**不改动** `probeVideoMetadata` 的元数据静默降级与异步 `GenerateThumbnail` 的只记日志行为（两者语义不同）。
- 一致性：每轮巡检在单事务内先清空 `media_health_issues` 再写入当轮快照；**全程只读 `media_files`、绝不改 `deleted_at`**（软删真源归 FR-25/27）。删除问题媒体复用 FR-69 批量软删端点，不在巡检里删除。

### 5.1.1 缩略图生成

- `thumbnail.go` 提供缩略图能力：入库时对新文件异步调用 `GenerateThumbnail`，视频取第 2 秒帧、普通图片缩放为目标宽，统一经 ffmpeg 生成；HEIC/RAW 改经外部 ImageMagick（`magick`）缩放生成缩略图（FR-37）。生成失败仅记日志不阻塞入库。
- **透明源中性灰底（FR-81 P1）**：带 alpha 的源（透明 PNG / 部分 WEBP / HEIC 等）若直接压 JPEG，透明区会被默认黑底合成纯黑。ffmpeg 路径以 `color=0x808080` 经 `scale2ref`+`overlay` 把图像合成到中性灰底；magick 路径以 `-background #808080 -flatten` 刷灰后再缩放。无 alpha 的源叠加灰底不可见、结果不变。灰底色值由常量 `thumbnailMatteColor` 统一定义。
- **多尺寸缓存（FR-81 P12）**：受支持尺寸白名单 `{160,320,640}`（默认 320）。`thumbnailPathForSize` 按尺寸映射缓存路径——**默认尺寸 320 保持历史命名（无后缀），非默认尺寸用 `<hash>_<size>.jpg` 与默认产物并存**；故 dHash 去重（FR-70）与健康巡检（FR-73）读取的固定缩略图路径不变。
- 缩略图存于数据目录下 `thumbnails/`（启动时 `InitThumbnailDir` 初始化，按原始路径 SHA-256 hash 命名避免特殊字符冲突）。
- `GET /api/library/thumbnail/:id` 返回缩略图，支持 `size` 查询参数按列宽请求多尺寸（非白名单值回落默认 320）；对应尺寸尚未生成时返回 `202` 并触发后台生成，前端可稍后重试。前端 `MediaThumbnail` 以 `object-fit:contain` + 中性背景容器按原比例自适应显示（竖图/正方图完整不裁，FR-81 P3），缺图先显骨架占位、命中 202 时短间隔轮询重载、加载失败显降级占位（FR-81 P14），并经 `srcset`/`sizes` 按列宽请求更小尺寸。图片预览弹窗仍用原图。

### 5.2 文件监听与增量更新

- 使用 `fsnotify` 对已注册本地目录进行递归监听，SMB 目录使用 5 分钟轮询扫描。
- watcher 只调用 `library.Service` 上报新增/删除事件，不直接操作 DB，保持 `watcher → library → db` 单向依赖。
- Create/Write 事件先根据所属 `library_id` 调用统一媒体后缀策略判断，支持图片、视频和该目录自定义后缀。
- 事件去抖：文件写入完成后（连续 500ms 无新事件）才触发入库，避免读取不完整文件。
- Remove/Rename 事件按路径委托 library 删除对应索引。

### 5.3 转码管道与流式输出

- FFmpeg 作为**外部进程**调用（`os/exec` 启动 `ffmpeg`/`ffprobe`），转码本身不经 CGO；CGO 仅用于 SQLite 驱动与可选的硬件编码器检测（见 §5.6）。转码引擎不内嵌编解码逻辑（架构不变量）。
- 转码管道：以参数化命令行启动外部 `ffmpeg`，由其完成解码→缩放→编码→（mpegts/HLS）输出。
- 编码输出经 ffmpeg stdout 与 HLS 切片文件提供，实时流式传输给客户端。
- 每个转码会话运行在独立 goroutine，通过 context.Context 管理生命周期。
- Seek 时 cancel 旧 context（终止旧 ffmpeg 进程），启动新进程定位到目标位置。
- 硬件加速编码器清单（按 per-codec 实测，见 §5.6 / [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)）：

| 平台 | H.264 编码器 | H.265 编码器 | 设备类型 |
|---|---|---|---|
| NVIDIA GPU | `h264_nvenc` | `hevc_nvenc` | `cuda` |
| Intel QSV | `h264_qsv` | `hevc_qsv` | `qsv` |
| AMD AMF | `h264_amf` | `hevc_amf` | `d3d11va` |
| VAAPI (Linux) | `h264_vaapi` | `hevc_vaapi` | `vaapi` |
| VideoToolbox (macOS) | `h264_videotoolbox` | `hevc_videotoolbox` | `videotoolbox` |
| Vulkan | `h264_vulkan` | `hevc_vulkan` | `vulkan` |
| 软件兜底 | `libx264` | `libx265` | — |

- 上述各家族另探测 AV1（`av1_nvenc`/`av1_qsv`/`av1_amf`/`av1_vaapi`/`av1_vulkan`/`libsvtav1`）与 VP9（`vp9_qsv`/`vp9_vaapi`/`libvpx-vp9`）等高级编码并如实展示。
- **转码目标编码可配置（FR-50，见 [ADR-0034](adr/0034-configurable-target-codec.md)，扩展 ADR-0003 而非推翻）**：服务端以 `settings` 表持久化「首选目标编码优先级」（键 `transcode_codec_priority`，JSON 数组，如 `["av1","h265","h264"]`），写入时按 FR-49 实测可输出集校验。单/多码率管道按所选编码参数化输出——编码器名由 `SelectEncoderForCodec(results, codec)` 选取，像素格式与关键参数由纯函数 `CodecOutputParams(codec)`（h264/h265/av1/vp9）映射，替代原先硬编 H.264。**默认仍 H.264**（未配置 / 非法回落 `["h264"]`，`NewPipeline()` 等价 `NewPipelineForCodec("h264")`，参数字节级不变，mpegts.js 可播）。
- **播放/转码输出分发（FR-51，见 §5.4）**：`SelectOutputPath(codec)` 纯函数按目标编码分发——`h264`（含空、未知）走现有 **MPEG-TS/HLS** 路径（mpegts.js 内核，分支实现不动）；`h265`/`av1`/`vp9` 走新增的 **fMP4/CMAF + 原生 MSE** 路径（见 [ADR-0035](adr/0035-fmp4-mse-playback-path.md)）。fMP4 编码器经 `SelectFMP4Encoder` 复用 §5.6 实测快照按硬件优先级选取，无硬件可用时软件兜底（`libx265`/`libsvtav1`/`libvpx-vp9`）。MPEG-TS 无 AV1/VP9 标准流类型（AV1 over TS 被 ffprobe 探测为 `bin_data`），故高级编码统一走 fMP4 路径；端到端可播仍需前端自适应播放器（FR-52）与协商（FR-53）。
- Intel 核显检测：通过 sysfs（`/sys/class/drm/card0/device/vendor` = `0x8086`）+ 驱动名（`i915`/`xe`）+ 无独立显存确认核显身份。
- 硬件检测优先级：CUDA → QSV → VAAPI → D3D11VA → DXVA2 → VideoToolbox → Vulkan → 软件。
- 硬件加速失败时自动降级，不中断播放。

### 5.4 HLS 切片与追播

- 转码输出同时写入 HLS 切片文件（`.ts`）和内存管道。
- mpegts.js 通过 HTTP Range 请求获取最新切片数据。
- 播放器轮询 m3u8 索引文件，检测到新切片时自动追加到 MSE 缓冲区。
- 追播延迟控制：播放器保持 3-5 秒的缓冲距离。
- 以上为 **H.264 路径**（[ADR-0003](adr/0003-hls-ts-streaming.md)/[ADR-0004](adr/0004-mpegts-js-player.md)，播放内核锁定 mpegts.js，本路径承载追播/边下边播/Seek，不变）。

#### 5.4.1 高级编码 fMP4/CMAF 输出路径（FR-51）

- **扩展而非取代播放内核**：H.264 维持 mpegts.js + MPEG-TS + HLS 不动；非 H.264（H.265/AV1/VP9）走新增的 **fMP4/CMAF 分片 + 浏览器原生 MSE** 路径（见 [ADR-0035](adr/0035-fmp4-mse-playback-path.md)）。fMP4 容器不是 TS，不触犯「禁止用原生 video/MSE 播 TS 流」红线，且与 FR-16「不走 TS 的直出场景允许原生 video」自洽。
- **产物**：`RunFMP4ToDir` 用外部 ffmpeg（`-f hls -hls_segment_type fmp4 -hls_playlist_type vod`）产出 HLS-fMP4（CMAF）——init segment `init.mp4`（含 `moov`）+ media segments `seg_NNN.m4s`（`moof`+`mdat`）+ 清单 `index.m3u8`（`EXT-X-MAP` + `EXTINF` + `EXT-X-ENDLIST`，VOD）。强制 8-bit `yuv420p`、固定 GOP（与 TS 路径同策略）；HEVC 打 `-tag:v hvc1`（Safari/MSE 兼容）；音频统一转 AAC（fMP4 不能 copy 裸流任意编码）。
- **前端契约（FR-52 消费）**：容器 fMP4/CMAF；codec MIME 串由 `FMP4CodecMIME` 给出（H.265 `video/mp4; codecs="hvc1.1.6.L93.B0"`、AV1 `av01.0.05M.08`、VP9 `vp09.00.10.08`）；URL 沿用 `/api/play/hls/{mediaID}/{index.m3u8|init.mp4|seg_NNN.m4s}`（静态服务，`.m4s`→`video/iso.segment`、init `.mp4`→`video/mp4`）。
- **追播边界**：本路径仅 **VOD（转码完成后整段播放）**，**不**实现实时追播/边下边播（实时 fMP4 追播复杂度高、属前端协同与后续阶段，不在本期）。H.264 路径追播能力不受影响。
- **与 FR-50 重叠点**：本路径接收一个「目标编码」入参产出对应 fMP4；目标编码优先级的持久化设置属 FR-50，整合时由 FR-50 设置层产出目标编码喂入。

### 5.5 多码率自适应（ABR）

- `MultiPipeline` 使用 FFmpeg filter_complex split 单进程多输出，同时生成 1080p/720p/480p 三档 HLS。
- 码率阶梯根据源分辨率自动裁剪（<720p 只输出 480p+720p，<1080p 不输出 1080p）。
- 所有码率共享同一 GOP（-g 48 -keyint_min 48 -sc_threshold 0），确保切换时画面连续。
- 切片文件名包含码率标识（如 `1080p_segment_000.ts`），m3u8 命名为 `{quality}.m3u8`。
- `master.m3u8` 包含 `EXT-X-STREAM-INF` 标签，描述各码率流的 BANDWIDTH/RESOLUTION。
- 前端 hls.js 动态 import，自动选择最佳码率；不支持 hls.js 时回退 mpegts.js。
- 详见 [ADR-0026](adr/0026-abr-adaptive-bitrate.md)。

#### 5.5.1 前端客户端能力探测 + 自适应播放器（FR-52）

- **客户端能力探测**（`frontend/src/utils/codec-capability.ts`，纯函数）：以 `MediaSource.isTypeSupported` 探测本浏览器在 fMP4 容器下可解码哪些高级编码。`codecMIME(codec)` 给出与后端 `FMP4CodecMIME`（§5.4.1）字节级一致的 MSE codec MIME 串（H.265 `hvc1.1.6.L93.B0` / AV1 `av01.0.05M.08` / VP9 `vp09.00.10.08`，真源在后端，前端为消费副本）；`isCodecSupported(codec)` 归类单编码（无 MIME 串 / 无 `MediaSource` 时 false）；`probeClientCapabilities()` 返回 `{h265,av1,vp9}` 能力描述，供 FR-53 协商上报。
- **自适应播放器**（`VideoPlayer` 扩展）：新增可选「播放描述符」入参 `PlaybackDescriptor{codec,url,path,fallbackUrl?}`，纯函数 `resolveDescriptor` 按 `path` 分发到对应内核——`ts`（H.264，含 master.m3u8 ABR）走现有 mpegts.js / hls.js-ABR 分支（**追播路径与实现一字不动**）；`fmp4`（H.265/AV1/VP9）走 hls.js 原生 fMP4+MSE 加载 `index.m3u8`（hls.js 原生支持 fMP4 分片，复用 §5.5 同一 HLS 内核）；`mp4` 走原生 video。**缺省描述符时行为与现状字节级一致**（现有调用方零改动）。
- **不支持回退**：分发到 fmp4 前先 `isCodecSupported(codec)` 校验，为 false 时——有 `fallbackUrl` 则回退按 TS 路径加载（mpegts.js，不抛 Network Error）；无回退源则展示「当前浏览器不支持该视频编码」提示而非报错。
- **本 FR 复用 ADR**：复用 [ADR-0035](adr/0035-fmp4-mse-playback-path.md)（fMP4/CMAF + 原生 MSE，已明确前端用 hls.js 消费 HLS-fMP4）与 [ADR-0026](adr/0026-abr-adaptive-bitrate.md)（hls.js 作为 HLS 内核），无新 ADR。
- **端到端边界**：「按客户端能力实际选编码 + 触发后端产 fMP4 + PlayPage 接线 + 会话记录」属 FR-53；本 FR 仅交付能力探测函数 + 播放器按描述符分发 + 不支持回退，PlayPage 的 URL 选择逻辑不变。fMP4 路径前端按 VOD 加载（FR-51 已定不实时追播）。

#### 5.5.2 端到端编码协商（FR-53）

把 FR-49/50/51/52 串通：**播放发起时做一次服务端协商，按「首选优先级 ∩ 客户端能力 ∩ 实测可产出」选出实际输出编码与播放路径**（决策见 [ADR-0036](adr/0036-codec-negotiation.md)）。

- **协商纯函数**（`transcoder.ChosenCodec(priority, clientCaps, producible)`）：返回首选优先级里第一个同时满足「客户端支持」`clientCaps[c]` 且「实测可产出」`producible[c]` 的编码；都不满足兜底 `h264`（恒可用底座，现有 mpegts.js+TS 路径保证可播）。编码标识大小写无关、`hevc`→`h265`。可穷举单测。
- **协商端点**（`POST /api/play/:id/negotiate`）：请求体携带客户端能力 `{h265,av1,vp9}`（来自 §5.5.1 探测）。读 FR-50 优先级（`settings.TranscodeCodecPriority`）+ FR-49 可产出并集（`capability.Capabilities().Codecs`）+ 客户端能力 → `ChosenCodec`。非 `h264` 同步调 §5.4.1 `PreSliceWithCodec` 产 fMP4，**产出失败降级回 `h264`/TS**（不报错，保证可播）。返回播放描述符 `{codec,path(ts/fmp4),url,mime,fallback_url}`（`BuildNegotiationDescriptor`，URL 相对路径，前端绝对化）。
- **前端接线**（`PlayPage`）：加载媒体后 `probeClientCapabilities()`（§5.5.1）→ `negotiate` → 协商出 fMP4 则把描述符交 §5.5.1 自适应播放器（hls.js 原生 MSE 播 AV1/HEVC/VP9）；协商出 `h264` / 协商失败则沿用既有 master 探测 → mpegts/mp4 路径（H.264 回退，不报错）。
- **会话记录实际编码与路径**：`playback.Service.RecordNegotiation` 把协商结果记到内存播放会话（`PlaybackSession.TargetCodec`/`OutputPath` 两字段）。**注**：本项目当前播放会话仅在内存（FR-12 Range 跟踪），不持久化；ARCHITECTURE §3 历史描述的 `transcode_sessions` 持久表代码从未落地，本 FR 不新建该表（避免镀金）。
- **复用 ADR**：复用 [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)（实测真源）、[ADR-0034](adr/0034-configurable-target-codec.md)（可配置目标编码）、[ADR-0035](adr/0035-fmp4-mse-playback-path.md)（fMP4/MSE 路径）、[ADR-0026](adr/0026-abr-adaptive-bitrate.md)（hls.js）；协商点 + 端点契约为本 FR 新增决策。
- **真机验证**：软件 AV1 端到端——设首选 `["av1","h264"]` + Chrome（支持 AV1）→ 协商出 av1 → 后端 `libsvtav1` 产 fMP4 → hls.js + 原生 MSE 实播（`videoWidth=640` 解码出帧、`readyState=4`、无 error）；H.264 客户端 → 协商出 `h264`/TS 走 mpegts.js。硬件 AV1/QSV/NVENC 端到端待对应硬件。

#### 5.5.3 转码预设与预生成队列（FR-77）

把「按需首播切片」前移为「手动预热」：用户定义可复用预设（编码 + 分辨率），把媒体加入预生成队列后台串行预转码，产出切片缓存到 `hlsDir/{mediaID}/`，消除首播冷启动等待。决策见 [ADR-0039](adr/0039-transcode-pregeneration-queue.md)。

- **预设存储**（`transcoder.PresetStore`，`preset_store.go`）：`transcode_presets` 表的 CRUD + 校验。编码白名单复用 §5.3 `CodecOutputParams`（h264/h265/av1/vp9，`hevc` 归一化为 `h265`）；空名 / 不支持编码 / 负分辨率整体拒绝不落库。职责单一，不承载队列/转码。
- **预生成队列**（`transcoder.PregenQueue`，`pregen_queue.go`）：**完整复用 FR-29 任务队列范式**——以 `transcode_tasks` 表为持久化真源、单 worker goroutine 串行执行、条件信号唤醒不空转、`RecoverRunning()` 重启把残留 `running` 重置为 `pending` 重新入队。落 transcoder 包以保持依赖单向（exec 直接调本包 `PreSliceWithCodec`，无需反向依赖 library）。任务入队时快照预设 `codec/width/height`，使执行不强依赖预设此后是否被改/删。
- **执行**：exec 函数经注入（便于单测替身）。`main.go` 生产实现为闭包：按 `media_id` 经 `library.GetMediaFileByID` 反查媒体路径，再调 §5.4.1 `PreSliceWithCodec(ctx, mediaID, path, w, h, codec, hlsMgr, hlsDir)` 同步切片（已存在切片则复用）。
- **`width/height` 局限**：现有 `PreSliceWithCodec` 不支持任意分辨率缩放（TS 路径按源分辨率选码率档位、fMP4 路径不缩放），故本期预设 `width/height` 仅落库为元数据、不进 ffmpeg 缩放参数。真正缩放需扩 `PreSliceWithCodec`，另立 FR（见 ADR-0039 后果）。
- **接线**：预设 CRUD `GET/POST/PUT/DELETE /api/transcode/presets`、入队 `POST /api/transcode/tasks{media_id,preset_id}`、列任务 `GET /api/transcode/tasks?status=`（见 §4）。前端转码预设页 `/transcode`（列/建/改/删 + 轮询任务列表）+ 播放页「加入预生成」入口（`PregenDialog` 选预设入队）。

### 5.6 硬件加速管理

- 硬件加速能力以**编码器实测**为单一真源（见 [ADR-0033](adr/0033-hwaccel-probe-source-cache.md)，取代 ADR-0015）：对各家族 × 各编码（H.264/H.265/AV1/VP9）候选用外部 ffmpeg 跑一小段试编码（`-f lavfi … -f null`，VAAPI/Vulkan 另带设备初始化），判定「编入 / 试编码成功」。
- 实测结果按 **ffmpeg 版本为键持久化于 SQLite**（`codec_probe_caches` 表）：版本不变复用缓存、版本变化失效重测，提供手动「重新测试」；启动时后台 goroutine 预热（不阻塞、单航道防重复实测）。
- 能力模型为 **per-codec**：每家族逐编码记录可用性，家族「可用」= 至少一编码试编码成功（不再强制 H.264 与 H.265 同时可用）。Intel 核显另辅以 sysfs 识别（仅 qsv 实测可用或 sysfs 确认才标记）。
- 转码选码 `SelectBestEncoder()` 读实测快照按硬件优先级（NVENC→QSV→AMF→VAAPI→Vulkan→VideoToolbox）选 H.264 编码器，冷态（预热未完成）软件兜底；实际编码仍由外部 ffmpeg 进程完成。失败自动降级，不中断播放。
- `-tags ffmpeg` 构建下另保留 CGO `avcodec_find_encoder_by_name` 低层检测（ADR-0013/0014），不在 HTTP/选码热路径。

### 5.7 系统诊断与编解码器实测（FR-21）

- `GET /api/system/info`：返回 OS/架构/CPU 数/主机名/Go 版本/应用版本（构建期 `-ldflags -X main.version` 注入）、ffmpeg 可用性与版本，并复用 §5.6 的硬件加速实测能力（per-codec）。
- `POST /api/system/codec-test`：对候选编码器（软件 + QSV/VAAPI/NVENC/AMF/VideoToolbox/Vulkan 的 H.264/H.265/AV1/VP9）用**外部 ffmpeg 跑一小段试编码**（`-f lavfi … -f null`），报告「是否编入当前 ffmpeg / 试编码是否成功 / 失败尾部」。默认读 §5.6 持久化缓存即时返回，`?force=true` 强制重测；响应附 `from_cache`/`ffmpeg_version`/`tested_at`。与 §5.6 同源（实测即真源）。
- `GET /api/system/env`（FR-56）：只读返回项目已知环境变量清单（key/中文用途/是否敏感/是否已设置/展示值），元数据表集中维护于 api 层（涵盖根 config 与 `internal/config` 两套来源）。**敏感项（`JWT_SECRET`、`SMB_MASTER_PASSWORD`）绝不回显明文**——`value` 固定掩码、只暴露 `set` 布尔（安全红线）；非敏感项 `value` 为 `os.Getenv` 明文。env 为进程级，只查看不修改。
- `POST /api/system/ffmpeg/detect`（FR-56）：对请求体 `path`（空=测当前）跑 `path -version` 验证可用性，基于纯函数 `transcoder.CheckFFmpegPath`（**不改写运行期全局路径**），返回 `{ffmpeg_available, ffmpeg_version}`，供用户保存前先验路径；持久化走 §5.8 的 `ffmpeg_path`/`ffprobe_path` 设置键。
- 前端在 `/system` 控制台页展示（硬件加速卡片按家族 × 编码逐项展示、标示缓存来源与「重新测试」）并支持一键复制纯文本报告。控制台页（`ConsolePage`，FR-113 取代 FR-55 两级结构）以 Mantine `Tabs` 拍平为**一级 tab**——运行环境 / 硬件加速 / 编解码 / 应用更新 / 设置（设置即 §5.8 的 `SettingsPage`），tab 状态由 URL query `?tab=env|hwaccel|codec|update|settings` 控制；`ConsolePage` 把前四项作为 `section` 传给 `SystemPage`（其只渲染对应区块、不再有内层 tab），第五项渲染 `SettingsPage`。**向后兼容旧深链**：`?tab=system&sys=<x>` 归一为对应一级 tab（`sys` 缺省落 env）、`?tab=settings` 落设置。每个含多区块的 tab（运行环境、设置）内由通用组件 `AnchorNav` 提供左侧锚点导航：`IntersectionObserver` 观测各区块、滚动高亮当前，点击 `scrollIntoView` 平滑定位。应用更新区块（FR-112 扩 FR-62/FR-46）合并为**单个「检查更新」按钮**（点击 = `force` 强制直连重查）；进入仅同步回填该频道本地缓存（`loadCachedUpdate`）——有缓存直接显示缓存版本与发布说明、无缓存则不显示更新区，**进入不自动联网**（去掉原后台 force=false 刷新）；执行更新成功后 `clearCachedUpdate` 清该频道缓存（下次进入需用户重新点检查更新强制重拉）。页眉 `UpdateIndicator`（FR-58）仍独立调 `checkUpdate(channel,false)`、不读本地缓存键，故缓存读写契约变化不影响其工作。

### 5.8 运行期设置（FR-24）

- 设置以 SQLite `settings` 表为唯一真源，由 `settings.Service` 封装读写：`Get`/`GetAll`/`Set`/`SetMany`，写入走主键冲突 upsert，批量写在单事务内原子完成（详见 ADR-0029）。
- `GET /api/settings` 返回全部键值（map 形式），`PUT /api/settings` 批量 upsert 并回读返回；前端在 `/system` 控制台页的「设置」tab（`?tab=settings`，FR-55）读写「每盘符回收站路径」「扫描周期」「工具路径（ffmpeg/ffprobe/magick）」等键值，旧 `/settings` 路由重定向至此。
- 已知键以常量集中定义（`recycle_bin_paths`/`scan_interval`/`update_channel`/`transcode_codec_priority`/`ffmpeg_path`/`ffprobe_path`/`magick_path`/`network_proxy`），结构化值以 JSON 字符串存于单 key，由消费方按需解析：回收站清理（FR-26）读 `recycle_bin_paths`（盘符→目录 JSON）解析后传给 `library.CleanupRecycle`；定时扫描读 `scan_interval`。
- **FFmpeg 路径持久化设置（FR-56）**：`ffmpeg_path`/`ffprobe_path` 让 ffmpeg/ffprobe 路径运行期可配置。`main.go` 启动时先 `resolveTool` 注入（环境变量→同目录捆绑版→PATH），随后若设置非空则覆盖（**持久化设置优先于自动发现**）；`PUT /api/settings` 含这两键时落库后即时调 `transcoder.SetFFmpegPath`/`SetFFprobePath`（ffprobe 同步给 `library`）应用到运行期，保存即生效、无需重启（api→transcoder 依赖方向允许）。
- **Magick 路径持久化设置（FR-63）**：`magick_path` 让 ImageMagick magick 路径运行期可配置，机制与 FR-56 完全一致——`main.go` 启动时 `resolveTool("JIANVIDEO_MAGICK_PATH", "magick")` 注入后若设置非空则覆盖；`PUT /api/settings` 含 `magick_path`（非空）时落库后即时调 `library.SetMagickPath` 应用到 HEIC/RAW 转换运行期（FR-37），保存即生效、无需重启。`library.magickPath` 与 transcoder 路径全局同为无锁包级变量，写入点仅限启动注入与 PUT 应用，沿用既有并发模型。启动期项（端口/DB 路径/debug 模式）与敏感项（JWT/SMB）保持只读、不做可编辑。
- **后端出站网络代理（FR-80）**：`network_proxy` 让后端所有外部 HTTP 出站运行期可配置走代理（空=直连），解决直连 GitHub 下载 CDN 不可达。新增独立无业务依赖的 `netproxy` 包持有全局代理（`atomic.Pointer[url.URL]` 无锁并发安全）：`SetProxy(rawURL)` 校验 scheme ∈ {http,https,socks5,socks5h}（均为标准库 `net/http` Transport 原生支持，**无新依赖**）后原子更新、空串清空、非法不覆盖；`ProxyFunc` 供 `http.Transport.Proxy` 使用（无代理返回 nil 走直连）。`update.Service`（首个也是当前唯一的后端出站消费者）的检测 client 与下载 client 各设 `Transport:&http.Transport{Proxy:netproxy.ProxyFunc}`，各自 Timeout 语义不变（检测 30s、下载无整体超时靠 context）。`main.go` 启动期读 `network_proxy` 非空则 `SetProxy` 注入；`PUT /api/settings` 含 `network_proxy` 时落库后即时 `netproxy.SetProxy`，非法 URL 仅记 WARN 不阻断保存（与工具路径空值守卫风格一致）。依赖方向单向（`api`/`update`/`main` → `netproxy`，`netproxy` 不依赖任何业务模块）。
- **自更新下载进度与失败重试（FR-90，扩 FR-46）**：给 FR-46 原本无反馈的下载链路加进度可观测性与失败可恢复性，**不动 `replaceAndRestart`**（Windows 覆盖运行中 exe / spawn 失败回退）正常逻辑。`downloadToTemp` 用计数 `io.Writer`（`countingWriter`）包装 `io.Copy`，每次写入后回调上报累计已下载字节（总字节取 `resp.ContentLength`，≤0 视为未知报 0）。`update.Service` 新增互斥量保护的进程内进度单例 `progressTracker`（状态机 `idle/downloading/verifying/done/failed`），**与 FR-46 检测结果的 TTL 缓存单例同构、不落库**（自更新本就用户显式触发、单次互斥，无需持久化），`Apply` 把回调接到 `setProgressDownloading` 并在进入校验 / 失败 / 完成时切状态。新增轮询端点 `GET /api/system/update/progress`（见 [docs/API.md](API.md)）读快照返回 `{state, downloaded, total, percent}`（`percent` 在 `total>0` 时即时算出、否则 0），鉴权随 `/api/*` 的 APIGuard、无外部依赖恒可用。前端「应用更新」子 tab 更新进行中每 ~800ms 轮询、以 Mantine `<Progress>` 展示百分比（总字节未知退化为展示已下载字节），失败后展示显式「重试」按钮直接重触发 apply。**只用 Go 标准库 + 内存状态轮询，不引 SSE/WebSocket/消息队列**（守架构不变量禁重型中间件），无新依赖、无新架构决策、无新 ADR（机制见 [docs/specs/update-progress.md](specs/update-progress.md)）。
- **运行时调试日志开关（FR-110）**：`debug_log` 让 GORM 日志在运行期可切换详细 / 安静。新增独立无业务依赖的 `dblog` 包以 `atomic.Bool` 持有开关：默认安静（Error 级 + 忽略 record-not-found，不刷普通 SQL 与「查无记录」噪音），开启切 Info 级（输出 SQL 与慢查询）。`dblog.Logger` 实现 `gorm/logger.Interface` 并预建「安静 / 详细」两委托 logger，按原子开关选择实际输出者；`LogMode` 以自身开关为唯一真源、忽略 GORM 启动期重设。`main.go` 在 `gorm.Open` 时把该 logger 注入 `gorm.Config.Logger`，启动后读 `settings.DebugLog()` 决定初始级别（重启保持）；`PUT /api/settings` 含 `debug_log` 时落库后即时调注入的 `dbLogger.SetEnabled`（经 `WithDebugLogApply` 回调）切级别，保存即生效、无需重启。区别于启动期只读的 `JIANVIDEO_DEBUG`（gin 模式，运行期不可切，保持原样）。依赖方向单向（`main`/`api` → `dblog`，`dblog` 仅依赖 gorm logger）。
- 与启动期 `config` 模块职责分离：`config` 管不可变部署参数（环境变量优先），`settings` 管用户运行期可改写的业务配置。环境变量只读查看由 §5.7 `GET /api/system/env` 提供。

### 5.9 分享链接（FR-43；密码/限次增强见 FR-78）

- 分享以 SQLite `shares` 表为真源（`token` 主键、`resource_type`、`resource_id`、`expires_at` 可空；密码/限次列 `password_hash`/`max_uses`/`used_count`）。`share.Service` 只管 token 生命周期：`crypto/rand` 生成 32 字节不可枚举 token，`Get` 校验过期（已过期/不存在统一为错误，公开层映射 `404`）。
- **密码与限次（FR-78，安全核心）**：可选访问密码以 bcrypt 哈希存 `password_hash`（复用 auth 既有 `golang.org/x/crypto/bcrypt`，绝不存明文 / 不回显前端 `json:"-"`），`VerifyPassword` 用 `bcrypt.CompareHashAndPassword` 比对；可选访问限次 `ConsumeUse` 在 GORM 事务内「先检查 `used_count<max_uses` 后 `used_count+1`」原子自增（SQLite 单写者 + 事务防并发超发），`max_uses=0` 为无限不计数。**口径**：密码门禁在 `ShareInfo`（进入页）强制——需密码且未带/带错密码只回 `{requires_password:true}`、不含任何 media/album 元信息（不泄露内容、不区分过期/撤销，便于前端弹密码框）、且**不消费**额度；限次的原子自增与密码二次校验发生在**实际访问资源**（raw/thumbnail/download/stream）时，每次成功访问计一次。密码错 / 限次尽统一映射 `404`。
- **鉴权受控例外**：`auth.APIGuard` 豁免前缀 `/api/share/`（带尾斜杠，不误伤受保护的管理端点 `/api/shares`）；公开路由内经 `shareAuth` 中间件自校验 token + 过期。
- **范围隔离（安全核心）**：每个 `/api/share/:token/media/:mediaId/*` 端点都做范围校验 `shareAllowsMedia`——media 分享比对 ID，album 分享查 `library.IsMediaInAlbum`；越权/不在范围/不存在一律 `404`，不区分以免信息泄露。
- **公开播放的安全边界**：免登视频播放只走 `playback.StreamFile`（渐进式原文件 + Range），**不**把 ffmpeg 转码/HLS 管线开放给匿名访客（防资源滥用/DoS）；需转码才能在浏览器播放的格式可下载原文件。`smb://` 不支持。
- 资源存在性与范围判定在 api 层用 `library` 完成，`share` 服务不依赖 `library`，保持无跨模块耦合。

### 5.10 照片地图与旅程轨迹（FR-39 / FR-76）

- **照片地图（FR-39，[ADR-0031](adr/0031-photo-map-leaflet.md)）**：前端页 `MapPage`（`/map`）用 leaflet + react-leaflet + OSM 在线瓦片展示带 GPS 的照片地理分布。数据经 `getMediaFiles({has_gps:true,...})` 分页累积拉取地理标记子集（后端 `has_gps` 筛选 `gps_lat != 0 OR gps_lon != 0`），逐点打 `Marker`、弹窗显示缩略图与名称。瓦片显示依赖联网，属真机/在线维度。
- **旅程轨迹（FR-76，扩 FR-39，无新 ADR）**：纯前端展示层增强，复用既定技术栈、不改后端、不引依赖。拉取时加 `sort:'media_time_asc'`（后端按 `COALESCE(media_time, added_at) ASC` 返回升序）；纯函数 `buildDayTracks(files)`（`frontend/src/utils/gpsTrack.ts`）过滤有效 GPS 点、复用 `groupMediaByDate(files,'day')` 按天分组、丢弃点数 < 2 的天、按日期升序输出 `{date,positions,color}[]`（颜色按下标循环取 `TRACK_COLORS`）。`MapPage` 据此渲染若干 `Polyline` 折线层叠加在散点上，「轨迹模式」`Switch`（默认开）控制折线显隐，散点 `Marker` 始终渲染。轨迹按「天」朴素聚合（同一天≈同一行程），更细的行程切分属后续增强、不在本期。

### 5.11 首页概览看板（FR-117，[ADR-0043](adr/0043-homepage-overview-dashboard.md)）

- 根路由 `/` 由时间轴改为「概览」数据看板；时间轴迁至 `/timeline`（导航「浏览」组并列「概览」「时间轴」两入口）。取代由 FR-A/AC-13 确立的「时间轴=首页」约定（修订 AC-13 + 新增 AC-21）。
- 看板总量维度由 `GET /api/library/summary` 提供：一次聚合（`COUNT`/`SUM` + 一次 `GROUP BY library_id`）返回媒体总数、视频/图片拆分、`SUM(file_size)`、`SUM(duration)`、启用库数与各库明细；全程 `deleted_at IS NULL`，视频/图片按内置图片扩展名集合区分（`LOWER(format) IN 内置图片集` 为图片，否则视频，与媒体筛选口径一致），避免 N+1。聚合在 library 服务层、`db` 仅读写。
- 其余维度复用既有端点（观看统计 FR-75 / 系统信息 FR-21 / 健康巡检 FR-73 / 扫描任务 FR-29 / 转码任务 FR-77 / 继续观看 FR-44）；前端各数据源独立降级，空库零值不崩。

### 5.12 系统指标采样与监控（FR-119，[ADR-0044](adr/0044-metrics-sampling-persistence.md)）

- `/monitor` 页展示 CPU / 内存 / 磁盘 / 转码并发的当前值 + 时序折线（range 1h/24h/7d），前端复用 FR-118 的 Recharts `TrendChart`、15s 轮询。
- **采集**：引入 `gopsutil/v4` 取系统 CPU% 与数据盘用量；进程内存/goroutine 用标准库 `runtime`；转码并发取播放服务 `ActiveSessions()`（只读 `len(sessions)`，经 `func() int` provider 注入，`metrics → playback` 不反向依赖）。
- **采样与持久化**：`internal/metrics` 采样器后台 `time.Ticker` 每 15s 采一行写入 SQLite `metric_samples` 表；按 7 天保留期裁剪防膨胀；随服务 `Start`/`Stop`（main 装配，注入 db + dataDir + 转码计数 provider），不泄漏 goroutine。采样逻辑在 metrics 服务层、`db` 仅 `metric_samples` 读写，依赖方向 `metrics → db` 单向。只落 SQLite，不引时序库 / Redis。
- **查询**：`GET /api/system/metrics?range=` 按 range 选窗口与桶大小下采样（`unixepoch(sampled_at) / 桶秒` GROUP BY + AVG/MAX），点数有界；`current` 为最新一条原始样本（按 `id DESC` 取，回避 mattn/go-sqlite3 的 time 文本序列化比较不可靠）。

### 5.13 版本化 schema 迁移（FR2-017，[ADR-0062](adr/0062-versioned-schema-migrations.md)）

- `internal/migration` 是启动期 schema 演进入口：生产启动不再直接执行无版本记录的全局 `InitSchema + AutoMigrate`，而是经 migration registry 顺序执行。
- 每个 migration 提供 ID、说明、`SafeToRetry`、`Up` 与 `Validate`；dry-run 只读返回步骤和影响预估，不写业务表、`schema_migrations` 或审计表。
- 真实迁移只在存在待执行步骤时运行；执行前使用 SQLite `VACUUM INTO` 在数据目录 `backups/` 下创建备份，并打开备份执行 `PRAGMA integrity_check`，校验失败即停止。
- `schema_migrations` 记录每步 `running/succeeded/failed`、错误摘要、校验摘要与备份路径；中断后重启会跳过已成功且校验通过的步骤，失败且 `SafeToRetry=true` 的步骤可重试。
- 当前 FR2-017 切片包含既有基础 schema 收敛、默认 Space 回填、关键索引创建与 FR2-007 查询 smoke；不实现 FR2-007/FR2-037/FR2-040 的完整业务 schema。
- 迁移开始、成功、失败写 `scope=system` 的 `audit_events`，`space_id` 为空，符合 ADR-0063 的系统级作用域语义。

## 6. 部署

- **运行形态**：单个可执行文件，内嵌前端静态资源。
- **依赖**：FFmpeg/ffprobe 外部进程；发布包随包附带，启动时按「环境变量 → 可执行文件同目录捆绑版 → PATH」自动发现（见 [ADR-0027](adr/0027-cross-platform-packaging.md)）。ImageMagick（`magick`）为可选外部进程，用于 HEIC/RAW 转 JPEG（FR-37），同样按「`JIANVIDEO_MAGICK_PATH` → 同目录捆绑版 → PATH」解析；未安装时仅 HEIC/RAW 显示不可用，不影响其他功能。须使用带 HEIC + RAW delegate 的构建。
- **数据库**：SQLite WAL 模式，数据库文件位于配置目录。
- **配置**：通过 `config.yml` 或环境变量控制（端口、媒体库路径、FFmpeg 路径等）。
- **前端构建**：React + TypeScript 通过 Vite 构建，`dist/` 目录通过 `go:embed` 内嵌。
- **开源协议页（FR-57，入口位置见 FR-61）**：当前版本（取自 `GET /api/system/info` 的 `app_version`）+「开源协议」链接 → `/licenses`（`LicensesPage`，受保护路由）的入口由左侧导航（`AppShell.Navbar`）底部承载（FR-61 取代原 FR-57 的 `AppShell.Footer` 页脚，页脚已移除）：桌面展开态平铺版本号文本与协议链接，收缩态（64px）以协议图标 + Tooltip（含版本号）承载、避免文字截断，移动端抽屉（`Drawer`）底部同样补版本号与协议链接。协议清单 `frontend/src/data/licenses.json` 由 `frontend/scripts/gen-licenses.mjs` **构建期生成**（`npm run gen:licenses`）：前端全部生产依赖经 `license-checker` 取协议全文、后端 `go.mod` 直接依赖（剔除 `// indirect`）尽力从本机 Go module cache 读全文（读不到给 `pkg.go.dev?tab=licenses` 外链）、项目自身读根 `LICENSE`（MIT）。JSON 入库、随 `dist/` go:embed 内嵌，**页面直接 import、运行时不联网**。
- **页眉「更新可用」提示（FR-58）**：全局页眉（`AppShell.Header`）的 `UpdateIndicator` 组件消费 FR-46 的更新检查——挂载时按持久化频道（设置键 `update_channel`）调一次 `GET /api/system/update/check`（**非 force**，命中后端 10 分钟 TTL 缓存即廉价返回），仅当 `has_update=true` 时常驻展示提示（含目标版本 `latest`/`tag`），点击经 React Router 导航到 `/system?tab=update`（FR-113 拍平为一级 tab 后直接选中应用更新 tab；旧式 `?tab=system&sys=update` 仍兼容）。检查失败 / 无更新一律不展示、**失败静默**（复用 FR-46 优雅降级语义），不新增端点、不绕缓存、不轮询。
- **PWA**：经 `vite-plugin-pwa` 产出 `manifest.webmanifest` + Service Worker，支持「添加到主屏」与离线应用壳；Service Worker 仅预缓存壳静态资源，`/api`/媒体流运行时走网络（见 [ADR-0028](adr/0028-mobile-pwa.md)）。
- **打包**：根目录 `Makefile` 一键完成「构建前端 → 编译单二进制（注入版本）→ 组装发布包（含随包 ffmpeg）」。
- **跨平台**：因 SQLite 用 mattn/go-sqlite3（CGO），采用各平台原生构建（在对应 OS 上 make），不做交叉编译（见 ADR-0027）。
- **自动发布**：GitHub Actions 原生 runner 矩阵（`ubuntu-latest`/`windows-latest`）各自 CGO 原生构建（复用 `build.yml`），`VERSION` 变更打 tag 出正式 Release、普通 push 滚动出 `dev` 预发布，产物含各平台单二进制 + `checksums.txt`；不引 Docker、不交叉编译（见 [ADR-0032](adr/0032-release-engineering.md)）。

## 7. 关键裁决与不做项

| 决策 | 说明 | ADR |
|---|---|---|
| Go 作为后端语言 | 单文件部署、跨平台、高性能 | [0001](adr/0001-go-backend.md) |
| SQLite WAL 作为元数据数据库 | 零配置、单文件、足够单用户场景 | [0002](adr/0002-sqlite-wal-metadata.md) |
| HLS/TS 强制输出 | 确保网络兼容性与追播能力 | [0003](adr/0003-hls-ts-streaming.md) |
| mpegts.js 作为播放内核 | 唯一可靠支持 TS 实时追加的浏览器方案 | [0004](adr/0004-mpegts-js-player.md) |
| 原生 SMB 支持 | 避免用户手动挂载 NAS 共享 | [0005](adr/0005-native-smb-support.md) |
| FFmpeg filter_complex split 单进程多输出 | 确保多码率 GOP 对齐，减少资源开销 | [0026](adr/0026-abr-adaptive-bitrate.md) |
| 移动端 PWA（仅缓存应用壳） | 可添加到主屏 + 离线壳，媒体流不离线缓存 | [0028](adr/0028-mobile-pwa.md) |
| 发布工程：CI 原生矩阵构建 + GitHub Releases 分发 + 二进制自更新 | 自动出全平台产物、用户一键更新；不引 Docker、不交叉编译 | [0032](adr/0032-release-engineering.md) |

**不做项**：
- 不做多用户/权限管理（单用户模式）
- 不做容器化部署（Docker 等）
- 不做分布式/集群架构
- 不做移动端原生 App
- 不做在线分享/社交功能
