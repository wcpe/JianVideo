# ADR-0014: NVIDIA NVENC 硬件加速

## 状态
提议中

## 背景
项目第一期 MVP 需要支持 NVIDIA GPU 硬件加速转码。FR-10 已定义 Intel QSV 的检测方案，现需补充 NVIDIA NVENC/NVDEC 的检测与编码器查找。NVIDIA 硬件通过 CUDA 设备类型（`cuda`）暴露，编码器为 `h264_nvenc` 和 `hevc_nvenc`。需统一硬件加速检测接口，使转码管理器可按优先级选择可用硬件。

## 决策
在 `internal/transcodec/` 下实现 NVIDIA 检测模块，通过 FFmpeg C API（CGO 绑定）检测 CUDA 设备可用性与编码器支持，与 Intel QSV 模块并列，由 `hwaccel.go` 统一调度。

## 理由
- **架构一致性**：与 FR-10 的 Intel QSV 检测逻辑对称，每个硬件平台一个文件，`hwaccel.go` 只负责统一接口和优先级遍历
- **最小依赖**：NVIDIA 检测仅需 `avcodec_find_encoder_by_name` 和 `av_hwdevice_*` 系列 API，FFmpeg 原生支持，无需额外库
- **安全降级**：检测失败时返回 `Available=false`，不影响软件编码兜底

## 后果
- 正：补齐 NVIDIA 硬件加速检测，使硬件加速覆盖 Intel + NVIDIA 两大平台
- 正：统一接口设计便于后续添加 AMD AMF、VAAPI 等其他硬件平台
- 负：CGO 编译要求目标机器安装 FFmpeg 开发库，增加构建环境复杂度
- 负：`ffmpeg-go` 可能未暴露全部硬件设备 API，需验证

## 备选方案
- **方案 B：调用 `ffmpeg -encoders` 命令行解析**：无需 CGO，通过 `os/exec` 执行 FFmpeg 命令并解析输出。落选原因：解析文本输出脆弱，版本间格式可能变化，且无法检测硬件设备可用性
- **方案 C：统一在 `hwaccel.go` 中实现所有硬件检测**：减少文件数量。落选原因：违反单一职责，文件随硬件平台增加而膨胀，与 FR-10 的分离式设计不一致
