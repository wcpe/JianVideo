# 变更日志

本项目所有重要变更记录于此。

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布版本

### 新增
- **硬件加速检测统一为实测真源 + 持久化缓存 + 全编码探测（FR-49）**：此前硬件加速列表（`/api/transcode/hwaccel`，CGO 只查编码器是否编入、硬编 NVENC+QSV）与编解码器实测（`/api/system/codec-test`，外部 ffmpeg 真实试编码、含 AMF）是两套独立逻辑，导致 AMD 的 `h264_amf`/`hevc_amf` 实测成功却不显示在硬件加速列表，且实测无缓存每次重跑约 3 分钟、能力模型只认 H.264/H.265。改为以**编码器实测为单一真源**：结果按 ffmpeg 版本持久化于 SQLite（`codec_probe_caches`，版本变更失效重测 + 手动「重新测试」），硬件加速列表/系统信息/转码选码统一读这份缓存；能力模型重构为 **per-codec**（每家族逐编码记录可用性，家族可用=至少一编码试编码成功）；补齐 **AMD AMF（含 `av1_amf`）、VAAPI、VideoToolbox、Vulkan** 全家族与 **AV1/VP9** 探测（`av1_nvenc`/`av1_qsv`/`av1_amf`/`av1_vaapi`/`av1_vulkan`/`libsvtav1`、`vp9_qsv`/`vp9_vaapi`/`libvpx-vp9`）；启动时后台预热缓存（不阻塞、单航道防重复），GET 冷态返回「未测」不触发同步实测。前端硬件加速卡片改为按家族×编码逐项展示、标示缓存来源与「重新测试」。顺带修正 `intel_gpu` 假阳性（仅 qsv 实测可用或 sysfs 确认 Intel 核显才标记，不再因候选恒含 qsv 而误报）。AMD 真机验收：`/api/transcode/hwaccel` 与 `/system` 页正确显示 `h264_amf`/`hevc_amf` 可用、`preferred=h264_amf`、可输出编码含 av1/vp9（软件）、`intel_gpu=false`。取代 [ADR-0015](docs/adr/0015-hw-fallback.md)（见 [ADR-0033](docs/adr/0033-hwaccel-probe-source-cache.md)）。转码输出仍固定 H.264（AV1/VP9 实际用于输出与自适应播放属 FR-50~53）。
- **转码目标编码可配置（FR-50）**：转码「目标输出编码」由固定 H.264 改为可配置。服务端以 `settings` 表持久化「首选目标编码优先级」（键 `transcode_codec_priority`，JSON 数组，如 `["av1","h265","h264"]`），写入时按 FR-49 实测可输出集校验（非法 / 不支持 / 重复编码整体拒绝）。单/多码率管道按所选编码参数化输出——编码器名由 `SelectEncoderForCodec` 选取，像素格式与关键参数由纯函数 `CodecOutputParams`（h264/h265/av1/vp9）映射，替代原先硬编 H.264。**默认仍 H.264**（未配置 / 非法回落 `["h264"]`，`NewPipeline()` 等价 `NewPipelineForCodec("h264")`，参数字节级不变、mpegts.js 可播）。输出容器不随编码改变（playback 仍 TS、ABR 仍 HLS，FR-06 播放路径不动），故非 H.264 端到端可播留 FR-51/52。真机验证：首选 av1 → 选码 `libsvtav1`、生产管道产出非空 TS、ffprobe 断言编码 av1（硬件 AV1 待硬件）。扩展 [ADR-0003](docs/adr/0003-hls-ts-streaming.md)（见 [ADR-0034](docs/adr/0034-configurable-target-codec.md)）。
- **高级编码 fMP4/CMAF 输出与 MSE 播放路径（FR-51）**：H.264 维持 mpegts.js + MPEG-TS + HLS 路径不变（追播/边下边播/Seek 内核不动），非 H.264 编码（H.265/AV1/VP9）新增 **fMP4/CMAF 分片 + 浏览器原生 MSE** 播放路径——后端 `RunFMP4ToDir` 用外部 ffmpeg 产出 HLS-fMP4（`init.mp4` + `seg_NNN.m4s` + `index.m3u8`，含 `EXT-X-MAP`/`EXT-X-ENDLIST`，VOD），强制 8-bit `yuv420p`、固定 GOP、HEVC 打 `hvc1` tag、音频转 AAC；`SelectOutputPath` 纯函数按目标编码分发（h264→TS 路径分支不动、其余→fMP4），编码器经 `SelectFMP4Encoder` 复用 FR-49 实测快照按硬件优先级选取、软件兜底；HLS 端点扩展 `.m4s`（`video/iso.segment`）/init `.mp4`（`video/mp4`）MIME 识别。定义前端契约（容器/codec MIME 串 `hvc1`/`av01`/`vp09`/URL 方案/清单格式）供 FR-52 消费。**本期 fMP4 路径仅 VOD，不实时追播**（实时 fMP4 追播属后续阶段）。软件维度真机验证：经 `RunFMP4ToDir` 用 `libsvtav1`/`libx265`/`libvpx-vp9` 实转，`ffprobe` 断言容器=`mov,mp4` 且编码=`av1`/`hevc`/`vp9`、tag=`av01`/`hvc1`/`vp09`；硬件 AV1/QSV/NVENC 维度验收机无对应硬件待硬件。扩展播放内核决策见 [ADR-0035](docs/adr/0035-fmp4-mse-playback-path.md)。
- **前端客户端能力探测 + 自适应播放器（FR-52）**：消费 FR-51 前端契约，前端新增客户端编码能力探测与自适应播放分发。能力探测（`utils/codec-capability.ts`）以 `MediaSource.isTypeSupported` 探测本浏览器在 fMP4 容器下可解码哪些高级编码——`codecMIME` 给出与后端 `FMP4CodecMIME` 字节级一致的 MIME 串（`hvc1`/`av01`/`vp09`）、`isCodecSupported` 归类单编码、`probeClientCapabilities` 返回 `{h265,av1,vp9}` 能力描述（供 FR-53 协商上报）。自适应播放器（`VideoPlayer`）新增可选「播放描述符」入参（目标编码 + 清单 URL + 播放路径 `ts`/`fmp4`/`mp4` + 可选回退源），按路径分发——`ts`（H.264）走现有 mpegts.js（含 master.m3u8 ABR，**追播路径与实现不动**）、`fmp4`（高级编码）走 hls.js 原生 fMP4+MSE 加载 `index.m3u8`、`mp4` 走原生 video；缺省描述符时行为与现状字节级一致（现有调用方零改动、回归全绿）。**不支持回退**：分发到 fMP4 前先 `isCodecSupported` 校验，不支持且有回退源时回退 H.264/TS（不抛 Network Error），无回退源则展示「当前浏览器不支持该视频编码」提示。复用 [ADR-0035](docs/adr/0035-fmp4-mse-playback-path.md) 与 [ADR-0026](docs/adr/0026-abr-adaptive-bitrate.md)（无新 ADR）。**端到端「按客户端能力选编码 + 触发后端产 fMP4 + PlayPage 接线」属 FR-53**，完整真实媒体端到端真机待 FR-53 接线后整体进行。
- **端到端编码协商（FR-53）**：把 FR-49/50/51/52 串通——播放发起时做一次服务端协商，**按「首选优先级 ∩ 客户端能力 ∩ 实测可产出」选出实际输出编码与播放路径**，含降级兜底、会话记录实际编码与路径。协商纯函数 `ChosenCodec(priority,clientCaps,producible)`（首选里第一个客户端支持且系统可产出的编码，都不满足兜底 `h264`，可穷举单测）；新增端点 `POST /api/play/:id/negotiate`（客户端上报 `{h265,av1,vp9}`，读 FR-50 优先级 + FR-49 可产出并集协商，返回播放描述符 `{codec,path,url,mime,fallback_url}`）；非 H.264 同步调 FR-51 `PreSliceWithCodec` 产 fMP4，**产出失败降级回 H.264/TS**（不报错）；前端 `PlayPage` 探测客户端能力 → 请求协商 → 协商出 fMP4 则交 FR-52 自适应播放器（hls.js 原生 MSE），协商出 `h264`/协商失败则沿用既有 master 探测（H.264 回退）；协商结果记到内存播放会话（`PlaybackSession.target_codec`/`output_path`）。**软件 AV1 端到端真机验证**：设首选 `["av1","h264"]` + Chrome（支持 AV1）→ 协商出 av1 → 后端 `libsvtav1` 产 fMP4（ffprobe：容器 `mov,mp4`、编码 `av1`、tag `av01`）→ hls.js + 原生 MSE 实播（`videoWidth=640`/`videoHeight=360` 解码出帧、`readyState=4`、`buffered=[0,4.04]`、无 error）；H.264 客户端 → 协商出 `h264`/TS 走 mpegts.js。硬件 AV1/QSV/NVENC 端到端待对应硬件。协商点 + 端点契约决策见 [ADR-XXXX](docs/adr/XXXX-codec-negotiation.md)（号待整合分配）。

