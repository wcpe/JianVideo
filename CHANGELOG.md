# 变更日志

本项目所有重要变更记录于此。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布版本

### 新增
- **分享链接（FR-43）**：为指定媒体 / 相册生成 token 化只读公开链接，免登访客凭链接在线查看图片、在线播放视频、下载原文件，带可配置过期（含永不过期）。作为鉴权的受控例外：`auth.APIGuard` 豁免 `/api/share/` 前缀（带尾斜杠，不误伤受保护的管理端点 `/api/shares`），公开路由经 `shareAuth` 中间件自校验 token + 过期，且每个媒体端点都做范围校验（mediaId 必须 == 被分享媒体或 ∈ 被分享相册成员，越权一律 404）。token 由 `crypto/rand` 生成 32 字节不可枚举。安全边界：免登视频播放只走渐进式 `StreamFile`（原文件 + Range），不向匿名访客开放 ffmpeg 转码 / HLS 管线（防资源滥用）。新增 `internal/share` 服务、`shares` 表、管理端点 `POST/GET /api/shares` 与 `DELETE /api/shares/:token`、公开端点 `GET /api/share/:token`(+`/media/:mediaId/raw|thumbnail|download|stream`)；前端播放页 / 相册页加「分享」入口（选有效期、生成、复制链接），新增免登公开查看页 `/s/:token`（不套登录守卫，按类型展示图片 / 视频 / 相册网格）。复用 FR-13 鉴权、FR-40 相册、FR-42 下载。
- **下载原文件（FR-42）**：新增鉴权后的原文件下载端点 `GET /api/library/media/:id/download`，对图片与视频一视同仁回传磁盘原始字节（不转码/不转换，区别于 raw 端点），`Content-Disposition: attachment`、文件名为真实 `file_name`（RFC 5987 编码兼容中文），经流式回传支持 HTTP Range 断点续传。软删项不可下载（复用 FR-25）、`smb://` 远程文件暂不支持（返回 `400`，FR-02 真机受限）。前端播放页与图片预览弹窗新增「下载原文件」入口。
- **回收站清理（FR-26）**：新增清理端点 `POST /api/library/recycle/cleanup`，把回收站内全部软删项的磁盘源文件移动到其所在盘符对应的回收站目录（取自设置键 `recycle_bin_paths` 的 JSON 映射，盘符大小写不敏感）、按删除日期分子目录 `<回收站目录>/<YYYY-MM-DD>/<文件名>`，移动成功后删除 `media_files` 记录（先移动成功、后删记录保证一致）。校验先行：存在任一软删项所在盘符（含 SMB / 无盘符）未配置回收站路径则整体拒绝（返回 `409`、不移动任何文件）。前端 `/recycle` 回收站页新增「清理回收站」按钮（二次确认），未配路径时提示去设置页配置。复用 FR-24 设置与 FR-25 软删能力，`library` 服务不依赖 `settings`（盘符→目录映射由 API 层解析后传入）。
- **增量/全量扫描 + 已删文件对账（FR-27）**：扫描区分两种模式——「增量更新」只索引新增文件，「全量扫描」遍历后对账数据库记录，库内未软删但源文件已不存在的记录标记软删进回收站（复用 FR-25，不物理删除、不动磁盘）。`POST /api/library/scan/:id` 新增可选查询参数 `mode`（`full`/`incremental`，缺省增量，向后兼容）；对账仅本地扫描启用，SMB 轮询保持增量语义、遍历整体出错时放弃对账以免误删。前端存储库管理页每个库新增「增量更新」「全量扫描」两个入口。
- **扫描任务队列 + 页眉任务展示（FR-29）**：扫描改为持久化任务队列。触发扫描（`POST /api/library/scan/:id`）改为建 `pending` 任务入队、立即返回任务 ID，由单 worker goroutine 串行执行（多次触发按入队顺序排队、不并发抢资源），worker 调用现有 `ScanLibraryWithType` 按任务的 `scan_type`（full/incremental，对接 FR-27）执行扫描。队列以新增 SQLite `scan_tasks` 表为持久化真源，服务重启时把残留 `running` 任务重置为 `pending` 重新入队。新增任务列表端点 `GET /api/library/scan/tasks`（返回任务列表与当前进行中任务，进行中任务进度由实时全局扫描状态覆盖）。前端 `AppLayout` 页眉新增扫描任务指示器：有进行中/排队任务时常驻展示数量徽标，点开展示任务列表与各自进度。
- **定时扫描（FR-28）**：新增后台定时扫描调度器 `library.ScanScheduler`，按设置 `scan_interval`（秒，FR-24 真源）周期性对所有启用媒体库经 FR-29 队列入队**增量**扫描，作为实时文件监听的兜底（捕获网络目录、丢事件、停机期间的增删）。周期 `<=0`/非法即关闭；等满一个周期才首次触发（不在启动/重启时立即扫，避免扫描风暴）；逐库跳过已有 `pending`/`running` 任务的库以防积压。设置页保存 `scan_interval` 后经 `PUT /api/settings` 的设置变更回调 `Reload` 即时重排、无需重启。纯定时组件经函数注入解耦（`-race` 单测覆盖周期触发/关闭/热重载/停止/幂等）；周期复用既有设置页字段，无前端改动。

