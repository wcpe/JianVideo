# ADR-0035：高级编码 fMP4/CMAF + 原生 MSE 播放路径（扩展播放内核）


## 状态
提议中

## 背景

项目播放内核锁定为 **mpegts.js + MPEG-TS + HLS**（[ADR-0003](0003-hls-ts-streaming.md)、[ADR-0004](0004-mpegts-js-player.md)、[ADR-0010](0010-hls-ts-output.md)），架构红线明确「禁止用原生 video 播 TS 流」「禁止替换播放内核」。该路径承载 H.264 的追播、边下边播、精准 Seek，是经过验收的核心能力。

FR-49（[ADR-0033](0033-hwaccel-probe-source-cache.md)）已把硬件加速能力重构为 per-codec 实测真源，系统具备识别与产出 H.265/AV1/VP9 的基础。但这些高级编码无法沿用现有内核播放：

1. **MPEG-TS 裸流封装高级编码兼容性差**：mpegts.js 面向 H.264/AAC 的 TS 流设计，对 H.265/AV1/VP9 支持薄弱或缺失。
2. **浏览器原生 MSE 不接受 TS**：高级编码要在浏览器播放，主流路径是 fMP4/CMAF（ISO BMFF 分片）+ `MediaSource` + `SourceBuffer`。
3. **既有内核不能动**：H.264 路径已验收且是红线，不能为高级编码改造它。

需要决策：为非 H.264 编码引入怎样的输出与播放路径，且不破坏播放内核红线。

## 决策

**扩展（而非取代）播放内核**：H.264 维持 mpegts.js + MPEG-TS + HLS 路径**原样不动**；非 H.264 编码（H.265 / AV1 / VP9）走**新增的 fMP4/CMAF 分片 + 浏览器原生 MSE** 播放路径。在转码输出处按目标编码分发到两条路径之一。

- 后端为非 H.264 编码用外部 ffmpeg 产出 **HLS-fMP4（CMAF）**：init segment（`init.mp4`）+ media segments（`.m4s`）+ HLS-fMP4 清单（`index.m3u8`，`EXT-X-MAP` + `EXT-X-ENDLIST`，VOD）。
- 容器为 fMP4/CMAF，**不是 MPEG-TS**——因此不触犯红线「禁止用原生 video / MSE 播 TS 流」。
- 与 FR-16「不走 TS 的直出场景允许原生 video」自洽：高级编码走 fMP4 + 原生 MSE，本就属「不走 TS」的场景。
- 本期 fMP4 路径仅支持 **VOD（转码完成后整段播放）**，不实现实时追播；H.264 路径的追播能力不受影响。

## 理由

- **为何扩展而非取代**：mpegts.js + TS 是 H.264 追播/边下边播/Seek 的验收过的内核，取代它会回归核心能力、触碰红线。高级编码是新增需求，用并存的第二条路径承载，对 H.264 零影响。
- **为何选 fMP4/CMAF 而非别的容器**：① 浏览器原生 MSE 对 fMP4 支持成熟（`SourceBuffer` 直接吃 fMP4 分片）；② CMAF 是 H.265/AV1/VP9 在 Web 端的事实标准封装；③ ffmpeg 的 `hls` muxer 配 `-hls_segment_type fmp4` 直接产出标准 HLS-fMP4，复用现有 HLS 静态服务端点（`/api/play/hls/*path`），改动面最小；④ HLS-fMP4 清单天然带 `EXT-X-MAP`（init segment）描述，前端 hls.js（支持 fMP4）或自管 MSE 均可消费。
- **为何不碰 TS 红线**：红线针对「TS 流」；fMP4 容器与 TS 是两种封装，真机 ffprobe 确认产物 `format_name=mov,mp4`，与 TS 无关。
- **为何与现有 HLS 并存而非合并**：两条路径清单格式（TS HLS vs fMP4 HLS）、segment 类型（`.ts` vs `.m4s`+init）、前端驱动（mpegts.js vs 原生 MSE/hls.js-fMP4）不同，强行合并会污染已验收的 H.264 分支。按目标编码分发、各走各路最清晰。
- **回退策略**：目标编码无硬件可用时回软件兜底编码器（`libx265`/`libsvtav1`/`libvpx-vp9`）；端到端「客户端不支持某高级编码时回退 H.264/mpegts.js」属 FR-52/53 的协商逻辑，本 ADR 只保证后端能产出两条路径、分发正确。

## 后果

- **正**：高级编码（H.265/AV1/VP9）可经浏览器原生 MSE 播放；H.264 路径零回归；为 FR-52（前端能力探测 + 自适应播放器）、FR-53（端到端编码协商）提供后端基石与明确前端契约。
- **正**：复用现有 `/api/play/hls/*path` 静态服务，仅扩展 `.m4s`/init `.mp4` 的 MIME 识别，端点改动最小。
- **负 / 边界**：本期 fMP4 路径**不支持实时追播 / 边下边播**，仅 VOD（转码完成后播放）。实时 fMP4 追播需不同的 MSE 分片驱动机制，复杂度高、属前端协同范畴，留待后续（不在本期镀金）。
- **负**：fMP4 不能沿用 TS 路径的 `-c:a copy`，音频统一转 AAC。
- **依赖方向**：分发与 fMP4 管道在 `transcoder` 包内，方向不变（`transcoder → db`），不引入反向依赖。
- 与 [ADR-0003](0003-hls-ts-streaming.md)/[ADR-0004](0004-mpegts-js-player.md) 关系：**不取代**它们，二者对 H.264/TS 仍完全有效；本 ADR 在其之上**新增**一条并存路径。

## 备选方案

- **取代 mpegts.js，全部走 fMP4 + 原生 MSE**：会丢掉 H.264 已验收的追播/边下边播/Seek 内核，触碰红线，回归核心能力。否。
- **用 MPEG-TS 封装高级编码喂 mpegts.js**：mpegts.js 对 H.265/AV1/VP9 支持薄弱，TS 封装高级编码兼容性差，且仍受「TS 内核只认 H.264」局限。否。
- **DASH（MPD + fMP4）替代 HLS-fMP4**：DASH 同样基于 fMP4，但需引入 dash.js、另一套清单格式与端点，且与现有 HLS 静态服务不复用。HLS-fMP4 改动面更小、与现状一致。否。
- **本期就做 fMP4 实时追播**：复杂度高、属前端协同与后续阶段，提前做属镀金（范围纪律）。本期先 VOD 闭环。否。
