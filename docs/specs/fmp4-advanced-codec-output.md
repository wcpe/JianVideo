# 功能规格：高级编码 fMP4/CMAF 分片输出与 MSE 播放路径

> 状态：开发中　·　关联 PRD：FR-51　·　分支：feature/fr-51-fmp4-output

## 1. 背景与目标

当前转码输出固定为 H.264 + MPEG-TS + HLS，前端经 mpegts.js 操作 MSE 播放（FR-06/FR-16）。该路径承载追播、边下边播、精准 Seek 等关键能力，是项目锁定的播放内核（[ADR-0003](../adr/0003-hls-ts-streaming.md)、[ADR-0004](../adr/0004-mpegts-js-player.md)、架构红线）。

随着 FR-49 补齐 per-codec 实测能力，系统已能识别并产出 H.265/AV1/VP9 等高级编码。但 mpegts.js + MPEG-TS 不适合承载这些编码（裸 TS 流封装高级编码兼容性差、mpegts.js 设计面向 H.264）。需要为非 H.264 编码引入一条独立的输出与播放路径。

本 FR 属第五期（P5），是「高级编码端到端」链条的后端基石：为非 H.264 编码产出浏览器原生 MSE 可加载的 **fMP4/CMAF 分片 + 清单**，并在转码输出处按目标编码分发到对应路径。前端消费（客户端能力探测 + 自适应播放器）属 FR-52，本 FR 只定义并交付**前端契约**，不动前端播放器。

## 2. 需求（要什么）

范围内：
- 新增 fMP4/CMAF 分片输出管道：对目标编码为 h265/av1/vp9 的媒体，用外部 ffmpeg 产出 **HLS-fMP4（CMAF）** 产物——init segment（`init.mp4`）+ media segments（`.m4s`）+ HLS-fMP4 清单（`index.m3u8`，含 `EXT-X-MAP` 与 `EXT-X-ENDLIST`）。
- 目标编码 → 编码器 + 参数选择：复用 FR-49 的 `SelectEncoderForCodec` 选实测可用编码器；本 FR 自带最小的「目标编码 → 编码器 + ffmpeg 参数」映射（与并行开发的 FR-50 在整合时对齐，见 §6 重叠点）。
- 播放/转码输出**分发点**：在产出转码输出处按目标编码分发——`h264` 走现有 TS/HLS 路径（**分支实现一字不动**），其余编码走新 fMP4 路径。分发对 h264 必须等价于现状（无回归）。
- HTTP 端点：提供 fMP4 清单、init segment、media segment 的读取端点（沿用现有 `/api/play/hls/*path` 静态服务机制，新增 `.m4s`/`.mp4` 的 MIME 识别）。
- **前端契约**（FR-52 直接消费，见 §3.4）：容器、codec MIME 串、URL 方案、清单格式、追播支持边界，写清楚并落进 ADR/本 spec。

不做（范围外）：
- **fMP4 路径的实时追播 / 边下边播**：本期 fMP4 路径仅支持「转码完成后整段播放」（VOD，`EXT-X-PLAYLIST-TYPE:VOD` + `EXT-X-ENDLIST`），**不**实现 H.264/TS 路径那样的实时追随。理由见 §6。H.264 路径的追播能力不受影响。
- 前端播放器改造（客户端能力探测、自适应入口、MSE fMP4 加载逻辑）——属 FR-52。
- 端到端编码协商（首选优先级 ∩ 客户端能力 ∩ 硬件可产出）——属 FR-53。
- 目标编码优先级的持久化设置——属 FR-50（本 FR 仅接收一个目标编码入参，不读写设置）。
- 硬件 AV1 / Intel QSV / NVIDIA NVENC 的真机验证（验收机为 AMD RX 580，无对应硬件，标「待硬件」）。

## 3. 设计（怎么做）

涉及架构决策（为非 H.264 扩展一条 fMP4/CMAF + MSE 播放路径，与既有 mpegts.js/TS 并存而非取代），另写 ADR：[ADR-0035](../adr/0035-fmp4-mse-playback-path.md)。决策正文见该 ADR，本节只描述实现机制。

### 3.1 目标编码 → 编码器 + 参数（transcoder 包，纯函数）

