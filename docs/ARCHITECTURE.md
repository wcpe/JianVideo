# 架构设计：轻量级单用户视频媒体服务器

> 系统当前真貌（HOW）。始终原地更新到现状；结构 / 机制变了就改它。

## 1. 定位与边界

一款单用户私有视频媒体服务器，将分散在多个硬盘或 NAS 中的视频和图片汇聚到一个 Web 媒体库，通过浏览器直接播放所有格式的视频（不兼容格式自动转码为 HLS/TS 流），并支持图片预览和硬件加速转码降低 CPU 负载。

**边界**：
- 系统仅服务单用户，无多租户、权限管理。
- 前端编译产物通过 `go:embed` 内嵌于 Go 二进制，Web 服务器由 Go 统一承载。
- 不依赖外部数据库服务，元数据使用 SQLite（WAL 模式）本地存储。
- 不依赖外部消息队列、缓存或容器编排。
- FFmpeg 通过 CGO 绑定（csnewman/ffmpeg-go）直接调用 libavcodec/libavformat/libavutil/libswscale C API，编译时需链接 FFmpeg 开发库（libavcodec-dev 等）。
- 支持全部硬件加速编码器（NVIDIA NVENC、Intel QSV、AMD AMF、VAAPI、VideoToolbox、Vulkan），必须同时支持 H.264 和 H.265。
- 硬件加速能力在启动时自动检测，通过 `GET /api/transcode/hwaccel` 接口暴露。

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
| `library` | 媒体库管理、目录注册、异步递归扫描与进度状态、图片/视频后缀策略、文件索引、媒体文件 CRUD、目录浏览、缩略图生成 | → `db` |
| `playback` | 播放进度追踪、Range 请求处理、会话管理 | → `db`, `library` |
| `player` | HLS 切片写入、m3u8 索引管理、master playlist 生成 | → `library` |
| `transcoder` | FFmpeg 转码管道、多码率转码（MultiPipeline）、硬件加速检测/选择、流式输出、字幕转换（SRT/ASS→WebVTT、字幕文件查找） | → `db` |
| `watcher` | 文件系统事件监听（fsnotify） | → `library` |
| `auth` | 单用户登录/会话管理（JWT + bcrypt） | → `db` |
| `settings` | 运行期键值设置读写（按 key 读/写、批量 upsert），为回收站、定时扫描提供配置真源 | → `db` |
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
| file_name | TEXT | 文件名 |
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

**转码会话（transcode_sessions）**

| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增主键 |
| media_id | INTEGER FK | 关联媒体文件 |
| pid | INTEGER | FFmpeg 进程 PID |
| output_url | TEXT | HLS 播放 URL |
| status | TEXT | 状态：running/stopped/error |
| hw_accel | TEXT | 使用的硬件加速类型 |
| created_at | DATETIME | 创建时间 |

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

## 4. 接口

对外接口为 RESTful HTTP API，前端通过 `go:embed` 内嵌的静态资源提供。详细契约见 `docs/API.md`。

**接口概览**：

| 分组 | 前缀 | 说明 |
|---|---|---|
| 认证 | `/api/auth` | 登录、登出、会话校验 |
| 媒体库 | `/api/library` | 目录增删、媒体文件列表、搜索、异步扫描与进度 SSE、目录浏览、图片 raw 预览、缩略图、后缀配置 |
| 播放 | `/api/play` | 视频流播放、Seek、转码控制 |
| 转码 | `/api/transcode` | 转码状态查询、硬件加速能力查询 |
| 配置 | `/api/config` | 系统配置读取 |
| 设置 | `/api/settings` | 运行期键值设置读取与批量写入 |

### 5.0 目录浏览

- `GET /api/library/browse`：按 `library_id` + `parent_path` 浏览目录内容
- 一次 SQL 查询（`file_path LIKE prefix%`）+ Go 层 map 分组聚合子目录
- 面包屑由后端按路径分隔符拆分构建；Windows 盘符路径保持 `D:/...` 形式，不额外加 `/D:`
- `file_path` 索引确保前缀查询性能满足 NFR-08（500ms 内响应）
- 前端 Tab 切换（时间轴 | 文件目录），媒体库目录卡片提供“浏览”入口，面包屑导航 + 文件列表复用现有卡片样式
- 存储库管理页（`/library-manager`）只展示存储库卡片（扫描进度 + 已索引媒体数量），不内嵌媒体文件列表；点击卡片携 `library_id` + 起始 `path` 跳转 `/browse` 定位到该库根目录。`GET /api/library/paths` 每项附带 `media_count`（按 `library_id` 一次 `GROUP BY` 统计、排除软删），避免按库 N+1 计数

