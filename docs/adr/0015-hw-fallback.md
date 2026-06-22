# ADR-0015：硬件加速自动降级机制

## 状态
已被 [ADR-0033](0033-hwaccel-probe-source-cache.md) 取代

## 背景
FR-10 和 FR-11 分别实现了 Intel QSV 和 NVIDIA NVENC 的独立检测函数。但缺少统一的降级策略：当环境中不存在任何硬件加速、或硬件编码器初始化失败时，转码服务无法自动降级为软件编码，导致转码功能不可用。需要在启动时枚举所有可用硬件加速并按优先级选择最优方案。

## 决策
在 `internal/transcoder/fallback.go` 中实现统一的 `SelectBestEncoder()` 函数，启动时通过 `sync.Once` 调用 `DetectAllHWAccels()` 检测并缓存结果，按优先级（CUDA → QSV → VAAPI → D3D11VA → DXVA2 → VideoToolbox → Vulkan → 软件）选择第一个同时支持 H.264 和 H.265 的编码器；若无可用硬件，返回软件编码器（libx264/libx265）。通过 `GET /api/transcode/hwaccel` API 端点暴露完整检测结果。

## 理由
- **启动时检测 + 缓存**：硬件加速能力在进程生命周期内不变，启动时检测一次即可，避免运行时重复检测的开销
- **sync.Once 懒初始化**：不依赖 main.go 显式调用，transcoder 包自包含初始化逻辑
- **统一接口**：`SelectBestEncoder()` 返回编码器名称和设备类型，调用方无需关心底层检测细节
- **软件兜底**：保证任何环境下转码服务都可用，不因缺少硬件加速而中断

## 后果
- 启动时间略有增加（需遍历所有硬件加速器）
- 本期只覆盖 NVIDIA 和 Intel，后续添加 VAAPI 等需扩展 `DetectAllHWAccels()`
- 运行时硬件状态变化（如热插拔 GPU）不会自动感知，需重启

## 备选方案
- **每次转码时检测**：性能开销大，不采纳
- **提供用户手动选择界面**：增加复杂度，本期自动选择最优，不做手动选择
- **编码过程中降级**：需要 FFmpeg 进程级别的错误捕获和重启，复杂度高，留后续优化