## 0.7.1（2026-06-23）

### 修复
- **自更新「测试版」频道错拉正式版、无法识别开发预览（FR-46）**：检测时按「整体最新 Release」选取，而 GitHub 把正式版排在滚动 `dev` 之前，导致测试版频道仍命中正式版（已发 0.7.0 后测试版仍显示 0.7.0、无更新）；且滚动 `dev` 的 tag 恒为 `dev`（非语义版本）无法参与版本比较。改为**按频道选对应 `prerelease` 标志的最新项**（正式版取 `prerelease=false`、测试版取 `prerelease=true`），测试版从 Release 名提取内嵌版本（如「开发预览（dev · 0.7.1-dev.<sha>）」）并按版本串不等判定更新；频道选择持久化到设置 `update_channel`（检查/更新不带 channel 时取设置）；前端频道标签由「稳定/预发布」改为「正式版/测试版」。对真实仓库验证：正式版→0.7.0 无更新、测试版→开发预览 有更新。
- **开发预览版本号比正式版还旧、发布说明为空（FR-48）**：预发布工作流原以 `$(cat VERSION)-dev.<sha>` 作版本号，而 `VERSION` 发版后不上抬，导致发布 0.7.0 后开发预览仍是 `0.7.0-dev.<sha>`——语义化版本里它**小于**已发布的 0.7.0（比正式版还旧），测试版频道会把它当「更新」推送（实为降级）。改为基于最新正式版 tag 用 `git describe` 推导下一修订号作 dev 基线（`v0.7.0 → 0.7.1-dev.<sha>`，始终领先于上个正式版），checkout 取全部 tag；并补齐开发预览的发布说明（滚动预览提示 + 版本/提交 + 自上个正式版以来的 CHANGELOG 未发布段）。
- **10-bit HEVC 源转码后不可播放**：转码（单/多码率管道）未强制 8-bit `yuv420p`，10-bit 源（如 HEVC Main 10）会编出 `High 10`（`yuv420p10le`）的 10-bit H.264，浏览器与 mpegts.js 均无法解码，表现为 HLS「能生成但不可播放」（如 TS/hevc/aac 1280×720）。多码率 scale 链加 `,format=yuv420p`、单管道加 `-pix_fmt yuv420p`，统一输出 8-bit H.264；以 10-bit HEVC/AAC TS 样片复现并验证修复后产物为 8-bit `High`。
- **release 构建中 gin 开启 debug 模式**：发布二进制默认运行于 gin debug 模式，启动刷屏 `[GIN-debug]` 路由表与警告。改为默认 release 模式（仅 info 级请求日志），仅当环境变量 `JIANVIDEO_DEBUG=1/true` 时启用 debug（在创建 gin 引擎前 `SetMode`）。

