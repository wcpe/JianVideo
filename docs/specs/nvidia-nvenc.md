# 功能规格：NVIDIA NVENC/NVDEC 硬件加速转码

> 状态：开发中　·　关联 PRD：FR-11　·　分支：feature/fr-11-nvenc

## 1. 背景与目标

为视频媒体服务器添加 NVIDIA GPU 硬件加速转码能力。利用 NVENC（编码）和 NVDEC（解码）将 H.264/H.265 转码负载从 CPU 卸载到 NVIDIA GPU，显著降低 CPU 占用率。属于第一期 MVP 的核心硬件加速组件，与 FR-10（Intel QSV）并列。

## 2. 需求（要什么）

- 启动时检测系统中是否存在可用的 NVIDIA GPU（通过 FFmpeg CUDA 硬件设备类型）
- 检测 NVIDIA GPU 是否同时支持 H.264（`h264_nvenc`）和 H.265（`hevc_nvenc`）编码器
- 提供统一接口供转码管理器查询可用硬件编码器
- 硬件加速失败时自动降级为软件编码，不中断播放
- 范围内：NVIDIA NVENC/NVDEC 检测与编码器查找
- 不做（范围外）：实际转码管道实现、显存管理、多 GPU 选择

## 3. 设计（怎么做）

### 3.1 文件结构

| 文件 | 职责 |
|---|---|
| `internal/transcoder/hwaccel.go` | 硬件加速统一接口与优先级选择 |
| `internal/transcoder/nvidia_nvenc.go` | NVIDIA GPU 检测与 NVENC 编码器查找 |
| `internal/transcoder/nvidia_nvenc_test.go` | NVIDIA 检测的单元测试 |

### 3.2 检测流程

1. 调用 `av_hwdevice_find_type_by_name("cuda")` 获取 CUDA 设备类型枚举值
2. 遍历所有 CUDA 设备（`av_hwdevice_iterate_types`），尝试创建硬件设备上下文（`av_hwdevice_ctx_create`）
3. 对每个成功打开的 CUDA 设备，依次查找 `h264_nvenc` 和 `hevc_nvenc` 编码器（`avcodec_find_encoder_by_name`）
4. 两个编码器均找到时，标记 NVIDIA 硬件加速可用
5. 将编码器名称与设备信息封装为 `HwAccelInfo` 返回

### 3.3 统一接口

`hwaccel.go` 定义共享结构体与接口：

```go
type HwAccelInfo struct {
    Name        string // 显示名称，如 "NVIDIA NVENC"
    DeviceType  string // FFmpeg 设备类型，如 "cuda"
    H264Encoder string // H.264 编码器名称，如 "h264_nvenc"
    H265Encoder string // H.265 编码器名称，如 "hevc_nvenc"
    Available   bool   // 是否可用
}

type HwAccelDetector interface {
    Detect() ([]HwAccelInfo, error)
}
```

`hwaccel.go` 的 `DetectAllHardwareAccels()` 遍历所有已注册的检测器（NVIDIA、Intel QSV），按优先级返回可用硬件列表。

### 3.4 CGO 依赖

- 使用 `github.com/csnewman/ffmpeg-go` 的 CGO 绑定
- 通过 CGo 调用 FFmpeg 的 `libavcodec` 和 `libavutil` API
- 编码器查找使用纯 `avcodec_*` 接口，不涉及实际转码

## 4. 任务拆分

- [x] 创建规格文档 `docs/specs/nvidia-nvenc.md`
- [x] 创建 ADR `docs/adr/0014-nvidia-nvenc.md`
- [x] PRD §4 改 FR-11 状态为「开发中」
- [ ] 测试先行：编写 `nvidia_nvenc_test.go`（红阶段）
- [ ] 实现 `nvidia_nvenc.go`（检测逻辑）
- [ ] 实现 `hwaccel.go`（统一接口）
- [ ] 测试通过（绿阶段）
- [ ] CHANGELOG 追加
- [ ] 文档同步：ARCHITECTURE 更新

## 5. 验收标准

- 在有 NVIDIA GPU 的机器上，`Detect()` 返回 `Available=true` 且包含 `h264_nvenc` 和 `hevc_nvenc`
- 在没有 NVIDIA GPU 的机器上，`Detect()` 返回 `Available=false`，不 panic
- `DetectAllHardwareAccels()` 正确汇总 NVIDIA 和 Intel QSV 的检测结果
- 所有单元测试通过（go test ./internal/transcoder/...）
- 手动验收步骤：在有 NVIDIA GPU 的机器上运行测试，确认检测可用

## 6. 风险 / 待定

- CGO 编译依赖 FFmpeg 开发库已安装，CI 环境需提前配置
- `ffmpeg-go` 绑定的 API 覆盖范围需验证，部分硬件设备创建接口可能未暴露
- 多 GPU 场景暂不处理，默认使用第一个可用设备
