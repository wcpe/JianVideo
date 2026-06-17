# 功能规格：Intel QSV 硬件加速

> 状态：开发中　·　关联 PRD：FR-10　·　分支：feature/intel-qsv

## 1. 背景与目标

FR-10 要求支持 Intel QSV 硬件加速转码。Intel Quick Sync Video 利用 Intel 核显的专用硬件单元进行编解码，大幅降低 CPU 负载。本规格定义 QSV 硬件检测与编码器查找的实现范围。

属于第一期 MVP 范围（P1）。

## 2. 需求（要什么）

- 检测 Intel 核显是否存在（通过 sysfs 读取 vendor ID 0x8086 + 驱动名 i915/xe）
- 确认核显无独立显存（非独显）
- 查找 H.264 QSV 编码器（`h264_qsv`）
- 查找 H.265 QSV 编码器（`hevc_qsv`）
- 同时支持 H.264 和 H.265 才算 QSV 可用
- 提供 `GET /api/transcode/hwaccel` 接口返回 QSV 可用性

范围内：Intel 核显检测、QSV 编码器查找、H.264+H.265 双编码支持验证
不做（范围外）：QSV 实际转码管道实现（后续任务）、Windows 平台检测（当前仅 Linux sysfs）

## 3. 设计（怎么做）

### 3.1 模块

新增 `internal/transcoder/` 包，包含：
- `hwaccel.go` — 硬件加速通用检测接口
- `intel_qsv.go` — Intel QSV 专用检测逻辑

### 3.2 Intel 核显检测

通过读取 Linux sysfs：
- `/sys/class/drm/card0/device/vendor` — 应为 `0x8086`
- `/sys/class/drm/card0/device/driver/module/drivers` 或 `/sys/class/drm/card0/device/driver` — 驱动名应为 `i915` 或 `xe`
- 无独立显存：检查 `/sys/class/drm/card0/device/mem_info_vram_total` 不存在或为 0，或通过 `lspci` 判断

### 3.3 编码器查找

通过 FFmpeg C API 查找编码器：
- `avcodec_find_encoder_by_name("h264_qsv")` — H.264 QSV 编码器
- `avcodec_find_encoder_by_name("hevc_qsv")` — H.265 QSV 编码器

两者均非 NULL 时 QSV 可用。

### 3.4 API 响应

复用已有 `GET /api/transcode/hwaccel` 接口，在 `available` 数组中加入 `"qsv"`。

## 4. 任务拆分

- [x] 创建规格文档 `docs/specs/intel-qsv.md`
- [x] PRD §4 FR-10 状态改为「开发中」
- [x] 创建 ADR `docs/adr/0013-intel-qsv.md`
- [ ] 初始化 Go 项目骨架（go.mod、main.go）
- [ ] 编写 QSV 检测单元测试（红阶段）
- [ ] 实现 QSV 检测与编码器查找代码（绿阶段）
- [ ] 文档同步：CHANGELOG 追加变更记录
- [ ] 提交

## 5. 验收标准

- 在有 Intel 核显的 Linux 机器上，`DetectQSV()` 返回 `available: true`
- 在没有 Intel 核显的机器上，`DetectQSV()` 返回 `available: false`
- H.264 和 H.265 编码器同时找到才判定可用
- 单元测试全部通过
- `GET /api/transcode/hwaccel` 响应中包含 QSV 状态

## 6. 风险 / 待定

- **CGO 依赖**：编码器查找需要 FFmpeg C API（CGO），编译环境需有 FFmpeg 开发库
- **平台限制**：Intel 核显检测当前仅通过 Linux sysfs 实现，Windows/macOS 需后续扩展
- **无 CGO 编译环境**：在 Windows 开发机上可能无法编译 CGO 部分，测试只能在 Linux 环境运行