## 0.4.0（2026-06-22）

### 新增
- **系统诊断页（FR-21）**：新增 `GET /api/system/info`（OS/架构/CPU 数/主机名/Go 版本/应用版本/ffmpeg 版本 + 复用硬件加速检测）与 `POST /api/system/codec-test`（对软件 + QSV/VAAPI/NVENC/AMF/VideoToolbox 的 H.264/H.265 候选编码器用外部 ffmpeg 跑真机试编码，报告是否编入/是否成功/失败尾部）；前端 `/system` 页展示并支持一键复制纯文本报告。编解码器实测走外部 ffmpeg CLI，独立于 CGO 检测，普通构建即可用，专供 FR-10/FR-11 真机验收。
- **跨平台打包（FR-22）**：新增根目录 `Makefile`（`frontend`/`build`/`build-hwaccel`/`package`/`clean`），一键产出单二进制发布包，构建期经 `-ldflags -X main.version` 注入版本号，并把用户自备的 ffmpeg/ffprobe 随包附带；主程序启动按「环境变量 → 可执行文件同目录捆绑版 → PATH」自动发现 ffmpeg/ffprobe，发布包开箱即用。各平台原生构建（CGO）。
- **移动端 PWA（FR-45）**：引入 `vite-plugin-pwa`，构建产物含 `manifest.webmanifest`（中文名称/品牌紫主题色/192·512·maskable 图标/`display: standalone`）与 Service Worker（`autoUpdate`），支持「添加到主屏」并以独立窗口启动、断网时离线加载应用壳；Service Worker 仅预缓存壳静态资源，`/api` 与媒体流运行时走网络不缓存。`index.html` 补 PWA meta（主题色/iOS 主屏/`viewport-fit=cover`），并加移动端安全区与触控优化。可安装/离线属真机维度，自动化覆盖 manifest 字段与 SW 注册逻辑。
- **设置子系统（FR-24）**：新增 `internal/settings` 服务（按 key 读/写、批量 upsert）与 `GET /api/settings`、`PUT /api/settings` 端点，运行期设置以 SQLite `settings` 表为唯一真源、重启后保留；前端新增 `/settings` 设置页（Mantine）与导航项，支持配置「每盘符回收站路径」「扫描周期」等键值，为后续回收站清理（FR-26）、定时扫描（FR-28）提供配置真源。设置持久化决策见 ADR-0029。
- **收藏与标签（FR-41）**：媒体支持标星收藏与自定义标签。新增收藏切换端点 `PUT /api/library/media/:id/favorite`、标签管理端点 `GET/POST /api/library/tags`、媒体打/去/列标签端点 `GET/POST /api/library/media/:id/tags` 与 `DELETE /api/library/media/:id/tags/:tag_id`；`GET /api/library/media` 新增 `favorite`、`tag_id` 筛选参数。前端时间轴媒体卡片加标星按钮，新增「仅收藏」开关、标签筛选下拉与新建标签入口。复用 foundation 已建的 `media_files.favorite` 字段及 `tags`/`tag_mappings` 表。
- **续播与观看状态（FR-44）**：持久化每媒体「上次播放位置」与「已看/未看」标记。新增观看位置上报端点 `PUT /api/play/:id/position`、标记已看端点 `PUT /api/play/:id/watched` 与继续观看列表端点 `GET /api/library/continue-watching`。前端 `VideoPlayer` 载入有进度的视频时定位续播、播放中约每 10s 上报位置、接近片尾自动标记已看；首页时间轴新增「继续观看」区块展示有进度未看完的媒体并可点击续播。记录的是用户观看位置，与 `playback` 模块的转码/缓冲进度独立。复用 foundation 已建的 `media_files.last_position`/`watched`/`last_watched_at` 字段。
- **文件名双模式编辑（FR-30）**：区分「系统内显示名」与「真实文件名」两种修改。新增显示名端点 `PUT /api/library/media/:id/display-name`（仅更新库内 `media_files.display_name`，不动磁盘文件，空串表示清除）；真实文件名修改复用既有 `PUT /api/library/media/:id/rename`（磁盘改名）。列表/卡片/详情展示名统一优先用 `display_name`、为空回退 `file_name`。前端播放页标题区新增「改显示名」「改文件名」入口，两者均经二次确认弹窗后才执行。复用 foundation 已建的 `media_files.display_name` 字段。
- **软删除与回收站（FR-25）**：删除媒体改为软删除——仅置 `media_files.deleted_at`，不物理删除数据库记录、不动磁盘源文件。常规列表与各库已索引计数排除已软删项（`ListMediaFilesFiltered`/`ListLibraryPathViews` 统一 `deleted_at IS NULL` 口径）。新增回收站列表端点 `GET /api/library/recycle` 与还原端点 `POST /api/library/media/:id/restore`；前端新增 `/recycle` 回收站页（展示软删项 + 一键还原）与导航项。复用 foundation 已建的 `media_files.deleted_at` 字段。
- **媒体时间与 EXIF 提取（FR-31）**：扫描入库时为媒体解析「媒体时间」并提取完整 EXIF。图片用 `imagemeta`（纯 Go）读取拍摄时间与相机/镜头/光圈/快门/ISO/GPS，视频用 ffprobe 读 `creation_time`；按 EXIF/拍摄时间 → 文件名日期解析（`IMG_20230101_120000` / `2023-01-01` / `Screenshot_2023...` 等）→ 文件创建时间 → 修改时间的降级链定出 `media_time` 与来源 `media_time_source`，写入 foundation 已建字段。`GET /api/library/media` 新增 `sort=media_time`/`media_time_asc` 排序（按 `COALESCE(media_time, added_at)`），时间轴按媒体时间组织。新增纯 Go 依赖 `github.com/evanoberholster/imagemeta`。
- **HEIC/RAW 图片支持（FR-37）**：识别常见相机 RAW 扩展名（cr2/nef/arw/dng/rw2/raf/orf/srw/pef）并归入图片类。HEIC/RAW 浏览器无法直接渲染，服务端经外部 ImageMagick（`magick`）转成 JPEG：`GET /api/library/media/:id/raw` 对 HEIC/RAW 返回转换后的 JPEG（普通图片仍直出原图），缩略图也改经 magick 生成；转换结果缓存于数据目录下 `image_cache/`（按「源路径 + 源修改时间」hash 命名，二次命中不重转）。`magick` 路径按「`JIANVIDEO_MAGICK_PATH` → 可执行文件同目录捆绑版 → PATH」解析（与 ffmpeg 一致）；未安装时 `raw` 端点返回 `503`、转换失败返回 `500`，均记中文日志、不影响其他功能。外部 ImageMagick 决策见 ADR-0030。真实 HEIC/RAW 显示需目标机安装带 HEIC + RAW delegate 的 ImageMagick（真机维度）。

