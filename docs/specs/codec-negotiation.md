# 功能规格：端到端编码协商（FR-53）

> 状态：开发中　·　关联 PRD：FR-53　·　分支：feature/fr-53-codec-negotiation

## 1. 背景与目标

前四个 FR 各就位但尚未串通：FR-49 给出「系统实测可产出编码并集」，FR-50 给出「首选目标编码优先级」设置，FR-51 给出「按目标编码产 fMP4/CMAF」的输出路径，FR-52 给出「客户端能力探测 + 自适应播放器」。本 FR 把它们端到端联起来：**播放发起时按「首选优先级 ∩ 客户端能力 ∩ 硬件可产出」协商出实际输出编码与播放路径，含降级兜底，并在会话上记录实际编码与路径。** 属 P5（高级编码端到端可播）的收口 FR。

## 2. 需求（要什么）

- **协商纯函数**：`ChosenCodec(priority, clientCaps, producible) string` = 首选优先级里第一个同时满足「客户端支持」且「实测可产出」的编码；都不满足则兜底 `h264`。可穷举单测。
- **协商端点**：`POST /api/play/:id/negotiate`，请求体带客户端能力 `{h265,av1,vp9}`；后端读 FR-50 优先级、FR-49 可产出并集，协商出实际编码，返回**播放描述符**（编码 + 路径类型 ts/fmp4 + 清单 URL + MIME + H.264 回退 URL）。
- **后端按协商实际产出**：`h264` → 现有 mpegts.js + TS 路径（master.m3u8 / stream，分支不动）；非 `h264` → 调 `PreSliceWithCodec`（FR-51）产 fMP4，返回 `/api/play/hls/{id}/index.m3u8`。
- **前端接线**：PlayPage 探测能力（FR-52）→ 请求协商 → 拿描述符 → 交自适应播放器（FR-52）播；协商不出高级编码 → H.264 回退（不报错）。
- **会话记录实际编码与路径**：在播放会话上记录本次协商的实际编码与播放路径。
- 范围内：协商算法、协商端点、fMP4 产出接线、PlayPage 协商接线、会话记录。
- 不做（范围外）：实时 fMP4 追播（FR-51 已定仅 VOD）；硬件 AV1/QSV/NVENC 端到端真机（本机无对应硬件，标「待硬件」）；ABR fMP4 多码率（P2）；前端 MIME 表改由后端下发（可选优化，不在本期）。

## 3. 设计（怎么做）

### 3.1 协商核心（纯函数，`internal/transcoder/negotiation.go`）

- `ChosenCodec(priority []string, clientCaps map[string]bool, producible map[string]bool) string`：遍历 `priority`，返回首个 `clientCaps[c] && producible[c]` 的编码；无命中返回 `DefaultTargetCodec`（`h264`）。归一化大小写、`hevc`→`h265`（复用 `normalizeCodec`）。`h264` 视为「客户端恒支持、系统恒可产出」的兜底底座（不强制出现在 producible/clientCaps 里也能兜底）。
- `BuildNegotiationDescriptor(mediaID, codec)`：把协商结果映射为描述符——`h264` → `{path:ts, url:/api/play/hls/{id}/master, codec:h264}`；高级编码 → `{path:fmp4, url:/api/play/hls/{id}/index.m3u8, codec, mime:FMP4CodecMIME(codec), fallbackUrl:/api/play/{id}/stream}`。纯函数，URL 为相对路径。

### 3.2 协商端点（`internal/api`，`Handler.Negotiate`）

- `POST /api/play/:id/negotiate`，请求体 `{"client_caps":{"h265":bool,"av1":bool,"vp9":bool}}`。
- 读 `settings.TranscodeCodecPriority()`（FR-50）+ `capability.Capabilities().Codecs`（FR-49 可产出并集）+ 请求体客户端能力 → `ChosenCodec`。
- 非 `h264`：同步调 `transcoder.PreSliceWithCodec(...,codec,...)`（FR-51）产 fMP4。产出失败 → 记 WARN、**降级回 `h264`/TS 描述符**（不报错，保证可播）。
- 返回 `BuildNegotiationDescriptor` 的描述符（绝对化 URL 由前端处理，端点返回相对路径）。
- 经 `playback.Service.RecordNegotiation(mediaID, codec, path)` 记录会话实际编码与路径。
- 未注入 settings / capability / hlsDir / hlsMgr 时回退 `h264`/TS 描述符（无服务环境可用）。

