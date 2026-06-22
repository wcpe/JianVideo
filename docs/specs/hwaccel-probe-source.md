# 功能规格：硬件加速检测统一为实测真源 + 持久化缓存 + 全编码探测

> 状态：开发中　·　关联 PRD：FR-49　·　分支：feature/hwaccel-probe-source

## 1. 背景与目标
属第五期（高级编码与自适应播放，P5）。解决三个真机暴露的问题：①AMD `h264_amf`/`hevc_amf` 在 `codec-test` 实测成功却不显示在硬件加速列表（两套独立检测真源不一致）；②`codec-test` 无缓存，每次重跑最长约 3 分钟；③能力模型只认 H.264/H.265，无法表达 AV1/VP9 等。架构决策见 [ADR-0033](../adr/0033-hwaccel-probe-source-cache.md)（取代 ADR-0015）。本 FR 是 FR-50（可配置目标编码）的数据源前提。

## 2. 需求（要什么）
- 以编码器实测（`ProbeEncoders`）为硬件加速能力**唯一真源**，硬件加速列表与转码选码均派生自它。
- 实测结果按 **ffmpeg 版本为键持久化 SQLite**；版本不变复用缓存、版本变化失效重测；提供**手动「重新测试」**强制重跑。
- 能力模型重构为 **per-codec**：逐家族 × 逐编码记录「编入 / 试编码成功」；家族可用 = 至少一编码试编码成功。
- 补齐探测候选：AMD AMF（含 `av1_amf`）、VAAPI、VideoToolbox、Vulkan，以及 NVENC/QSV 的 AV1、软件 AV1/VP9。
- 前端硬件加速卡片如实展示「全家族 × 全编码」实测结果，并标示结果来自缓存 / 提供「重新测试」。
- **范围内**：检测统一 + SQLite 缓存 + per-codec 模型 + 候选扩展 + 前端展示与重测按钮 + 后台预热。
- **不做（范围外）**：转码目标编码可配置（FR-50）、fMP4 输出与播放器改造（FR-51/52）、端到端协商（FR-53）；本 FR 转码输出仍固定 H.264（`Preferred` 仍取可用的 H.264 编码器）。

## 3. 设计（怎么做）
架构决策见 ADR-0033，此处只列落地改动，不重复决策正文。

**数据模型（db）**：新增 GORM 模型 `internal/db/models/codec_probe.go` → `CodecProbeCache{ FFmpegVersion(主键), Results(JSON []EncoderProbeResult), TestedAt }`；加入 `main.go` AutoMigrate。

**transcoder 核心（纯函数，无副作用，可穷举单测）**：
- `candidateEncoders()` 扩展全家族 + AV1/VP9（`encoder_probe.go`）。
- `buildProbeArgs()` 为 VAAPI / Vulkan 加设备初始化（其余家族沿用通用模板）；已知极慢的软件 AV1 编码加快速预设防 20s 超时误判。
- `BuildCapabilities(results []EncoderProbeResult) *HWAccelInfo`（新，`fallback.go` 重写）：probe 结果 → per-codec 能力映射。
- `SelectBestEncoder(results, codec)`：从结果选第一个可用编码器，无则软件兜底。

**能力服务（副作用隔离层）**：`internal/transcoder` 新增 `CapabilityService{ db *gorm.DB }`：
- `CodecResults(ctx, force bool) ([]EncoderProbeResult, fromCache bool, ...)`：读 SQLite（按当前 ffmpeg 版本）；命中即返回；未命中或 `force` → `ProbeEncoders` + 持久化。单航道（mutex/in-flight）防并发重复实测。
- `Capabilities(ctx) *HWAccelInfo`：`BuildCapabilities(缓存结果)`；冷缓存返回「未测」（不阻塞）。
- `WarmCacheAsync()`：启动时后台 goroutine 预热。

**契约模型重构（破坏性，唯一消费方为内嵌前端）**：`HWAccelInfo` / `HWAccelCapability` 改为 per-codec（`codecs[]`），新增 `from_cache`/`ffmpeg_version`/`tested_at`。同步 `docs/API.md`。

**API 装配**：`/api/transcode/hwaccel` 由 `transcoder.HWAccelHandler`（net/http 自由函数）改为 `*Handler` 方法读能力服务；`/api/system/info` 的 hwaccel 同源；`/api/system/codec-test`（POST）默认读缓存、`?force=true` 强制重测，响应附 `from_cache`/`tested_at`。`Handler` 增 `WithCapabilityService(...)` 注入；`main.go` 装配并 `WarmCacheAsync()`。

**前端**：`types` 改 hwaccel 为 per-codec；`SystemPage` 硬件加速卡片按家族×编码展示、`buildReport` 同步；codec-test 区显示「来自缓存 / 实测时间」+「重新测试」按钮（force）；`api/system.ts` real+mock 同步。

## 4. 任务拆分
- [ ] 测试先行（红）：`encoder_probe_test`（候选含 amf/av1/vp9 全家族、VAAPI/Vulkan 参数）、`fallback_test`（BuildCapabilities/SelectBestEncoder 穷举：空/仅某编码/多家族/全不可用）、缓存服务测试（命中复用、版本变化失效、force 重测、并发单航道）、`system_handler`/`hwaccel` 端点测试（含 from_cache）。
- [ ] db 模型 + AutoMigrate。
- [ ] transcoder 核心纯函数（候选扩展、probe 参数、BuildCapabilities、SelectBestEncoder）。
- [ ] CapabilityService（缓存读写 + 冷缓存不阻塞 + 后台预热 + 单航道）。
- [ ] API 装配（hwaccel 改方法、codec-test force、注入、main.go 预热）。
- [ ] 前端（types/SystemPage/system.ts + 测试）。
- [ ] 文档同步：PRD 状态、ARCHITECTURE §5.6/§5.7、API、CHANGELOG。

## 5. 验收标准
- **AC（真机，需用户确认）**：本 AMD 机运行打包产物，`/system` 硬件加速卡片**显示 `h264_amf`/`hevc_amf` 可用**（与 codec-test 一致）；列出系统支持的全部编码（含 AV1 若硬件/软件支持）。
- 二次打开 `/system` 或再次触发 codec-test **读缓存即时返回**（不再 3 分钟重跑）；点「重新测试」强制重跑；ffmpeg 版本变更后自动失效重测（可单测模拟）。
- `BuildCapabilities`/`SelectBestEncoder` 纯函数穷举单测全绿（空结果、仅 H.264、含 AV1、多家族、全不可用）。
- 缓存服务单测：命中复用不重跑、版本不一致失效、force 重测、并发不双跑（`-race`）。
- `/api/transcode/hwaccel`、`/api/system/info`、`/api/system/codec-test` 端点测试绿；受影响包 `go test ./internal/...` 全绿；前端 `npm test` 绿。
- 冷缓存下 GET `/api/system/info` 与 `/api/transcode/hwaccel` **不阻塞**（< 1s 返回「未测」），不触发同步 3 分钟实测。

## 6. 风险 / 待定
- **软件 AV1 探测慢**：`libaom-av1` 5 帧也可能逼近 20s 超时 → 用 `libsvtav1` 为主并加快速预设；`libaom-av1` 视情况纳入或注明。
- **Vulkan 设备初始化**：`-init_hw_device vulkan` 在无 Vulkan 环境会失败（如实标记不可用即可，属正常）。
- **破坏性契约**：hwaccel JSON 结构变更，须前后端同 PR 改齐，避免前端读旧字段。
- **冷缓存期转码选码**：预热未完成时 `SelectBestEncoder` 软件兜底（H.264），预热完成后恢复硬件——可接受，且不中断转码。