### 变更
- **用户相册/合集（FR-40）**：新增相册服务与 `GET/POST /api/albums`、`DELETE /api/albums/:id`、`GET/POST /api/albums/:id/items`、`DELETE /api/albums/:id/items/:mediaId` 端点，支持跨目录把任意媒体手动归入相册、浏览相册成员、移出成员；删除相册仅清理 `albums` 与 `album_items`，不删除源文件与 `media_files` 记录。前端新增 `/albums` 相册页（列相册、建相册、删相册、看相册内容、从媒体库加入/移出媒体）与导航入口。
- **文档对齐**：修正 `README.md`、`docs/ARCHITECTURE.md` 中「CGO 绑定 ffmpeg-go 直接调用 libav C API」的过时表述——转码实为外部 ffmpeg 进程调用，CGO 仅用于 SQLite 与可选硬件编码器检测。
- **存储库管理页精简（FR-23）**：`/library-manager` 移除页内媒体文件列表，仅保留存储库卡片（扫描进度 + 已索引媒体数量）；点击卡片携 `library_id` 与起始路径跳转 `/browse` 定位到该库根目录。`GET /api/library/paths` 响应每项新增 `media_count`（该库未软删媒体数量，按 `library_id` 一次 `GROUP BY` 统计，向后兼容）。