- `TargetCodec`：目标视频编码标识（`h264`/`h265`/`av1`/`vp9`）。
- 纯函数 `SelectFMP4Encoder(results, codec)`：复用 `SelectEncoderForCodec`，在 per-codec 实测快照中取该编码硬件优先级最高的可用编码器；无硬件可用时回软件兜底（`libx265`/`libsvtav1`/`libvpx-vp9`）。
- 纯函数 `BuildFMP4Args(inputPath, encoder, outputDir)`：构建 fMP4/CMAF 的 ffmpeg 命令行参数。关键参数：
  - `-c:v <encoder>`、`-pix_fmt yuv420p`（统一 8-bit，避免 10-bit 解码兼容问题，与现有 TS 路径同策略）、固定 GOP（`-g 48 -keyint_min 48 -sc_threshold 0`）。
  - HEVC 加 `-tag:v hvc1`（保证 fMP4 内 tag 为 `hvc1` 而非 `hev1`，Safari/MSE 兼容性更好）。
  - 封装：`-f hls -hls_segment_type fmp4 -hls_time 4 -hls_playlist_type vod -hls_fmp4_init_filename init.mp4 -hls_segment_filename <seg>_%03d.m4s <index>.m3u8`。
  - 音频 `-c:a aac`（fMP4 不能封装裸 TS 的 `copy` 任意编码，统一转 AAC）。

### 3.2 fMP4 输出执行（transcoder 包，副作用隔离）

- `RunFMP4ToDir(ctx, inputPath, codec, outputDir)`：选编码器 → `BuildFMP4Args` → 跑外部 ffmpeg（复用现有 `ffmpegPath`、进程组、`WaitDelay`、context 取消语义），输出 init/m4s/m3u8 到 `outputDir`。
- 产出校验：确认 `init.mp4` 与 `index.m3u8` 真实生成，否则清理并报错（与现有 `verifySliceOutputs` 同风格）。

### 3.3 分发点（transcoder 包）

- `PreSlice` 增加目标编码维度：保持现有签名兼容（默认 h264 走原 `MultiPipeline` TS 路径），新增按目标编码分发的入口 `PreSliceWithCodec(...)`：
  - `codec == h264`（或空，默认）→ 现有多码率 TS/HLS 路径，**完全不动**。
  - `codec ∈ {h265, av1, vp9}` → `RunFMP4ToDir` fMP4 路径，产出 CMAF 清单。
- 分发是纯条件分支，h264 分支调用既有代码原样，保证零回归。

### 3.4 前端契约（FR-52 消费的关键产出）

- **容器**：fMP4 / CMAF（ISO BMFF 分片 MP4）。init segment 含 `moov`（无媒体数据），media segment 为 `.m4s`（`moof`+`mdat`）。
- **codec MIME 串**（前端 `MediaSource.isTypeSupported` 用）：
  - H.265：`video/mp4; codecs="hvc1.1.6.L93.B0"`（音频并轨时附 `,mp4a.40.2`）
  - AV1：`video/mp4; codecs="av01.0.05M.08"`
  - VP9：`video/mp4; codecs="vp09.00.10.08"`
  - 由后端纯函数 `FMP4CodecMIME(codec)` 给出，随清单/会话信息暴露给前端，前端据此 `addSourceBuffer`。
- **URL 方案**（沿用现有 HLS 静态服务 `/api/play/hls/*path`）：
  - 清单：`GET /api/play/hls/{mediaID}/index.m3u8`
  - init segment：`GET /api/play/hls/{mediaID}/init.mp4`
  - media segment：`GET /api/play/hls/{mediaID}/{name}.m4s`
  - MIME：`.m4s` → `video/iso.segment`，`.mp4`(init) → `video/mp4`，`.m3u8` → `application/vnd.apple.mpegurl`。
- **清单格式**：标准 HLS-fMP4（`EXT-X-VERSION:7` + `EXT-X-MAP:URI="init.mp4"` + `EXTINF`/`.m4s` + `EXT-X-PLAYLIST-TYPE:VOD` + `EXT-X-ENDLIST`）。前端可用 hls.js（支持 fMP4）或自管 MSE 加载。
- **追播支持**：本期 fMP4 路径为 **VOD（不支持实时追播）**。前端发起前应判定：实时转码追播仅 H.264 路径具备；高级编码走「转码完成 → VOD 播放」。

