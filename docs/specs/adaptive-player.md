# 功能规格：前端客户端能力探测 + 自适应播放器

> 状态：开发中　·　关联 PRD：FR-52　·　分支：feature/fr-52-adaptive-player

## 1. 背景与目标

FR-51（[ADR-0035](../adr/0035-fmp4-mse-playback-path.md)、[spec](fmp4-advanced-codec-output.md)）已交付后端「高级编码 fMP4/CMAF + 原生 MSE」输出路径，并定义了前端契约：非 H.264 编码（H.265/AV1/VP9）经 `GET /api/play/hls/{mediaID}/{index.m3u8|init.mp4|seg_NNN.m4s}` 提供 HLS-fMP4 清单与分片，codec MIME 串由后端 `FMP4CodecMIME` 给出。但前端播放器（`VideoPlayer`）当前只识别 H.264/TS（mpegts.js）与多码率 master.m3u8（hls.js ABR），尚不会消费 fMP4 路径。

本 FR 属第五期（P5），是「高级编码端到端」链条的前端环节：① 实现**客户端能力探测**，用 `MediaSource.isTypeSupported(mime)` 探测本浏览器可解码哪些高级编码；② 扩展播放器为**自适应播放器**，按「播放描述符（目标编码 + 清单 URL + 播放路径）」分发到对应内核——H.264 走现有 mpegts.js+TS（不动），高级编码走 hls.js（fMP4 模式）；③ 客户端不支持目标编码时**降级回 H.264 路径**，不抛 Network Error。

「按客户端能力实际选哪个编码 + 触发后端产 fMP4」的端到端协商与后端接线属 FR-53，不在本 FR。本 FR 的播放器对「给定 fMP4 清单 URL + 编码」能正确用 hls.js 播即可。

## 2. 需求（要什么）

范围内：
- **客户端能力探测（探测函数）**：
  - 纯函数 `codecMIME(codec)`：目标编码（`h265`/`av1`/`vp9`）→ fMP4 容器 MSE codec MIME 串，与后端 `FMP4CodecMIME` 字节级一致（H.265 `video/mp4; codecs="hvc1.1.6.L93.B0"`、AV1 `av01.0.05M.08`、VP9 `vp09.00.10.08`）。
  - `isCodecSupported(codec)`：对给定编码取 MIME 串调 `MediaSource.isTypeSupported`，返回布尔；非高级编码 / 无 MIME / 环境无 `MediaSource` 时返回 `false`。
  - `probeClientCapabilities()`：返回客户端能力描述 `{ h265, av1, vp9 }`（各布尔），供 FR-53 协商上报。本 FR 实现并暴露探测函数，不做上报接线。
- **自适应播放器（按描述符分发）**：
  - 引入「播放描述符」`PlaybackDescriptor { codec, url, path }`——`codec` 目标视频编码、`url` 清单/流 URL、`path` 播放路径（`ts` H.264/TS、`fmp4` 高级编码、`mp4` 原文件直出）。
  - `VideoPlayer` 接收描述符（向后兼容现有 `url`/`isABR`/`streamType` 入参），按 `path` 分发：
    - `ts`（H.264，含现有 master.m3u8 ABR 与单码率 TS）→ 现有 mpegts.js / hls.js-ABR 分支，**实现一字不动、追播不变**。
    - `fmp4`（H.265/AV1/VP9）→ hls.js（原生支持 fMP4+MSE）加载 `index.m3u8` 播放。
    - `mp4`（原文件直出）→ 现有原生 video 分支不动。
  - hls.js 已是项目 ABR 内核（[ADR-0026](../adr/0026-abr-adaptive-bitrate.md)），fMP4 路径复用同一库，不引新依赖。
- **不支持回退**：自适应入口在分发到 fMP4 前先 `isCodecSupported(codec)` 校验；为 `false` 时回退 H.264/TS 路径（不初始化 fMP4，不抛 Network Error）。回退仅需「描述符携带 H.264 回退 URL」或「由调用方按探测结果改走 TS 描述符」——本 FR 在播放器内实现「目标编码不支持 → 走回退 URL」的兜底，回退 URL 由调用方提供（FR-53 接线时给真实回退源）。

不做（范围外）：
- **端到端编码协商**（首选优先级 ∩ 客户端能力 ∩ 硬件可产出、播放发起时选实际编码并触发后端产 fMP4、会话记录实际编码/路径）——属 FR-53。
- 把探测结果**上报后端**与据此请求特定编码的接线——属 FR-53。本 FR 只实现并暴露 `probeClientCapabilities()`。
- **fMP4 路径的实时追播 / 边下边播**——FR-51 已定 fMP4 仅 VOD，本 FR 前端据此按 VOD 加载（hls.js 默认 VOD），不实现 fMP4 实时追随。
- PlayPage 的编码协商改造与真实 fMP4 媒体接线——属 FR-53。本 FR 不改 PlayPage 的 URL 选择逻辑（仍走现有 master 探测 → mpegts/mp4）。

## 3. 设计（怎么做）

本 FR 复用 [ADR-0035](../adr/0035-fmp4-mse-playback-path.md)（fMP4/CMAF + 原生 MSE 播放路径）与 [ADR-0026](../adr/0026-abr-adaptive-bitrate.md)（hls.js 作为 HLS 内核），**无需新 ADR**——ADR-0035 已明确「前端可用 hls.js（支持 fMP4）消费 HLS-fMP4 清单」，本 FR 是其前端落地，不引入新架构决策。

### 3.1 客户端能力探测（`frontend/src/utils/codec-capability.ts`，纯函数）