### 3.3 会话记录（`internal/playback`）

- 在内存 `models.PlaybackSession` 增 `TargetCodec`、`OutputPath` 两字段（gorm 标签随表，但当前会话仅在内存，不新增持久化表——与现状一致）。
- `Service.RecordNegotiation(mediaID, codec, path)`：取/建会话并写入两字段。
- 说明：ARCHITECTURE 文档历史上描述过 `transcode_sessions` 表（含 `hw_accel`），但代码从未落地该表；本 FR 不新建该表，按现状把协商结果记在内存播放会话上，避免镀金。

### 3.4 前端接线（`frontend/src/pages/PlayPage.tsx` + `api/play.ts`）

- 新增 `api/play.ts` 的 `negotiate(mediaID, caps)` 调用。
- PlayPage：加载媒体后 `probeClientCapabilities()` → `negotiate` → 拿描述符 → 传给 `VideoPlayer` 的 `descriptor` 入参（FR-52）。
- 协商失败 / 描述符为 ts → 沿用现有 master 探测 → mpegts/mp4 行为（H.264 回退，不报错）。

### 3.5 架构决策

- 协商端点契约 + 协商算法归位属新架构决策 → 写新 ADR（号由整合分配，占位 `docs/adr/0036-codec-negotiation.md`）。复用 ADR-0034（可配置目标编码）、ADR-0035（fMP4/MSE 播放路径）、ADR-0033（实测真源）、ADR-0026（hls.js）。

## 4. 任务拆分

- [ ] 协商纯函数 `ChosenCodec` + `BuildNegotiationDescriptor`（测试先行，穷举）
- [ ] `POST /api/play/:id/negotiate` 端点 + fMP4 产出接线 + 降级兜底
- [ ] `playback.Service.RecordNegotiation` + 会话两字段
- [ ] 前端 `negotiate` API + PlayPage 探测→协商→播放接线
- [ ] 真机 E2E：软件 AV1 fMP4 浏览器播放 + H.264 回退
- [ ] 文档同步：PRD 状态、ARCHITECTURE 协商机制段、API.md 新端点、CHANGELOG、新 ADR

## 5. 验收标准

- 后端单测：`ChosenCodec` 穷举（首选命中 / 客户端不支持跳过 / 不可产出跳过 / 全不满足兜底 h264 / 空优先级兜底 h264）全绿；`BuildNegotiationDescriptor` ts/fmp4 分支正确；negotiate 端点响应正确（h264 返 ts、高级编码返 fmp4 且 URL/MIME 正确、产出失败降级 h264）；会话记录实际编码与路径。
- 前端单测：PlayPage「探测→协商→播放」流程（协商返 fmp4 → 传描述符给播放器；协商返 ts / 协商失败 → H.264 回退）。
- **真机 E2E（需用户确认通过）**：设首选含 av1 + 支持 AV1 的浏览器（Chrome）→ 播放真实视频 → 后端用 `libsvtav1` 产 fMP4 → 浏览器 MSE 播 AV1 成功；并验 H.264 客户端走 mpegts.js。硬件 AV1/QSV/NVENC 维度本机无对应硬件，标「待硬件」。
- `go build ./internal/...`、受影响包 `go test`、`go vet` 全绿；前端 `npx tsc --noEmit` + `npx vitest run` 全绿。

## 6. 风险 / 待定

- **fMP4 同步产出耗时**：negotiate 端点同步跑 ffmpeg 产 fMP4，长视频可能耗时较长。本期沿用 FR-51 的 10 分钟硬上限；产出失败降级 h264 兜底。更细的异步产出 + 轮询属后续优化，不在本期。
- **会话不持久化**：协商结果记在内存会话，进程重启丢失。与现状 `PlaybackSession` 一致，不新建表（避免镀金）。
- **硬件维度待硬件**：本机仅能软件 AV1 真机验，硬件家族端到端标「待硬件」。
