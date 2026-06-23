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
| `transcoder` | FFmpeg 转码管道、多码率转码（MultiPipeline）、硬件加速检测/选择、流式输出、字幕转换（SRT/ASS→WebVTT、字幕文件查找） | → `db` |
| `watcher` | 文件系统事件监听（fsnotify） | → `library` |
| `auth` | 单用户登录/会话管理（JWT + bcrypt） | → `db` |
| `settings` | 运行期键值设置读写（按 key 读/写、批量 upsert），为回收站、定时扫描提供配置真源 | → `db` |
| `share` | 分享链接 token 生命周期与过期（FR-43）；只管 token，资源存在性/范围判定由 api 层用 `library` 完成，无跨模块耦合 | → `db` |
| `db` | SQLite 数据库初始化、GORM 元数据 CRUD | 无业务依赖 |
| `config` | 配置加载（环境变量优先） | 无业务依赖 |

**依赖方向**：`web` → `api` → `library` / `playback` / `player` / `transcoder` → `db`，严格单向，禁止反向。`config` 和 `auth` 为横切关注点。

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
| last_position | REAL | 上次播放位置（秒，FR-44），用于续播 |
| watched | INTEGER | 是否已看完（FR-44），0/1 |
| last_watched_at | DATETIME | 最近一次观看时间（FR-44），用于「继续观看」排序 |
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
> 软删除与回收站（FR-25）：删除媒体仅置 `deleted_at`，不物理删除记录、不删除磁盘源文件。`deleted_at` 为普通索引列（非 GORM 软删约定），故服务层在常规列表/计数手工加 `deleted_at IS NULL`（`ListMediaFilesFiltered`、`ListLibraryPathViews` 等），回收站列表查 `deleted_at IS NOT NULL`，还原清空该列。
>
> 回收站清理（FR-26）：`CleanupRecycle(drivePaths)` 把全部软删项的磁盘源文件移动到其所在盘符对应的回收站目录、按 `deleted_at` 日期分子目录，移动成功后删除 `media_files` 记录（先移动成功、后删记录保证一致）。盘符→目录映射由 `api` 层从设置键 `recycle_bin_paths`（JSON）解析后传入，`library` 服务不依赖 `settings`、不解析 JSON（职责单一）。校验先行：存在任一软删项所在盘符（含 SMB / 无盘符）未配置则整体拒绝（`ErrRecycleBinPathUnset` → HTTP 409），不移动任何文件。
>
> 媒体时间与 EXIF（FR-31）：入库点 `CreateMediaFile` 统一富化元数据——图片用 `imagemeta`（纯 Go）提取拍摄时间与相机/镜头/光圈/快门/ISO/GPS，视频用 ffprobe 读 `creation_time`；再按 EXIF → 文件名日期 → 文件创建时间 → 修改时间的降级链定出 `media_time` 与 `media_time_source`。时间轴列表按 `COALESCE(media_time, added_at)` 排序。

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

**分享链接（shares）（FR-43）**

| 字段 | 类型 | 说明 |
|---|---|---|
| token | TEXT PK | 加密随机不可枚举令牌（`crypto/rand` 32 字节 hex） |
| resource_type | TEXT | 分享资源类型：`media` / `album` |
| resource_id | INTEGER, INDEX | 被分享的媒体 ID 或相册 ID |
| expires_at | DATETIME NULL | 过期时间；空表示永不过期 |
| created_at | DATETIME | 创建时间 |

公开访问以 token 为唯一凭据，过期/撤销即失效；范围与安全边界见 §5.9。

## 4. 接口

对外接口为 RESTful HTTP API，前端通过 `go:embed` 内嵌的静态资源提供。详细契约见 `docs/API.md`。

**接口概览**：

| 分组 | 前缀 | 说明 |
|---|---|---|
| 认证 | `/api/auth` | 登录、登出、会话校验 |
| 媒体库 | `/api/library` | 目录增删、媒体文件列表、搜索、异步扫描与进度 SSE、扫描任务队列与列表（FR-29）、目录浏览、图片 raw 预览、缩略图、原文件下载（FR-42）、后缀配置、继续观看列表（FR-44）、软删除/回收站与还原（FR-25）、回收站清理（FR-26） |
| 相册 | `/api/albums` | 相册增删、跨目录成员增删与成员浏览（FR-40） |
| 播放 | `/api/play` | 视频流播放、Seek、转码控制、观看位置上报与已看标记（FR-44） |
| 转码 | `/api/transcode` | 转码状态查询、硬件加速能力查询 |
| 配置 | `/api/config` | 系统配置读取 |
| 设置 | `/api/settings` | 运行期键值设置读取与批量写入 |
| 分享管理 | `/api/shares` | 创建/列出/撤销分享链接（鉴权后，FR-43） |
| 公开分享 | `/api/share/:token` | 免登只读访问被分享媒体/相册：元信息、图片 raw、缩略图、原文件下载、视频渐进式播放（APIGuard 豁免，经 shareAuth + 范围校验，FR-43） |