## 0.7.0（2026-06-22）

### 新增
- **远程自更新（FR-46）**：系统诊断页新增「应用更新」——频道切换（稳定/预发布）、检查更新（展示当前 vs 最新版本与发布说明）、一键更新并重启（二次确认）、回滚到上一版。后端 `internal/update` 经 GitHub Releases 检测/按平台选资产/下载/校验 sha256/替换运行中二进制（改名旧 exe 绕 Windows 文件锁）/自动重启/回滚；`main.go` 监听带端口重试以完成重启接管；API `GET/POST /api/system/update/{check,apply,rollback}`（复用 FR-13 鉴权）。校验失败/缺产物/非更新版本一律拒绝替换。单测覆盖版本比较/资产匹配/checksums 校验/频道选择/校验失败拒绝。对真实 Release 的端到端替换重启需 push 后真机验证（见 ADR-0032）。
- **自动发布 CI（FR-47/FR-48）**：新增 GitHub Actions 工作流——`build.yml`（可复用：`ubuntu-latest`/`windows-latest` 原生 runner 各自 CGO 原生构建前端 `go:embed` + 注入版本号的单二进制 + sha256 校验和）；`release.yml`（推送版本 tag `vX.Y.Z` 或手动触发 → 多平台构建 + 创建正式 GitHub Release，附产物 + `checksums.txt`）；`prerelease.yml`（普通 push 或手动触发 → 滚动刷新 `dev` 预发布）。不引 Docker、不交叉编译，沿用 ADR-0027 的「各平台原生构建」并扩展到 CI（见 ADR-0032）。CI 全绿/Release 真出现需推送线上仓库后验证。

