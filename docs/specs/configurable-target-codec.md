# 功能规格：转码目标编码可配置（FR-50）

> 状态：开发中　·　关联 PRD：FR-50　·　分支：feature/fr-50-target-codec

## 1. 背景与目标

当前转码输出固定为 H.264/MPEG-TS（FR-06），单/多码率管道把 H.264 编码器与 `-pix_fmt yuv420p`
硬编在 `buildArgs`/`buildMultiArgs` 里。FR-49 已让系统具备「实测可输出哪些编码」的 per-codec 能力数据
（`HWAccelInfo.Codecs` 并集 + `SelectEncoderForCodec(results, codec)` 按编码选硬件优先编码器）。

本功能（FR-50，第五期 P5）让「目标输出编码」可配置：服务端持久化「首选目标编码优先级」设置，
转码管道按所选编码参数化输出（编码器名、像素格式、关键编码参数）。这是为 FR-51/52/53
（高级编码 fMP4 输出、客户端能力探测、端到端协商）铺底的一层。

## 2. 需求（要什么）

范围内：
- **持久化首选目标编码优先级**：复用既有 `settings` 表/服务，新增键 `transcode_codec_priority`，
  值为 JSON 数组（如 `["h264"]` 或 `["av1","h265","h264"]`）。
- **设置校验**：只接受 FR-49 能力 `codecs`（系统实测可输出）并集内的编码；含非法/不支持编码即整体拒绝。
- **管道按目标编码参数化**：把 `buildArgs`/`buildMultiArgs` 中硬编的 H.264 抽成「按目标编码取参数」——
  编码器名来自 `SelectEncoderForCodec`，像素格式与关键编码参数来自「编码 → 输出参数」纯函数映射。
  支持 h264/h265/av1/vp9。
- **默认 H.264、运行时默认行为不变**：未配置 / 配置为空 / 解析失败一律回落 `["h264"]`；
  `NewPipeline()` 等既有入口默认产出与今天**字节级一致**的 H.264/TS 参数，mpegts.js 仍可播。

不做（范围外，属 FR-51/52/53）：
- fMP4 / CMAF 分片输出与播放器改造（输出容器仍由播放路径决定，本 FR 不改容器）。
- 按客户端能力实际选码、切播放路径、端到端协商。
- 追播在新编码下的行为验证。
- 新增 HTTP 端点 / 前端 UI（本 FR 只交付配置存储 + 管道可参数化的能力；
  设置经既有 `PUT /api/settings` 通道写入即可，专用 UI 留后续）。

## 3. 设计（怎么做）

涉及模块：`internal/transcoder`（核心）、`internal/settings`（设置读写与校验）。

- **编码 → 输出参数纯函数**（`internal/transcoder/codec_params.go`，新增）：
  `CodecOutputParams(codec string) (CodecParams, bool)` 返回该编码的像素格式与关键编码参数（无副作用，可穷举单测）。
  - h264：`-pix_fmt yuv420p`（与现状一致，不加额外参数，保证默认行为不变）。
  - h265：`-pix_fmt yuv420p` + `-tag:v hvc1`（便于后续容器封装，纯软件 TS 输出不依赖它，但写明编码语义）。
  - av1：`-pix_fmt yuv420p`。
  - vp9：`-pix_fmt yuv420p`。
  - 关键参数保守取值，硬件/软件编码器通用；编码器特有调优不在本 FR 引入（YAGNI）。
- **编码器选取**复用 FR-49 `SelectEncoderForCodec(results, codec)`：按 codec 选硬件优先编码器，无则软件兜底。
- **Pipeline 参数化**：`Pipeline` 增 `codec` 字段；新增 `NewPipelineForCodec(codec)` 依次
  `SelectEncoderForCodec`（冷态软件兜底）+ `CodecOutputParams` 装配；`NewPipeline()` 等价于
  `NewPipelineForCodec("h264")`，保持既有签名与默认行为。`buildArgs`/`buildMultiArgs` 用 `p.codec`
  对应的 pix_fmt / 关键参数替换硬编 `-pix_fmt yuv420p`；codec 为空按 h264 处理（兼容直接构造 `Pipeline{}` 的测试）。
- **settings 目标编码优先级**（`internal/settings/service.go`）：
  - 新增键常量 `KeyTranscodeCodecPriority = "transcode_codec_priority"`。
  - `TranscodeCodecPriority() []string`：读键、JSON 解析、空/非法回落 `["h264"]`。
  - `SetTranscodeCodecPriority(priority []string, allowed []string) error`：校验 priority 非空、
    每项在 `allowed`（FR-49 codecs 并集）内、无未知编码，通过则 JSON 序列化写入；否则返回业务错误。
    校验为纯逻辑，`allowed` 由调用方从 `CapabilityService` 取，settings 不反向依赖 transcoder。
- 输出容器策略不变：playback 仍 `-f mpegts`、ABR 仍 `-f hls`（FR-06 播放路径不动）。

涉及架构决策（转码输出从「固定 H.264」变为「可配置」）→ 新写 ADR（占位 `docs/adr/0034-configurable-target-codec.md`），
说明与 FR-06「固定 TS/H.264」的关系（扩展而非推翻播放，输出容器仍由播放路径决定）。

## 4. 任务拆分

- [ ] 编码 → 输出参数纯函数 + 穷举单测（h264/h265/av1/vp9 + 未知编码）
- [ ] settings 目标编码优先级读写 + 校验 + round-trip / 非法拒绝 / 默认单测
- [ ] Pipeline 增 codec 字段 + `NewPipelineForCodec` + buildArgs/buildMultiArgs 参数化 + 单测
- [ ] 真机：首选 av1 → 选 libsvtav1，真实 ffmpeg 转一小段，ffprobe 断言输出视频编码=av1
- [ ] ADR-0034-configurable-target-codec.md
- [ ] 文档同步：PRD 状态、ARCHITECTURE §5.3、CHANGELOG 未发布段

## 5. 验收标准

- 单测：给定目标编码（h264/h265/av1/vp9），管道构建出正确 ffmpeg 参数（编码器名 + pix_fmt + 关键参数）；
  设置读写 round-trip；非法 / 不支持编码被拒；默认（未配置）= H.264。
- 默认行为不变：`NewPipeline()` 产出参数与改动前一致（既有 `pipeline_buildargs_test` / `multi_pipeline_args_test` 全绿）。
- **真机（本机 AMD RX 580，软件 AV1 可验，需用户/实测确认）**：设首选为 av1 → 选码走 libsvtav1
  （`SelectEncoderForCodec` 选出的 av1 编码器），真实 ffmpeg 转一小段并用 ffprobe 断言输出视频编码=av1。
  硬件 AV1 本机无法验，标注「待硬件」。单元测试不替代此项。
- `go build ./internal/...` 通过、受影响包 `go test ./internal/...`（transcoder/settings/api）全绿、
  `go vet ./internal/transcoder/ ./internal/settings/` 干净。

## 6. 风险 / 待定

- 本机为 AMD RX 580，硬件 AV1（`av1_amf`）无法真机验证，仅软件 AV1（libsvtav1）可验，硬件路径标「待硬件」。
- 输出容器与播放路径不在本 FR 改动；非 H.264 编码经 TS 裸流当前浏览器多不可播——这正是 FR-51/52 的工作，
  本 FR 仅交付「配置 + 管道可参数化」，不保证非 H.264 端到端可播放。