### 5.0 目录浏览

- `GET /api/library/browse`：按 `library_id` + `parent_path` 浏览目录内容
- 一次 SQL 查询（`file_path LIKE prefix%`）+ Go 层 map 分组聚合子目录
- 面包屑由后端按路径分隔符拆分构建；Windows 盘符路径保持 `D:/...` 形式，不额外加 `/D:`
- `file_path` 索引确保前缀查询性能满足 NFR-08（500ms 内响应）
- 前端 Tab 切换（时间轴 | 文件目录），媒体库目录卡片提供“浏览”入口，面包屑导航 + 文件列表复用现有卡片样式
- 存储库管理页（`/library-manager`）只展示存储库卡片（扫描进度 + 已索引媒体数量），不内嵌媒体文件列表；点击卡片携 `library_id` + 起始 `path` 跳转 `/browse` 定位到该库根目录。`GET /api/library/paths` 每项附带 `media_count`（按 `library_id` 一次 `GROUP BY` 统计、排除软删），避免按库 N+1 计数
- **聚合虚拟根（FR-66，[ADR-0037](adr/0037-aggregate-directory-browse.md)）**：`parent_path` 取哨兵 `__root__` 时进入聚合根分支——忽略 `library_id`，列出所有启用库作为顶层目录项（`DirInfo.library_id` 填该库 ID、`name`=label、`path`=库 path），面包屑单段 `{name:"全部存储库", path:"__root__"}`；其余 `parent_path` 走原单库前缀逻辑不变。前端目录浏览页（`/browse`）默认进虚拟根列全部库、点库携 `library_id` 下钻、面包屑可回根；带 `library_id`+`path` 深链则直接进该库（向后兼容）。库列表真源仍是 `library_paths`、媒体真源仍是 `media_files`，聚合根不新增持久状态

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

### 5.1.1 缩略图生成

- `thumbnail.go` 提供缩略图能力：入库时对新文件异步调用 `GenerateThumbnail`，视频取第 2 秒帧、普通图片缩放为 320px 宽，统一经 ffmpeg 生成；HEIC/RAW 改经外部 ImageMagick（`magick`）缩放生成 320px 宽缩略图（FR-37）。生成失败仅记日志不阻塞入库。
- 缩略图存于数据目录下 `thumbnails/`（启动时 `InitThumbnailDir` 初始化，按原始路径 SHA-256 hash 命名避免特殊字符冲突）。
- `GET /api/library/thumbnail/:id` 返回缩略图；尚未生成时返回 `202` 并触发后台生成，前端可稍后重试。媒体卡片用缩略图，图片预览弹窗仍用原图。

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
- 前端在 `/system` 控制台页的「系统信息」tab 展示（硬件加速卡片按家族 × 编码逐项展示、标示缓存来源与「重新测试」）并支持一键复制纯文本报告。控制台页（`ConsolePage`，FR-55）以 Mantine `Tabs` 把「系统信息」与「设置」（§5.8）合并为单页两 tab，tab 状态由 URL query `?tab=system|settings` 控制；`SystemPage`/`SettingsPage` 原样作 tab 内容。

### 5.8 运行期设置（FR-24）

- 设置以 SQLite `settings` 表为唯一真源，由 `settings.Service` 封装读写：`Get`/`GetAll`/`Set`/`SetMany`，写入走主键冲突 upsert，批量写在单事务内原子完成（详见 ADR-0029）。
- `GET /api/settings` 返回全部键值（map 形式），`PUT /api/settings` 批量 upsert 并回读返回；前端在 `/system` 控制台页的「设置」tab（`?tab=settings`，FR-55）读写「每盘符回收站路径」「扫描周期」「FFmpeg 路径」等键值，旧 `/settings` 路由重定向至此。
- 已知键以常量集中定义（`recycle_bin_paths`/`scan_interval`/`update_channel`/`transcode_codec_priority`/`ffmpeg_path`/`ffprobe_path`），结构化值以 JSON 字符串存于单 key，由消费方按需解析：回收站清理（FR-26）读 `recycle_bin_paths`（盘符→目录 JSON）解析后传给 `library.CleanupRecycle`；定时扫描读 `scan_interval`。
- **FFmpeg 路径持久化设置（FR-56）**：`ffmpeg_path`/`ffprobe_path` 让 ffmpeg/ffprobe 路径运行期可配置。`main.go` 启动时先 `resolveTool` 注入（环境变量→同目录捆绑版→PATH），随后若设置非空则覆盖（**持久化设置优先于自动发现**）；`PUT /api/settings` 含这两键时落库后即时调 `transcoder.SetFFmpegPath`/`SetFFprobePath`（ffprobe 同步给 `library`）应用到运行期，保存即生效、无需重启（api→transcoder 依赖方向允许）。
- 与启动期 `config` 模块职责分离：`config` 管不可变部署参数（环境变量优先），`settings` 管用户运行期可改写的业务配置。环境变量只读查看由 §5.7 `GET /api/system/env` 提供。

