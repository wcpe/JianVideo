# 功能规格：硬件加速自动降级机制

> 状态：开发中　·　关联 PRD：FR-12　·　分支：feature/hw-fallback

## 1. 背景与目标

FR-10（Intel QSV）和 FR-11（NVIDIA NVENC）已经实现了单一硬件加速的检测函数。但在实际使用中，用户环境可能不存在任何硬件加速（如无 GPU 的服务器），或硬件编码器初始化失败。当前缺少统一的降级策略：需要一个机制在启动时枚举所有可用硬件加速并按优先级选择最优方案，当硬件编码失败时自动降级为软件编码（libx264/libx265），确保转码服务始终可用。

属于第一期（MVP）硬件加速管理（ARCHITECTURE §5.4）。

## 2. 需求（要什么）

- 启动时调用 `DetectAllHWAccels()` 检测所有可用硬件加速，按优先级遍历：CUDA → QSV → VAAPI → D3D11VA → DXVA2 → VideoToolbox → Vulkan → 软件
- 每个硬件必须同时支持 H.264 和 H.265 编码器才算可用
- `SelectBestEncoder()` 从检测结果中选择最优编码器，返回编码器名称和设备类型
- 硬件加速失败时自动降级为软件编码（libx264/libx265），不中断转码
- `GET /api/transcode/hwaccel` 端点返回硬件加速能力信息，结构为 `HWAccelInfo`
- 响应结构包含：`available`（可用列表）、`preferred`（优先选择）、`intel_gpu`（是否为 Intel 核显）、`intel_gpu_detail`（Intel GPU 详情）、`h264_supported`、`h265_supported`、`software_fallback`

范围内：
- 统一硬件检测接口 `DetectAllHWAccels()` 和 `SelectBestEncoder()`
- 降级逻辑封装在 `fallback.go`
- HTTP API 端点 `GET /api/transcode/hwaccel` 在 `hwaccel_api.go`
- 启动时初始化检测并缓存结果

不做（范围外）：
- 运行时动态切换硬件加速（重启后重新检测）
- 硬件加速失败的实时重试/降级（编码过程中的降级属于后续优化）
- 用户手动选择硬件加速（本期自动选择最优）
- 硬件解码（NVDEC/QSV 解码）的检测（本期只关注编码）

## 3. 设计（怎么做）

### 3.1 模块与文件

| 文件 | 职责 |
|---|---|
| `internal/transcoder/hwaccel.go` | 统一检测接口 `HwAccelInfo`、`DetectAllHWAccels()`、`FirstAvailable()` |
| `internal/transcoder/nvidia_nvenc.go` | NVIDIA NVENC 检测（`DetectNVIDIA()`） |
| `internal/transcoder/intel_qsv.go` | Intel QSV 检测（`QSVAvailable()`、`isIntelGPU()`） |
| `internal/transcoder/fallback.go` | 降级逻辑：`SelectBestEncoder()`、`HWAccelInfo` 构建、`EnsureSoftwareFallback()` |
| `internal/transcoder/hwaccel_api.go` | HTTP API 端点处理器 |
| `internal/transcoder/encoder_cgo.go` | CGO 编码器查找（`findEncoderByName()`） |
| `internal/transcoder/encoder_stub.go` | 无 CGO 时的 stub |

### 3.2 关键类型

```go
// HWAccelCapability 描述单个硬件加速能力。
type HWAccelCapability struct {
    Name        string `json:"name"`         // 显示名称，如 "NVIDIA NVENC"
    DeviceType  string `json:"device_type"`  // FFmpeg 设备类型，如 "cuda"
    H264Encoder string `json:"h264_encoder"` // H.264 编码器名称
    H265Encoder string `json:"h265_encoder"` // H.265 编码器名称
    Available   bool   `json:"available"`    // 是否可用
}

// HWAccelInfo 为 API 响应结构。
type HWAccelInfo struct {
    Available        []HWAccelCapability `json:"available"`
    Preferred        string              `json:"preferred"`
    IntelGPU         bool                `json:"intel_gpu"`
    IntelGPUDetail   string              `json:"intel_gpu_detail"`
    H264Supported    bool                `json:"h264_supported"`
    H265Supported    bool                `json:"h265_supported"`
    SoftwareFallback bool                `json:"software_fallback"`
}
```

### 3.3 降级策略

1. 启动时调用 `DetectAllHWAccels()` 获取全部硬件加速信息
2. `SelectBestEncoder()` 调用 `FirstAvailable()` 按优先级选择第一个可用硬件
3. 若无任何硬件可用，`SelectBestEncoder()` 返回软件编码器（libx264 或 libx265）
4. API 端点返回完整检测结果，供前端展示和用户了解当前环境能力

### 3.4 启动时初始化

在应用启动阶段（main.go 或 transcoder 初始化的 `init()` 中）调用检测函数并缓存结果，避免每次 API 请求都重新检测。本期在 `fallback.go` 中通过 `sync.Once` 实现懒初始化。

## 4. 任务拆分

- [x] 复制 FR-10/FR-11 共享代码（hwaccel.go, encoder_cgo.go, encoder_stub.go, intel_qsv.go, intel_qsv_cgo.go, intel_qsv_stub.go, nvidia_nvenc.go）
- [ ] 编写规格文档 `docs/specs/hw-fallback.md`
- [ ] PRD §4 把 FR-12 状态从「计划」翻为「开发中」
- ] 写 ADR-0015 `docs/adr/0015-hw-fallback.md`
- [ ] 测试先行：编写 `fallback_test.go`（红阶段）
- [ ] 实现 `fallback.go`（SelectBestEncoder、HWAccelInfo 构建）
- [ ] 实现 `hwaccel_api.go`（HTTP 端点处理器）
- [ ] 更新 `docs/API.md` 中 `/api/transcode/hwaccel` 的响应结构
- [ ] 运行测试验证从红转绿
- [ ] CHANGELOG 追加一行
- [ ] 文档同步：ARCHITECTURE §5.4 更新

## 5. 验收标准

- `DetectAllHWAccels()` 返回完整的硬件加速列表（NVIDIA + Intel），每项包含编码器和可用状态
- `SelectBestEncoder()` 在无硬件可用时返回软件编码器名称
- `GET /api/transcode/hwaccel` 返回符合 `HWAccelInfo` 结构的 JSON
- 单元测试全部通过（含边界条件：空列表、仅 H.264、仅 H.265、全部不可用）
- `go vet ./internal/transcoder/` 无告警

## 6. 风险 / 待定

- CGO 检测依赖 FFmpeg 开发库，CI 环境可能无法运行 CGO 测试；通过 build tag 区分 cgo 和 stub 测试
- 本期只覆盖 NVIDIA 和 Intel 两种硬件加速；VAAPI、D3D11VA 等在后续 FR 中添加时需扩展 `DetectAllHWAccels()`