### 变更
- **真机验收归真**：FR-21（系统诊断 + 编解码实测）、FR-22（跨平台打包·Windows 单二进制）、FR-45（移动端 PWA：SW 注册激活 + 可安装 manifest + 离线壳）经真机验收通过，PRD 标记已交付@v0.6.2——功能代码随 v0.6.2 发布，本条仅记录验收归真、无代码变更。FR-10/11（Intel QSV/VAAPI、NVIDIA NVENC）与 FR-22 的 Linux 维度因验收机无对应硬件/环境，保持开发中待真机。

## 0.6.2（2026-06-22）

### 修复
- **扫描入库未提取视频时长/编码/分辨率（FR-31）**：库扫描入库视频时，`enrichMediaMetadata` 此前仅用 ffprobe 读取容器 `creation_time` 定媒体时间，从未探测时长/编码/分辨率，导致 `media_files` 的 `duration`/`video_codec`/`audio_codec`/`width`/`height`/`bitrate` 恒为零值，FR-34 详情面板的「分辨率/时长/视频编码/音频编码」始终空白。改为入库时一次 `ffprobe -show_format -show_streams` 同时取容器时长/码率/`creation_time` 与首个视频/音频流的编码及分辨率并写库（library 包直接用注入的 ffprobe 路径，不依赖 transcoder 以守模块依赖方向）。新增 ffprobe JSON 解析纯函数单测与「真实 ffmpeg 生成视频→入库→断言字段非零」的集成回归用例。
- **HEIC/RAW 转换在 ImageMagick 7 下失效（FR-37）**：`buildMagickConvertArgs`/`buildMagickThumbnailArgs` 把 `-auto-orient` 等图像算子放在输入图像之前，ImageMagick 7 的 `magick` 按左到右解析时报「no images found for operation `-auto-orient`」，致 HEIC/RAW 的 `/raw` 与缩略图转换在真机 IM7 下全部失败（此前单测只断言命令含输出路径、未校验算子相对输入的位置，故未发现；真机装 IM7 才暴露）。改为输入图像在前、算子在后；强化单测断言 `-auto-orient`/`-thumbnail` 位于输入之后。真机以 ImageMagick 7.1.2（libheif+libraw）端到端验证：真实 HEIC→JPEG、真实 Sony ARW→JPEG + 缩略图 + 缓存命中。
- **SMB 连接缺省端口（FR-02）**：`smb.Client.Connect` 直接以 `creds.Host` 作为拨号地址传给 go-smb2 的 `Dial`，host 未带端口（如 `localhost`、`192.168.x.x`）时报「missing port in address」致连接必失败。新增 `normalizeDialAddr`：host 无端口补默认 SMB 端口 445、已带端口原样保留，附纯函数单测。真机以本地 SMB 共享端到端验证：连接成功、扫描入库 4 个文件（`smb://` 路径），错误凭据时优雅失败不崩溃（FR-02 高风险区「凭据错误处理」）。

## 0.6.1（2026-06-22）

### 修复
- **PWA 资源被 SPA 兜底（FR-45）**：内嵌前端服务此前仅把 `/assets/*` 作静态服务，其余路径一律由 `NoRoute` 回退 `index.html`，导致 vite-plugin-pwa 产在 dist 根目录的 `sw.js`、`manifest.webmanifest`、`workbox-*.js` 在打包二进制中被当作 SPA 路由返回 `text/html`——Service Worker 无法注册、manifest 无效，实际部署中 PWA 安装/离线失效。改为 SPA 兜底前先尝试从内嵌 `frontend/dist` 命中根级真实文件并按真实 MIME 返回（`.webmanifest` → `application/manifest+json`），命中不到才回 `index.html`。新增 web 层回归用例断言这些根资源返回正确 Content-Type（非 `text/html`）。此前 FR-45 的 vitest 仅 mock 测 manifest 字段与 SW 注册逻辑，未覆盖内嵌服务器是否真把文件服出，故 bug 被掩盖、真机 curl 才暴露。
- **EXIF 光圈格式化（FR-31）**：图片 EXIF 光圈此前用 `float64` 精度格式化 imagemeta 的 `float32` 光圈值，对非整光圈（如 f/1.8、f/2.8）会输出 `f/2.799999952316284` 之类的精度噪声串，并经 FR-38 详情面板 / 时间轴对用户可见。改为按 `float32` 精度取最短往返表示，正确显示 `f/2.8`。快门、镜头、GPS 提取本即正确，未改动；以真实带 GPS 样片新增回归用例。

## 0.6.0（2026-06-22）