### 修复
- **软删项收口访问隔离（FR-25）**：修复已软删（回收站中）的媒体仍可经详情/播放/raw/缩略图等端点访问的隔离差距——此前 `GetMediaFileByID` 不区分软删状态，软删后 `GET /api/library/media/:id`、播放流、`/raw`、缩略图等仍能取到记录。现 `GetMediaFileByID` 增加 `deleted_at IS NULL` 过滤，软删项经上述正常访问路径一律视为不存在（404），仅经回收站列表（`GET /api/library/recycle`）可见；还原（`POST /api/library/media/:id/restore`）走自身查询读取软删记录，不受影响。
- **亮色模式残留深色改用主题感知变量**：修复切换到亮色模式后页面背景、视频播放器控制条、缩略图占位、时间线等位置仍显深色的问题——此前 `index.css` 强制 `color-scheme: dark` 且 body 背景写死 `--mantine-color-dark-8`，播放器控制条与若干组件写死 `var(--mantine-color-dark-N)` 深色兜底，不随主题切换。现 `color-scheme` 改为 `light dark`、body 背景与各表面/前景改用随主题切换的 Mantine 语义变量（`--mantine-color-body`/`--mantine-color-default`/`--mantine-color-default-border`/`--mantine-color-dimmed`），暗色模式保持不变。亮色实际视觉效果属真机/浏览器维度。
- **捕获 ffmpeg stderr 补全缩略图/HLS 失败日志**：修复缩略图与 HLS 预切片失败时日志不足、难以排查的问题——此前图片/视频缩略图 ffmpeg 命令用 `cmd.Run()` 不捕获 stderr，失败仅记一句 `exit status`（无具体原因）；HLS 预切片的分辨率探测丢弃 ffmpeg 输出、m3u8 校验错误不含输出目录、切片/校验失败无统一日志，与成功日志不对称。现缩略图 ffmpeg 命令捕获 stderr，失败按 ERROR 级别记录「文件路径 + stderr 关键尾部（截断 500 字符）」；HLS 探测失败保留 ffmpeg 输出尾部、m3u8 校验错误补全输出目录、切片/校验/保存失败统一记 ERROR 级日志含 mediaID/输出目录/档位上下文，成功失败对称、不再静默吞错。
- **缩略图生成限并发并加 ffmpeg 超时**：修复扫描大目录时缩略图生成卡死整个页面的问题——此前每个文件直接 `go` 启动 ffmpeg/magick，无并发上限、ffmpeg 无超时，扫描大库会瞬间炸出成百上千并发进程导致资源耗尽。改为所有缩略图任务统一经固定容量信号量限并发（`min(4, CPU 核数)`），并为 ffmpeg 图片/视频缩略图命令加 30s 超时（`exec.CommandContext`，超时即终止并记中文 WARN 日志，不挂起）。保持异步生成、不阻塞扫描。
- **扫描期媒体富化改有界并发，提速大库扫描（FR-31）**：修复 FR-31 媒体时间/EXIF 提取在扫描入库时同步逐文件调用 ffprobe、大库扫描串行过慢的问题。扫描对去重后的新文件列表改用固定容量信号量（`min(4, CPU 核数)`，与缩略图一致）有界并发处理，单文件内富化仍同步（`media_time` 入库即写），总耗时约降为 1/cap，又不致瞬间炸出大量 ffprobe 子进程；入库计数用原子操作、写入经 SQLite WAL 串行、进度状态互斥更新，并发安全（`go test -race` 无竞争）。