### 3.5 不触碰清单（红线保护）

以下文件/行为本 FR 一字不改：`Pipeline.buildArgs`（单码率 TS）、`MultiPipeline.buildMultiArgs`（多码率 TS/HLS）、`HLSManager` 既有方法、前端 `VideoPlayer.tsx` 与 mpegts.js/hls.js 逻辑、H.264 的 TS 切片命名与 master.m3u8 结构。

## 4. 任务拆分
- [ ] 复制模板写本 spec；PRD FR-51 状态「计划」→「开发中」；占位 ADR
- [ ] 测试先行：`SelectFMP4Encoder`/`BuildFMP4Args`/`FMP4CodecMIME` 纯函数单测（h265/av1/vp9 参数正确、含 `-movflags`/`hls_segment_type fmp4`/codec/pix_fmt/tag）；dispatch 单测（h264 走原 TS 参数不变、其余走 fMP4）；MIME 串映射单测；fMP4 MIME 端点识别单测
- [ ] 实现 fMP4 管道（`BuildFMP4Args`/`RunFMP4ToDir`）+ 分发（`PreSliceWithCodec`）+ 端点 MIME 识别
- [ ] 真机：libsvtav1/libx265 实转 av1/h265 fMP4，ffprobe 断言容器=mov/mp4 且编码=av1/hevc；H.264 回归全绿
- [ ] 文档同步：PRD 状态、ARCHITECTURE §5.3/5.4、ADR、CHANGELOG

## 5. 验收标准
- 单测：fMP4 参数纯函数对 h265/av1/vp9 构建正确 ffmpeg 参数（`-f hls -hls_segment_type fmp4`、`-hls_fmp4_init_filename`、`-c:v` 对应编码器、`-pix_fmt yuv420p`、HEVC 含 `-tag:v hvc1`）；清单/MIME 串正确；**dispatch 断言 h264 仍走原 TS 路径、原路径参数逐字不变**。
- 真机（软件维度，AMD RX 580 可验）：用 `libsvtav1` / `libx265` 实际转出 av1 / h265 的 fMP4 分片，`ffprobe` 断言容器=`mov,mp4` 且视频编码=`av1`/`hevc`；codec MIME 串与 `av01`/`hvc1` tag 对应。**需用户确认通过**（实转 + ffprobe 证据）。
- 硬件 AV1 / QSV / NVENC 维度：验收机无对应硬件，标「待硬件」。
- H.264 回归：transcoder/player/api 相关包测试全绿，TS/mpegts 行为不变。
- `go build ./...` 通过、受影响包 `go test ./internal/...` 全绿、`go vet` 干净。

## 6. 风险 / 待定
- **追播是否纳入本期**：明确**不纳入**。理由：① H.264/TS 追播依赖 mpegts.js 对裸 TS 的实时追加与 event 播放列表，是锁定内核能力；fMP4 路径的实时追播需要不同的 MSE/分片驱动机制，复杂度高、属前端（FR-52/53）协同范畴；② 第五期当前目标是「让高级编码能在浏览器播出来」，先 VOD 跑通是最小可用闭环；③ 提前实现实时 fMP4 追播属镀金（范围纪律）。本期实现「转码完成 → VOD 播放」，spec/ADR 显式标注，不假装支持。
- **与 FR-50 的重叠点**：本 FR 自带最小「目标编码 → 编码器 + 参数」选择（只接收一个 `codec` 入参），FR-50 负责「目标编码优先级」的持久化设置与读取。整合时：FR-50 的设置层产出「目标编码」，喂给本 FR 的 `PreSliceWithCodec`/`RunFMP4ToDir`。两者无逻辑冲突，整合时把「目标编码来源」从入参对接到 FR-50 设置即可。
- **音频处理**：fMP4 不沿用 TS 路径的 `-c:a copy`（裸流任意编码），统一转 AAC，避免源音频编码不被 MP4 容器接受。
- **清理策略**：fMP4 产物与现有 HLS 产物同目录策略（转码结束保留，暂不清理），沿用 ADR-0010 后果，本 FR 不额外处理。