## 5. 关键机制

### 5.1 媒体库扫描与后缀策略

- 本地目录注册时必须校验路径存在且为目录，入库前转为绝对路径并统一为正斜杠。
- `ScanLibraryWithType` 按 `LibraryPath.type` 分发：`local` 使用 `filepath.WalkDir` 递归扫描，`smb` 使用 SMB 客户端遍历共享目录。
- 媒体识别统一由 `library.Service` 维护：内置视频后缀和图片后缀始终可用，自定义后缀通过 `media_extensions.library_id` 绑定到单个 `LibraryPath`。
- 扫描入库按 `library_id + file_path` 去重，重复扫描不会重复写入。
- 图片文件可通过 `GET /api/library/media/:id/raw` 提供本地预览；视频文件继续走播放链路。
- 异步扫描：`POST /api/library/scan/:id` 经 `Service.StartAsyncScan` 在后台 goroutine 执行，接口立即返回不阻塞主线程；进度由 `scan_status.go` 维护的全局 `ScanStatus`（`sync.RWMutex` 并发安全，同一时刻仅跟踪一个扫描任务）记录，经 `GET /api/library/scan/progress` SSE 端点每 500ms 推送，`completed`/`error` 后关闭连接。`ScanLibraryWithType` 仍保留同步签名供 watcher 等内部调用。

### 5.1.1 缩略图生成

- `thumbnail.go` 提供缩略图能力：入库时对新文件异步调用 `GenerateThumbnail`，视频取第 2 秒帧、图片缩放为 320px 宽，统一经 ffmpeg 生成，失败仅记日志不阻塞入库。
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
- 硬件加速编码器完整清单（必须同时支持 H.264 和 H.265）：

| 平台 | H.264 编码器 | H.265 编码器 | 设备类型 |
|---|---|---|---|
| NVIDIA GPU | `h264_nvenc` | `hevc_nvenc` | `cuda` |
| Intel QSV | `h264_qsv` | `hevc_qsv` | `qsv` |
| AMD AMF | `h264_amf` | `hevc_amf` | `d3d11va` |
| VAAPI (Linux) | `h264_vaapi` | `hevc_vaapi` | `vaapi` |
| VideoToolbox (macOS) | `h264_videotoolbox` | `hevc_videotoolbox` | `videotoolbox` |
| Vulkan | `h264_vulkan` | `hevc_vulkan` | `vulkan` |
| 软件兜底 | `libx264` | `libx265` | — |

- Intel 核显检测：通过 sysfs（`/sys/class/drm/card0/device/vendor` = `0x8086`）+ 驱动名（`i915`/`xe`）+ 无独立显存确认核显身份。
- 硬件检测优先级：CUDA → QSV → VAAPI → D3D11VA → DXVA2 → VideoToolbox → Vulkan → 软件。
- 硬件加速失败时自动降级，不中断播放。

### 5.4 HLS 切片与追播

- 转码输出同时写入 HLS 切片文件（`.ts`）和内存管道。
- mpegts.js 通过 HTTP Range 请求获取最新切片数据。
- 播放器轮询 m3u8 索引文件，检测到新切片时自动追加到 MSE 缓冲区。
- 追播延迟控制：播放器保持 3-5 秒的缓冲距离。

### 5.5 多码率自适应（ABR）