### 安全
- **全 `/api` 路由强制鉴权（FR-13）**：修复未授权访问漏洞——此前仅 `/api/me` 挂了认证中间件，库、播放、HLS 切片、stream、raw、缩略图、设置、相册、SMB 凭据、系统诊断、转码等路由均注册在裸引擎上，未登录即可访问与修改全部数据和媒体。新增全局 `auth.APIGuard` 中间件，对路径前缀为 `/api/` 但不属于 `/api/auth/` 的请求强制校验 JWT（Cookie `auth_token` 或 `Authorization: Bearer` 任一有效即放行，均无效返回 `401`）；`/api/auth/login`、`/api/auth/logout`、`/health`、静态资源 `/assets/*` 与 SPA 回退保持豁免，保证未登录也能加载前端壳与登录。
- **SMB 主密码强制显式配置（FR-02）**：`smb.MasterPassword()` 不再在 `SMB_MASTER_PASSWORD` 未设置时回退公开弱默认值，改为返回错误；显式空串同样视为未配置。未配置时全部 Save/Load 调用点拒绝以弱密钥加解密——`POST /api/smb/credentials` 返回 `503`，添加 SMB 库路径仅记录 ERROR 不落弱密钥，SMB 扫描/播放返回明确错误。消除「拿到 `smb_credentials.enc` 即可用公开常量离线解出明文密码」的风险。同步更新 `docs/API.md`、`docs/OPERATIONS.md`。

## 0.3.0（2026-06-21）

### 修复
- **SMB 凭据（FR-02）主密码一致性与密码持久化**：修复保存时用 `SMB_MASTER_PASSWORD`/默认主密码、加载时却用空串，导致加密与解密主密码不一致、凭据永远无法读回的缺陷；同时修复 `Credentials.Password` 因 `json:"-"` 未写入加密文件、SMB 连接始终使用空密码的问题。新增 `smb.MasterPassword()` 作为加解密主密码唯一来源，统一全部 Save/Load 调用点（`saveSMBConfig`/`SaveSMBCredentials`/`library`/`playback`）。

### 变更
- **ABR 自适应码率（FR-07）真机验收交付**：多码率 master.m3u8 + hls.js ABR 链路（代码自 v0.1.0 已落地、未变更）经真机验证（`blob:` MSE 播放、多码率清单生效），状态由开发中转为已交付。
- **双进度条（FR-20）真机验收交付**：VideoPlayer 缓冲进度 + 播放进度双滑块（代码自 v0.2.0 已落地、未变更）经真机验证，状态由开发中转为已交付。
- **接口契约（FR-02）**：`POST /api/smb/credentials` 请求体移除 `master_password` 字段，加解密主密码改由服务端 `SMB_MASTER_PASSWORD` 环境变量统一管理；同步更新 `docs/API.md`、`docs/OPERATIONS.md`。

## 0.2.0（2026-06-21）

### 新增
- **播放器（FR-18）末端缓冲等待**：VideoPlayer 监听 mpegts.js `error` 事件 + 原生 `<video>` `waiting`/`stalled` 事件，进入等待态展示「等待新数据…」横幅；mpegts.js 报错后 1s 自动 `unload + load` 重载，新数据可用时 `canplay`/`playing` 自动复位。

### 修复
- **转码（FR-10/11/12）**：挂载硬件加速能力查询端点 `GET /api/transcode/hwaccel`。此前 HWAccelHandler 已实现但未注册路由，请求被前端 SPA 回退（NoRoute）接管返回 HTML 而非硬件能力 JSON，与 ARCHITECTURE §1/§4 不符；现已注册并返回 available/preferred/h264_supported 等字段。
- **监听器（FR-03）**：修复 main.go 未启动文件监听——watcher 包齐备且单测全绿，但程序入口从未实例化或 Start，导致新增/删除文件不自动入库（真机 35s 不入库）。修复后实测加文件 1s 入库（spec ≤30s）、删文件 1s 移除（spec ≤10s）。