- `codecMIME(codec: string): string`：归一化编码（小写、`hevc`→`h265`）后查表，返回 fMP4 MSE MIME 串；非高级编码返回空串。表与后端 `fmp4CodecMIME` 同源（单一真源在后端，前端表加注释指明对应后端常量，FR-53 整合时可改为随会话信息下发）。
- `isCodecSupported(codec: string): boolean`：取 `codecMIME`，空串直接 `false`；环境无 `window.MediaSource?.isTypeSupported` 时 `false`；否则返回 `MediaSource.isTypeSupported(mime)`。
- `probeClientCapabilities(): ClientCapabilities`：对 `h265`/`av1`/`vp9` 各调 `isCodecSupported`，返回 `{ h265, av1, vp9 }`。

### 3.2 播放描述符与分发（`VideoPlayer`）

- 新增类型 `PlaybackDescriptor { codec: string; url: string; path: 'ts' | 'fmp4' | 'mp4'; fallbackUrl?: string }`（置于 `frontend/src/types`）。
- `VideoPlayer` 新增可选入参 `descriptor?: PlaybackDescriptor`：
  - 缺省（未传 descriptor）→ 完全保持现有行为（`url`/`isABR`/`streamType` 入参驱动），**现有调用方与测试零改动**。
  - 传 descriptor → 按 `path` 分发：
    - `fmp4`：先 `isCodecSupported(codec)`；支持则用 hls.js 加载 `url`（fMP4 模式）；不支持且有 `fallbackUrl` 则回退按 TS 路径加载 `fallbackUrl`（mpegts.js），无 `fallbackUrl` 则展示「当前浏览器不支持该编码」提示而非抛错。
    - `ts`：等价现有 mpegts.js / hls.js-ABR 分支（按 url 是否 master.m3u8）。
    - `mp4`：等价现有原生 video 分支。
- hls.js fMP4 加载：复用现有 `initHlsPlayer` 机制（hls.js 原生支持 fMP4 分片，`loadSource(index.m3u8)` 即可），不需为 fMP4 写独立 MSE 驱动。

### 3.3 不触碰清单（红线保护）

以下本 FR 一字不改：mpegts.js 初始化与追播逻辑（`initMpegtsPlayer`、FR-18 末端缓冲等待）、现有 ABR `initHlsPlayer` 对 master.m3u8 的处理、PlayPage 的 URL 选择与续播/字幕逻辑、H.264/TS 播放行为。新增分发是「未传 descriptor 即旧行为」的增量包裹。

## 4. 任务拆分
- [ ] 复制模板写本 spec；PRD FR-52 状态「计划」→「开发中」
- [ ] 测试先行：`codec-capability` 单测（MIME 串与后端一致、`isTypeSupported` 归类、不支持 / 无 MediaSource 返回 false、`probeClientCapabilities` 三编码归类）；自适应分发单测（fmp4 描述符走 hls.js、ts 描述符走 mpegts.js、不支持回退 fallbackUrl 走 mpegts.js）
- [ ] 实现 `codec-capability.ts` + `PlaybackDescriptor` 类型 + `VideoPlayer` 描述符分发
- [ ] 验证：`npx tsc --noEmit`、eslint 改动文件、`npx vitest run` 全量绿（含现有 VideoPlayer 回归）
- [ ] 文档同步：PRD 状态、ARCHITECTURE 播放层 / §5.5、CHANGELOG

## 5. 验收标准
- 单测（vitest）：
  - 能力探测：`codecMIME` 对 h265/av1/vp9 返回与后端 `FMP4CodecMIME` 字节级一致的 MIME 串、非高级编码返回空；`isCodecSupported` 在 `isTypeSupported` 返回 true/false 时正确归类、无 `MediaSource` 时返回 false；`probeClientCapabilities` 三编码各按探测结果归类。
  - 自适应分发：`fmp4` 描述符（且 `isTypeSupported` true）走 **hls.js**；`ts` 描述符走 **mpegts.js**；`fmp4` 描述符但 `isTypeSupported` false 且有 `fallbackUrl` 时回退走 **mpegts.js**（不抛错）。
  - **现有 VideoPlayer（mpegts.js / 续播 / 末端缓冲）测试保持绿（回归）**。
- `npx tsc --noEmit` 通过、eslint 改动文件无错、`npx vitest run` 全量绿。
- 真机（标注）：完整「真实媒体 → fMP4 → 浏览器播」端到端属 FR-53 接线后；本 FR 标「待 FR-53 接线后整体真机」。若能用 fixture/静态 fMP4 清单在浏览器加载播放，可附验（非阻塞验收项）。

## 6. 风险 / 待定
- **回退源由谁提供**：本 FR 的播放器只实现「目标编码不支持 → 走 `fallbackUrl`（若有）」的兜底，真实 H.264 回退源的产出与选择属 FR-53 协商。本 FR 测试用 mock URL 验证分发与回退走向，不依赖真实回退源。
- **能力 MIME 表的单一真源**：MIME 串真源在后端 `fmp4CodecMIME`。本 FR 前端表为消费副本（加注释指向后端常量）。FR-53 整合时可改为「随清单/会话信息由后端下发 MIME 串」，前端不再硬编码——届时本表可删，属 FR-53 范畴，本 FR 不提前做。
- **fMP4 追播**：FR-51 已定 fMP4 仅 VOD，hls.js 默认按 VOD（清单含 `EXT-X-ENDLIST`）加载，前端无需额外处理；本 FR 不实现 fMP4 实时追随。
- **整体真机依赖 FR-53**：本 FR 单测全绿不替代「真实媒体端到端可播」，后者需 FR-53 接线（编码协商 + PlayPage 改造）后整体真机，本 FR 显式标注此差距。