- `MultiPipeline` 使用 FFmpeg filter_complex split 单进程多输出，同时生成 1080p/720p/480p 三档 HLS。
- 码率阶梯根据源分辨率自动裁剪（<720p 只输出 480p+720p，<1080p 不输出 1080p）。
- 所有码率共享同一 GOP（-g 48 -keyint_min 48 -sc_threshold 0），确保切换时画面连续。
- 切片文件名包含码率标识（如 `1080p_segment_000.ts`），m3u8 命名为 `{quality}.m3u8`。
- `master.m3u8` 包含 `EXT-X-STREAM-INF` 标签，描述各码率流的 BANDWIDTH/RESOLUTION。
- 前端 hls.js 动态 import，自动选择最佳码率；不支持 hls.js 时回退 mpegts.js。
- 详见 [ADR-0026](adr/0026-abr-adaptive-bitrate.md)。

### 5.6 硬件加速管理

- 硬件编码器检测：`-tags ffmpeg` 构建下经 CGO 调用 libav `avcodec_find_encoder_by_name` 判断编码器是否编入，辅以 Intel sysfs 识别核显；非该构建走纯 Go 存根（按软件编码降级）。
- 实际编码仍由外部 ffmpeg 进程以对应编码器名完成。
- 按优先级尝试：NVIDIA NVENC → Intel QSV → VAAPI → 软件；失败自动降级，不中断播放。

### 5.7 系统诊断与编解码器实测（FR-21）

- `GET /api/system/info`：返回 OS/架构/CPU 数/主机名/Go 版本/应用版本（构建期 `-ldflags -X main.version` 注入）、ffmpeg 可用性与版本，并复用 §5.6 的硬件加速检测结果。
- `POST /api/system/codec-test`：对候选编码器（软件 + QSV/VAAPI/NVENC/AMF/VideoToolbox 的 H.264/H.265）用**外部 ffmpeg 跑一小段试编码**（`-f lavfi … -f null`），报告「是否编入当前 ffmpeg / 试编码是否成功 / 失败尾部」。独立于 §5.6 的 CGO 检测，普通构建即可用，专供真机验收。
- 前端 `/system` 页展示并支持一键复制纯文本报告。

### 5.8 运行期设置（FR-24）

- 设置以 SQLite `settings` 表为唯一真源，由 `settings.Service` 封装读写：`Get`/`GetAll`/`Set`/`SetMany`，写入走主键冲突 upsert，批量写在单事务内原子完成（详见 ADR-XXXX）。
- `GET /api/settings` 返回全部键值（map 形式），`PUT /api/settings` 批量 upsert 并回读返回；前端 `/settings` 页读写「每盘符回收站路径」「扫描周期」等键值。
- 已知键以常量集中定义（`recycle_bin_paths`/`scan_interval`），结构化值以 JSON 字符串存于单 key，由消费方（回收站清理、定时扫描）按需解析。
- 与启动期 `config` 模块职责分离：`config` 管不可变部署参数（环境变量优先），`settings` 管用户运行期可改写的业务配置。

## 6. 部署

- **运行形态**：单个可执行文件，内嵌前端静态资源。
- **依赖**：FFmpeg/ffprobe 外部进程；发布包随包附带，启动时按「环境变量 → 可执行文件同目录捆绑版 → PATH」自动发现（见 [ADR-0027](adr/0027-cross-platform-packaging.md)）。
- **数据库**：SQLite WAL 模式，数据库文件位于配置目录。
- **配置**：通过 `config.yml` 或环境变量控制（端口、媒体库路径、FFmpeg 路径等）。
- **前端构建**：React + TypeScript 通过 Vite 构建，`dist/` 目录通过 `go:embed` 内嵌。
- **PWA**：经 `vite-plugin-pwa` 产出 `manifest.webmanifest` + Service Worker，支持「添加到主屏」与离线应用壳；Service Worker 仅预缓存壳静态资源，`/api`/媒体流运行时走网络（见 [ADR-0028](adr/0028-mobile-pwa.md)）。
- **打包**：根目录 `Makefile` 一键完成「构建前端 → 编译单二进制（注入版本）→ 组装发布包（含随包 ffmpeg）」。
- **跨平台**：因 SQLite 用 mattn/go-sqlite3（CGO），采用各平台原生构建（在对应 OS 上 make），不做交叉编译（见 ADR-0027）。

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

**不做项**：
- 不做多用户/权限管理（单用户模式）
- 不做容器化部署（Docker 等）
- 不做分布式/集群架构
- 不做移动端原生 App
- 不做在线分享/社交功能