### 变更
- **文档对齐**（期验收 P1 发现）：PRD AC-13 由 `/timeline` 改为根路由 `/`（FR-A 已重排）；FR-10/11 状态注「检测/单测就绪；本机无 Intel/NVIDIA，硬件真机待验」；`docs/specs/smart-playback.md`（FR-05）改为前端探测 `/api/play/hls/:id/master` 不可用即降级 `/api/play/:id/stream`，不再单提 `playinfo` 端点；`docs/specs/mpegts-player.md`（FR-16）澄清 mpegts.js 仅约束 TS/HLS 转码流，直出 MP4 经 `/stream` 允许原生 `<video>`。

## 0.1.0（2026-06-20）

### 新增
- 外挂字幕支持（FR-04）：后端 SRT/ASS→WebVTT 转换 API、前端字幕 overlay 渲染与轨道选择
- 实现 SMB/CIFS 网络共享支持（FR-02）：原生 SMB 连接管理、凭据 AES-GCM 加密存储、SMB 目录扫描与视频播放流
- **ABR 自适应码率（FR-07）**：后端 MultiPipeline 单进程多输出 1080p/720p/480p 三档 HLS 切片，码率阶梯根据源分辨率自动裁剪；新增 master.m3u8 生成与路由；前端 VideoPlayer 支持 hls.js ABR 模式（动态 import），自动回退 mpegts.js
- 初始化项目结构与 SDD 治理文档
- 实现 fsnotify 实时路径监控（FR-03）：递归监听媒体库目录，视频文件自动入库/移除，500ms 去抖机制
- 前端全面重构：引入 Mantine UI v9 暗色主题，替换手写 Tailwind 原子类
- 完善登录页（FR-13）：Mantine 表单 + zustand 认证状态管理 + 路由守卫
- 完善媒体库管理页（FR-01/FR-14）：路径增删、扫描、媒体列表/搜索/分页
- 新增文件目录浏览 API（FR-15）：`GET /api/library/browse` 按目录层级浏览媒体文件，支持面包屑导航、子目录列表、媒体文件列表，前端 Tab 切换（时间轴 | 文件目录）
- 完善视频播放页（FR-16）：VideoPlayer 接入 HLS 流
- 路由改造：修复 catch-all 通配符冲突，统一 BrowserRouter 模式
- 后端异步扫描 + SSE 进度推送（FR-C）：扫描改为后台 goroutine 异步执行，新增 `GET /api/library/scan/progress` SSE 端点实时推送已扫描数/总数/状态，前端展示扫描进度条并在完成后自动刷新
- 缩略图系统（FR-D）：后端扫描时通过 ffmpeg 异步生成 320px 缩略图（视频取第 2 秒帧、图片缩放），新增 `GET /api/library/thumbnail/:id` 端点，前端媒体卡片改用缩略图加载（图片预览弹窗仍用原图）
- 页面流程重设计（FR-A）：原综合媒体库页拆分为存储库管理 `/library-manager`、时间轴 `/`、目录浏览 `/browse` 三个独立页面，AppLayout 导航在三者间切换；管理页支持媒体文件删除与重命名（新增 `PUT /api/library/media/:id/rename` 磁盘改名端点）
- 时间轴视图重做（FR-B）：按 `added_at` 日期分组，左侧竖向日期轴 + 右侧缩略图网格，视频与图片均展示缩略图
- 虚拟列表 + 懒加载（FR-E）：时间轴与目录浏览改用 `@tanstack/react-virtual` 窗口虚拟滚动，只渲染可见区 + overscan；时间轴改为滚动到底自动加载更多（替代分页），缩略图 `loading="lazy"`
- 暗色模式 + 路由守卫 + 全局错误处理（FR-G）：MantineProvider 接入 `localStorageColorSchemeManager` 持久化主题，顶栏新增明暗切换按钮；新增 ProtectedRoute/RequireAnon 路由守卫（未认证跳登录、已认证访问登录页跳首页）；新增 `handleApiError` 工具，Axios 拦截器对网络错误统一 toast

