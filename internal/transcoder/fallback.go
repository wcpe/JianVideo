package transcoder

import (
	"log"
	"sync"
)

// HWAccelCapability 描述单个硬件加速能力。
type HWAccelCapability struct {
	Name        string `json:"name"`         // 显示名称，如 "NVIDIA NVENC"
	DeviceType  string `json:"device_type"`  // FFmpeg 设备类型，如 "cuda"
	H264Encoder string `json:"h264_encoder"` // H.264 编码器名称
	H265Encoder string `json:"h265_encoder"` // H.265 编码器名称
	Available   bool   `json:"available"`    // 是否可用
}

// HWAccelInfo 为 /api/transcode/hwaccel 的响应结构。
type HWAccelInfo struct {
	Available        []HWAccelCapability `json:"available"`
	Preferred        string              `json:"preferred"`
	IntelGPU         bool                `json:"intel_gpu"`
	IntelGPUDetail   string              `json:"intel_gpu_detail"`
	H264Supported    bool                `json:"h264_supported"`
	H265Supported    bool                `json:"h265_supported"`
	SoftwareFallback bool                `json:"software_fallback"`
}

// 检测缓存：启动时检测一次，后续直接返回缓存结果。
var (
	cachedAccels []HwAccelInfo
	cachedInfo   *HWAccelInfo
	detectOnce   sync.Once
)

// SelectBestEncoder 选择最优的 H.264 编码器。
// 按优先级遍历所有硬件加速，返回第一个同时支持 H.264 和 H.265 的编码器名称及其设备类型。
// 若无可用硬件，返回软件编码器 ("libx264", "")。
func SelectBestEncoder() (encoderName string, deviceType string, err error) {
	accels, err := DetectAllHardwareAccels()
	if err != nil {
		return "", "", err
	}

	best := FirstAvailable(accels)
	if best != nil {
		return best.H264Encoder, best.DeviceType, nil
	}

	// 降级为软件编码
	return "libx264", "", nil
}

// BuildHWAccelInfo 构建完整的硬件加速能力信息。
// 通过 sync.Once 确保启动时只检测一次，后续调用返回缓存结果。
func BuildHWAccelInfo() (info *HWAccelInfo) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] 硬件加速检测发生 panic: %v，降级为软件编码", r)
			// panic 后 detectOnce 已标记为完成，后续调用直接返回安全降级结果
			cachedInfo = buildInfoFromAccels(nil)
			info = cachedInfo
		}
	}()
	detectOnce.Do(func() {
		accels, err := DetectAllHardwareAccels()
		if err != nil {
			// 检测失败时仍构建降级结果
			accels = nil
		}
		cachedAccels = accels
		cachedInfo = buildInfoFromAccels(accels)
	})
	return cachedInfo
}

// buildInfoFromAccels 从硬件加速列表构建 HWAccelInfo。
func buildInfoFromAccels(accels []HwAccelInfo) *HWAccelInfo {
	info := &HWAccelInfo{
		Available: make([]HWAccelCapability, 0, len(accels)),
	}

	h264Supported := false
	h265Supported := false

	for _, a := range accels {
		cap := HWAccelCapability{
			Name:        a.Name,
			DeviceType:  a.DeviceType,
			H264Encoder: a.H264Encoder,
			H265Encoder: a.H265Encoder,
			Available:   a.Available,
		}
		info.Available = append(info.Available, cap)

		if a.Available {
			h264Supported = true
			h265Supported = true

			// 第一个可用的即为 preferred
			if info.Preferred == "" {
				info.Preferred = a.H264Encoder
			}
		}

		// Intel GPU 检测
		if a.DeviceType == "qsv" {
			info.IntelGPU = true
			intelGPU, err := isIntelGPU()
			if err == nil && intelGPU {
				info.IntelGPUDetail = "Intel iGPU detected via sysfs"
			}
		}
	}

	// 软件兜底始终可用
	if !h264Supported {
		h264Supported = true // libx264
	}
	if !h265Supported {
		h265Supported = true // libx265
	}

	info.H264Supported = h264Supported
	info.H265Supported = h265Supported
	info.SoftwareFallback = info.Preferred == ""

	// 若无硬件可用，preferred 设为软件编码器
	if info.Preferred == "" {
		info.Preferred = "libx264"
	}

	return info
}

// EnsureSoftwareFallback 返回软件兜底的 H.264 和 H.265 编码器名称。
func EnsureSoftwareFallback() (h264 string, h265 string, err error) {
	return "libx264", "libx265", nil
}