### 新增
- **照片地图视图（FR-39）**：新增 `/map` 页，基于照片 EXIF GPS（FR-31）在 OpenStreetMap 在线瓦片上展示地理分布，标记弹窗显示缩略图与名称。引入 `leaflet` + `react-leaflet`（+ devDep `@types/leaflet`）+ OSM 在线瓦片（无 token/账号，见 ADR-0031）。后端 `GET /api/library/media` 新增 `has_gps=true` 结构化筛选（`gps_lat != 0 OR gps_lon != 0`，并入 FR-35 `MediaFilter`，参数化）；地图页分页累积拉取地理标记子集。导航新增「地图」入口。瓦片显示依赖联网、真实 GPS 依赖 FR-31 真机提取。
- **目录资源管理器视图（FR-33）**：目录浏览增强为类资源管理器——展示方式切换（列表详情 / 大-中-小图标，缩略图密度随档位）、排序切换（名称 / 大小 / 类型 / 修改时间，目录恒在前）、单选 + `Shift` 区间选 + `Ctrl/Cmd` 切换（选中高亮），**双击**文件打开 FR-34 详情面板（取代原单击打开）。目录页接入 `MediaDetailPanel` 并移除已无引用的 `ImagePreviewModal`。为支持多档位与选择，目录视图改用常规网格/列表（单目录条目有界；时间轴大列表仍保留虚拟化）。
- **时间轴缩放与按媒体时间组织（FR-32）**：时间轴改为按**媒体时间**（FR-31，缺失回退入库时间）组织（`sort=media_time`），新增缩放粒度控件（日/月/年）：日按 `YYYY-MM-DD`、月按 `YYYY-MM`、年按 `YYYY` 分组，日期轴标签随粒度自适应；拖动浏览复用现有虚拟滚动。
- **前端筛选/搜索 UI（FR-36）**：时间轴与目录视图均接入表达式搜索框 + `MediaQueryFilters` 结构化筛选控件（类型「全部/图片/视频」、最小大小预设、拍摄时间范围）。时间轴页经 `useInfiniteMedia` 透传 `type`/`size_min`/`time_from`/`time_to`（筛选变化即重置首屏）；目录页（BrowsePage）有筛选/搜索时按当前目录路径（前缀，递归）查媒体接口展示匹配结果、无筛选时仍浏览文件夹树——两者均消费 FR-35 引擎，搜索框支持 `ext:`/`type:`/`size:` 表达式语法提示。
- **媒体筛选与表达式查询引擎（FR-35）**：`GET /api/library/media` 的 `search` 升级为 everything 式表达式（`library.ParseSearchExpression`）：裸词→文件名包含（多词 AND）、`ext:jpg,png`→扩展名、`type:image|video`→类型、`size:>10mb`/`<=2gb`/`>=500kb`（单位 b/kb/mb/gb/tb）→大小；纯文本向后兼容。另新增结构化查询参数 `type`/`size_min`/`size_max`/`time_from`/`time_to`（按 `COALESCE(media_time, added_at)` 比较）/`path`（目录前缀）。表达式只解析为结构化 `MediaFilter` 字段、全部走参数化查询，无 SQL 注入面；类型按内置图片扩展名集合粗筛。
- **EXIF 详情展示（FR-38）**：文件详情面板右侧新增 EXIF 区块（有数据才显示），展示拍摄时间（含来源标注 EXIF/文件名/创建/修改）、相机、镜头、光圈、快门、ISO、GPS 坐标，并对有 GPS 的媒体提供「在外部地图打开」链接（OpenStreetMap，新标签打开）。消费 FR-31 后端已提取并随 `media` 接口返回的 EXIF 字段；真实 EXIF 端到端依赖 FR-31 真机提取。
- **文件详情面板（FR-34）**：新增 `MediaDetailPanel`（全屏 Modal），时间轴点击媒体（图片与视频统一）打开：左侧预览（图片为可滚轮缩放的原图 1–4 倍、换项复位；视频为缩略图 + 「打开播放」按钮跳 `/play/:id`），右侧展示文件元数据（显示名/真实名/类型/大小/分辨率/时长/编码/加入与修改时间）并提供原文件下载入口。支持全屏切换、`←`/`→` 在已加载列表内切换上/下一项（端点夹紧）、`Esc` 关闭。取代时间轴原「图片弹窗 / 视频直跳」的分裂打开方式；`ImagePreviewModal` 仍由目录页使用（待 FR-33 统一）。

## 0.5.0（2026-06-22）

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