### 变更
- 前端代码结构拆分（FR-F）：LibraryPage 拆分为 LibraryPathManager / MediaTimeline / DirectoryBrowser 等子组件与 useLibraryPaths / useMediaList / useDirectoryBrowse hooks，VideoPlayer 改用 Tabler Icons 与 Mantine 样式，删除 App.css 模板代码
- 扫描接口改为异步（FR-C）：`POST /api/library/scan/:id` 不再等待扫描完成，立即返回 `{"status":"scanning"}`，实际进度经扫描进度 SSE 端点获取

### 修复
- **时间轴（FR-E）**：修复无限滚动失效——滚到底不再加载更多、永远停在首页 60 条。改用 IntersectionObserver 底部哨兵触发加载（替代依赖虚拟化 lastIndex 变化的脆弱判定），并在 useInfiniteMedia 用同步标记防止重复拉取。
- **扫描进度（FR-C）**：修复扫描完成后 SSE 重连风暴——浏览器 EventSource 每 ~3 秒重连并重复触发列表重拉。后端 SSE 改为保持连接打开、仅在状态变化时推送；前端按 `completed_at` 变化判定「新完成」才刷新，避免重复刷新且兼容快扫描。
- **媒体库**：修复本地扫描只读取第一层目录的问题，改为递归扫描并按目录类型分发 local/SMB。
- **媒体库**：统一图片/视频后缀识别策略，新增按 `LibraryPath` 绑定的自定义后缀，删除目录时同步清理，避免污染全局。
- **媒体库**：新增图片 raw 预览接口，前端时间轴与目录视图点击图片打开预览，视频仍跳转播放页。
- **前端**：补齐独立 `/timeline` 路由和侧边栏/移动抽屉入口，时间轴默认显式请求 `sort=time_desc`。
- **前端**：修复 Windows 路径下目录浏览面包屑出现 `/D:` 的问题，并为扫描提供可感知加载态。
- **安全**：移除硬编码默认 JWT 密钥，改为启动时生成随机密钥并提示用户设置 `JWT_SECRET` 环境变量
- **安全**：HLS 切片路由增加路径遍历防护（过滤 `..` 和 `/`）
- **安全**：Cookie 添加 `SameSite=Strict` 属性，防止 CSRF 攻击
- **安全**：Content-Disposition 文件名使用 `url.PathEscape` 转义，防止 HTTP 头注入
- **并发**：修复 `GetProgress` 中 `BufferedRanges` 的锁外反序列化数据竞争
- **并发**：客户端断开后流式传输 goroutine 主动 cancel context，防止 goroutine 泄露
- **并发**：HLS m3u8 writer 确保 Close() 总是被调用并写入 `EXT-X-ENDLIST`
- **并发**：播放会话 map 增加 30 分钟 TTL 清理机制，防止内存泄露
- **CGO**：修复 `findEncoderByName`/`findQSVEncoder` 违反 CGO 指针规则的问题（改为返回 `bool`）
- **基础设施**：硬件检测失败时降级继续而非阻断整个流程
- **基础设施**：`detectOnce` 增加 `recover()` 防止 panic 后返回 nil
- **基础设施**：`TranscodeSession.Start()` 增加重复启动检查
- **基础设施**：移除冗余的 `killProcessGroup` 调用（`exec.CommandContext` 已自动处理）
- **基础设施**：`isIntelGPU()` 添加 `//go:build linux` 平台构建标签
- **基础设施**：`setupTestDB` 从业务代码迁移到测试工具文件
- **性能**：`ScanLibrary` N+1 查询改为批量查询 + map 查找
- **性能**：流式传输从逐包 Flush 改为 `io.CopyBuffer`
- **性能**：HLS `GetM3U8`/`GetSegment` 锁内不再执行磁盘 I/O
- **健壮性**：ffprobe 增加 10 秒超时，防止永久阻塞
- **健壮性**：`envInt` 无效值增加警告日志
- **健壮性**：搜索参数中 `%` 和 `_` 通配符转义
- **健壮性**：DB 插入失败增加 WARN 日志
- **测试**：`play_handler_test.go` 宽松断言改为精确状态码断言
- **测试**：`handler_test.go`、`jwt_test.go`、`library/service_test.go` 补充错误路径和边界条件测试
- **测试**：`watcher_test.go` 增加超时时间减少 flaky 风险
- **前端**：Mock 数据统一为单一数据源（`mocks/data.ts`）
- **前端**：VideoPlayer 事件监听器在 `destroyPlayer` 中清理
- **前端**：`LibraryPage` 工具函数提取为模块级
- **前端**：auth store 移除硬编码 `'cookie_auth'` 字面量
- **前端**：mock 模式从运行时 `localStorage` 判断改为构建时 `VITE_USE_MOCK` 环境变量
- **规则**：`architecture-invariants.md` ORM 规则修正为允许 GORM（ADR-0023）
- **规则**：`architecture-invariants.md` 删除 Bukkit/Spigot 残留内容

