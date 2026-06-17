# ADR-0013: Intel QSV 硬件加速

## 状态
已接受

## 背景
FR-10 要求支持 Intel QSV 硬件加速转码。需要决策：如何检测 Intel 核显存在、如何验证 QSV 编码器可用性、以及如何在不支持 QSV 的环境下降级。

Intel 核显检测的主流方案：
1. **CGO + libdrm**：直接调用 DRM API 查询显卡信息，精确但引入 CGO 依赖
2. **sysfs 读取**：读取 `/sys/class/drm/` 下的设备信息，无需额外依赖，仅限 Linux
3. **exec lspci**：调用外部命令解析输出，跨平台但解析脆弱

编码器查找方案：
1. **CGO + libavcodec**：调用 `avcodec_find_encoder_by_name()`，精确且直接
2. **exec ffmpeg -encoders**：调用外部命令解析输出，无需 CGO 但依赖 ffmpeg 二进制

## 决策
Intel 核显检测采用 sysfs 读取（Linux），编码器查找采用 CGO + libavcodec。

## 理由
- **sysfs 检测**：Linux 下最轻量方案，无需外部命令或 CGO，直接读取内核暴露的信息。虽然仅限 Linux，但 QSV 本身就是 Linux 为主的硬件加速方案（Windows 上可通过 DXVA2/D3D11 实现，留后续扩展）。
- **CGO 编码器查找**：直接调用 FFmpeg C API 比解析命令行输出更可靠，且项目已决定使用 CGO 绑定 FFmpeg（ARCHITECTURE 决策），架构一致。
- **双编码验证**：必须同时找到 `h264_qsv` 和 `hevc_qsv` 才算 QSV 可用，符合 ARCHITECTURE §5.2 的要求。

## 后果
- QSV 检测代码依赖 Linux sysfs，在 Windows/macOS 上需降级返回不可用
- 编码器查找依赖 CGO 编译环境（需 libavcodec-dev）
- sysfs 路径 `card0` 在多显卡环境下可能需要遍历 `card0` ~ `cardN`

## 备选方案
- **exec lspci + 解析**：跨平台但解析脆弱，放弃
- **CGO + libdrm**：功能更完整但引入额外 CGO 依赖，当前 sysfs 足够，留后续按需引入
- **纯 Go 解析 `ffmpeg -encoders` 输出**：无需 CGO 但依赖外部二进制，解析易受版本差异影响，放弃