### 5.9 分享链接（FR-43）

- 分享以 SQLite `shares` 表为真源（`token` 主键、`resource_type`、`resource_id`、`expires_at` 可空）。`share.Service` 只管 token 生命周期：`crypto/rand` 生成 32 字节不可枚举 token，`Get` 校验过期（已过期/不存在统一为错误，公开层映射 `404`）。
- **鉴权受控例外**：`auth.APIGuard` 豁免前缀 `/api/share/`（带尾斜杠，不误伤受保护的管理端点 `/api/shares`）；公开路由内经 `shareAuth` 中间件自校验 token + 过期。
- **范围隔离（安全核心）**：每个 `/api/share/:token/media/:mediaId/*` 端点都做范围校验 `shareAllowsMedia`——media 分享比对 ID，album 分享查 `library.IsMediaInAlbum`；越权/不在范围/不存在一律 `404`，不区分以免信息泄露。
- **公开播放的安全边界**：免登视频播放只走 `playback.StreamFile`（渐进式原文件 + Range），**不**把 ffmpeg 转码/HLS 管线开放给匿名访客（防资源滥用/DoS）；需转码才能在浏览器播放的格式可下载原文件。`smb://` 不支持。
- 资源存在性与范围判定在 api 层用 `library` 完成，`share` 服务不依赖 `library`，保持无跨模块耦合。

## 6. 部署

- **运行形态**：单个可执行文件，内嵌前端静态资源。
- **依赖**：FFmpeg/ffprobe 外部进程；发布包随包附带，启动时按「环境变量 → 可执行文件同目录捆绑版 → PATH」自动发现（见 [ADR-0027](adr/0027-cross-platform-packaging.md)）。ImageMagick（`magick`）为可选外部进程，用于 HEIC/RAW 转 JPEG（FR-37），同样按「`JIANVIDEO_MAGICK_PATH` → 同目录捆绑版 → PATH」解析；未安装时仅 HEIC/RAW 显示不可用，不影响其他功能。须使用带 HEIC + RAW delegate 的构建。
- **数据库**：SQLite WAL 模式，数据库文件位于配置目录。
- **配置**：通过 `config.yml` 或环境变量控制（端口、媒体库路径、FFmpeg 路径等）。
- **前端构建**：React + TypeScript 通过 Vite 构建，`dist/` 目录通过 `go:embed` 内嵌。
- **开源协议页（FR-57，入口位置见 FR-61）**：当前版本（取自 `GET /api/system/info` 的 `app_version`）+「开源协议」链接 → `/licenses`（`LicensesPage`，受保护路由）的入口由左侧导航（`AppShell.Navbar`）底部承载（FR-61 取代原 FR-57 的 `AppShell.Footer` 页脚，页脚已移除）：桌面展开态平铺版本号文本与协议链接，收缩态（64px）以协议图标 + Tooltip（含版本号）承载、避免文字截断，移动端抽屉（`Drawer`）底部同样补版本号与协议链接。协议清单 `frontend/src/data/licenses.json` 由 `frontend/scripts/gen-licenses.mjs` **构建期生成**（`npm run gen:licenses`）：前端全部生产依赖经 `license-checker` 取协议全文、后端 `go.mod` 直接依赖（剔除 `// indirect`）尽力从本机 Go module cache 读全文（读不到给 `pkg.go.dev?tab=licenses` 外链）、项目自身读根 `LICENSE`（MIT）。JSON 入库、随 `dist/` go:embed 内嵌，**页面直接 import、运行时不联网**。
- **页眉「更新可用」提示（FR-58）**：全局页眉（`AppShell.Header`）的 `UpdateIndicator` 组件消费 FR-46 的更新检查——挂载时按持久化频道（设置键 `update_channel`）调一次 `GET /api/system/update/check`（**非 force**，命中后端 10 分钟 TTL 缓存即廉价返回），仅当 `has_update=true` 时常驻展示提示（含目标版本 `latest`/`tag`），点击经 React Router 导航到 `/system?tab=system#update`（`SystemPage` 应用更新卡片带 `id="update"` 锚点，尽力滚动定位）。检查失败 / 无更新一律不展示、**失败静默**（复用 FR-46 优雅降级语义），不新增端点、不绕缓存、不轮询。
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