### 移除
（无）

### 修复（二期 FR 审查修复）
- **CRITICAL**：SMB 流式播放中 `smbReadSeeker.Close()` 不再调用 `client.Disconnect()`，避免 HTTP 分块传输中途断开 SMB 会话
- **CRITICAL**：`GetProgress` 的 `exists` 检查移入 `RLock` 内，消除 nil 指针解引用风险
- **HIGH**：`openSMBFile` 加载凭据后检查 `creds == nil`，给出明确错误提示而非 panic
- **HIGH**：`saveSMBConfig` 主密码改为从 `SMB_MASTER_PASSWORD` 环境变量读取
- **HIGH**：`router.go` 所有播放路由添加 `parseMediaID` 错误处理，与 HLS 路由一致
- **HIGH**：`MultiPipeline.RunMulti` 添加 `dsts` 参数被忽略的 WARN 日志
- **HIGH**：`Credentials` 增加 `Domain` 字段，支持企业 Windows AD 环境
- **MEDIUM**：`Service.smbCreds` 添加 `smbCredsMu` 读写锁保护并发安全
- **MEDIUM**：`Watcher.pathToLib` 读取加 `RLock` 保护
- **MEDIUM**：`HLSSegmentWriter.Close` 添加 `closed` 标志防止重复关闭
- **MEDIUM**：`WriteSegment` 移除每次 `Sync()`，改为 `Close()` 时一次性 sync
- **MEDIUM**：`BrowseDirectory` 入口添加 `filepath.Clean` + `..` 路径遍历校验
- **MEDIUM**：`smbfs.normalize` 添加 `..` 过滤
- **MEDIUM**：`smb.Client.EnsureConnected` 使用 `sync.Once` 消除竞态窗口
- **MEDIUM**：`GetSubtitles` 对 SMB 路径返回空列表
- **MEDIUM**：`GetSubtitleContent` 空内容返回 204
- **MEDIUM**：`LibraryPage` catch 块不再静默吞错，添加错误状态和 UI 提示
- **MEDIUM**：`LibraryPage.activeTab` 同步到 URL query 参数
- **MEDIUM**：`LibraryPage` paths 变化时使用 `useRef` 追踪初始化，不重置浏览状态
- **MEDIUM**：`SubtitleEntry` 接口统一到 `types/index.ts`，消除重复定义
- **MEDIUM**：`VideoPlayer` 自动播放被阻止时显示"点击播放"提示
- **MEDIUM**：`VideoPlayer.hlsRef` 类型从 `unknown` 改为 `Hls`
- **LOW**：`master_test.go` 使用 `strconv.Itoa` 替代自定义 `itoa`
- **LOW**：`credentials.go` 添加 `saltLen`/`nonceLen` 常量
- **LOW**：`Watcher.Stop` 先关闭 watcher 再关闭 done 通道
- **LOW**：`Credentials.Password` 添加 `json:"-"` 标签
- **LOW**：新增 `credentials_test.go` 覆盖加解密 roundtrip、错误密码、空输入

> 发版时把"未发布版本"段切成 `## [X.Y.Z] - YYYY-MM-DD`，再新建空的"未发布版本"段。
