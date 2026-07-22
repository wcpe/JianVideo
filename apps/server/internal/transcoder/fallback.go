package transcoder

import (
	"sync/atomic"
)

// CodecSupport 某硬件家族下某编码的实测支持情况。
type CodecSupport struct {
	Codec    string `json:"codec"`   // "h264"/"h265"/"av1"/"vp9"
	Encoder  string `json:"encoder"` // 如 "h264_amf"
	Compiled bool   `json:"compiled"`
	TestedOK bool   `json:"tested_ok"`
}

// HWAccelCapability 单个硬件家族（含软件家族）的 per-codec 能力。
type HWAccelCapability struct {
	Name       string         `json:"name"`        // 显示名，见家族映射
	Family     string         `json:"family"`      // "software"/"amf"/"nvenc"/"qsv"/"vaapi"/"videotoolbox"/"vulkan"
	DeviceType string         `json:"device_type"` // 见家族映射
	Available  bool           `json:"available"`   // 该家族至少一个编码 tested_ok
	Codecs     []CodecSupport `json:"codecs"`
}

// HWAccelInfo 为 /api/transcode/hwaccel 与 /api/system/info 的 hwaccel 响应。
type HWAccelInfo struct {
	Available        []HWAccelCapability `json:"available"`
	Preferred        string              `json:"preferred"`         // 转码默认 H.264 编码器（保证可播）
	Codecs           []string            `json:"codecs"`            // 系统可输出编码并集，顺序 [h264,h265,av1,vp9]
	IntelGPU         bool                `json:"intel_gpu"`         //
	IntelGPUDetail   string              `json:"intel_gpu_detail"`  //
	SoftwareFallback bool                `json:"software_fallback"` // preferred 为软件编码器时 true
	FromCache        bool                `json:"from_cache"`        // 结果来自缓存（未重跑）
	FFmpegVersion    string              `json:"ffmpeg_version"`    //
	TestedAt         string              `json:"tested_at"`         // RFC3339；未测为 ""
}

// familyMeta 家族 → 显示名与 ffmpeg 设备类型的映射。
type familyMeta struct {
	name       string
	deviceType string
}

// familyMetaMap 家族元数据映射（device_type, 显示名）。
var familyMetaMap = map[string]familyMeta{
	"software":     {name: "软件编码", deviceType: ""},
	"qsv":          {name: "Intel QSV", deviceType: "qsv"},
	"nvenc":        {name: "NVIDIA NVENC", deviceType: "cuda"},
	"amf":          {name: "AMD AMF", deviceType: "d3d11va"},
	"vaapi":        {name: "VAAPI", deviceType: "vaapi"},
	"videotoolbox": {name: "Apple VideoToolbox", deviceType: "videotoolbox"},
	"vulkan":       {name: "Vulkan", deviceType: "vulkan"},
}

// familyOrder 家族在 Available 列表中的固定展示顺序。
var familyOrder = []string{"software", "qsv", "nvenc", "amf", "vaapi", "videotoolbox", "vulkan"}

// codecOrder 编码在顶层 Codecs 与家族 Codecs 中的固定顺序。
var codecOrder = []string{"h264", "h265", "av1", "vp9"}

// hardwarePriority 选码时硬件家族的优先级（软件不在内，作为兜底）。
var hardwarePriority = []string{"nvenc", "qsv", "amf", "vaapi", "vulkan", "videotoolbox"}

// BuildCapabilities 将编码器实测结果映射为 per-codec 能力信息（纯函数，无副作用）。
// 不设 FromCache/FFmpegVersion/TestedAt，由服务层填充。
func BuildCapabilities(results []EncoderProbeResult) *HWAccelInfo {
	info := &HWAccelInfo{
		Available: []HWAccelCapability{},
		Codecs:    []string{},
	}

	// 按家族分组（保留每家族内 results 的出现顺序）
	byFamily := make(map[string][]EncoderProbeResult)
	for _, r := range results {
		byFamily[r.Family] = append(byFamily[r.Family], r)
	}

	// 顶层 Codecs：某编码在任一家族 tested_ok 即纳入
	codecOK := make(map[string]bool)

	for _, family := range familyOrder {
		group, ok := byFamily[family]
		if !ok {
			continue
		}
		meta := familyMetaMap[family]
		capability := HWAccelCapability{
			Name:       meta.name,
			Family:     family,
			DeviceType: meta.deviceType,
			Codecs:     make([]CodecSupport, 0, len(group)),
		}
		for _, r := range group {
			capability.Codecs = append(capability.Codecs, CodecSupport{
				Codec:    r.Codec,
				Encoder:  r.Encoder,
				Compiled: r.Compiled,
				TestedOK: r.TestedOK,
			})
			if r.TestedOK {
				capability.Available = true
				codecOK[r.Codec] = true
			}
		}
		info.Available = append(info.Available, capability)
	}

	// 顶层 Codecs 按固定编码顺序输出并集
	for _, codec := range codecOrder {
		if codecOK[codec] {
			info.Codecs = append(info.Codecs, codec)
		}
	}

	// Preferred：按硬件优先级取第一个 tested_ok 的 H.264 编码器，无则软件兜底
	preferred, _, ok := SelectEncoderForCodec(results, "h264")
	if ok {
		info.Preferred = preferred
	} else {
		info.Preferred = "libx264"
	}
	info.SoftwareFallback = len(info.Preferred) >= 3 && info.Preferred[:3] == "lib"

	// Intel GPU 判定：仅当 qsv 家族有实测可用编码器，或 sysfs 确认 Intel 核显时才置真。
	// 不再因候选清单恒含 qsv 而误报（避免在非 Intel 机器上出现幻影 Intel GPU）。
	qsvUsable := false
	for _, r := range byFamily["qsv"] {
		if r.TestedOK {
			qsvUsable = true
			break
		}
	}
	intelGPU, err := isIntelGPU()
	intelGPU = err == nil && intelGPU
	if qsvUsable || intelGPU {
		info.IntelGPU = true
		if intelGPU {
			info.IntelGPUDetail = "Intel iGPU detected via sysfs"
		}
	}

	return info
}

// SelectEncoderForCodec 在实测结果中按硬件优先级选取指定编码的可用编码器。
// 仅在 tested_ok 且 codec 匹配的候选中选择；返回编码器名与家族 device_type。
func SelectEncoderForCodec(results []EncoderProbeResult, codec string) (encoder, deviceType string, ok bool) {
	for _, family := range hardwarePriority {
		for _, r := range results {
			if r.Family == family && r.Codec == codec && r.TestedOK {
				return r.Encoder, familyMetaMap[family].deviceType, true
			}
		}
	}
	return "", "", false
}

// probeSnapshot 实测结果的进程级快照，供 SelectBestEncoder 选码使用。
var probeSnapshot atomic.Pointer[[]EncoderProbeResult]

// setProbeSnapshot 更新实测结果快照（每次实测或缓存命中后调用）。
func setProbeSnapshot(r []EncoderProbeResult) {
	probeSnapshot.Store(&r)
}

// clearProbeSnapshot 清除已失效的进程级实测快照。
func clearProbeSnapshot() {
	probeSnapshot.Store(nil)
}

// storeProbeSnapshotForGeneration 仅在路径代次仍匹配时发布快照。
// 调用方可持有 CapabilityService.mu；SetFFmpegPath 不获取该锁，避免形成反向锁序。
func storeProbeSnapshotForGeneration(generation uint64, snapshot *[]EncoderProbeResult) bool {
	ffmpegPathMu.RLock()
	defer ffmpegPathMu.RUnlock()
	if generation != ffmpegPathGeneration {
		return false
	}
	probeSnapshot.Store(snapshot)
	return true
}

// SelectBestEncoder 选择最优的 H.264 编码器（保持签名不变，pipeline 不动）。
// 读实测快照：快照为空时软件兜底 ("libx264", "")；否则按硬件优先级选 H.264。
func SelectBestEncoder() (encoderName string, deviceType string, err error) {
	snap := probeSnapshot.Load()
	if snap == nil || len(*snap) == 0 {
		return "libx264", "", nil
	}
	if enc, dev, ok := SelectEncoderForCodec(*snap, "h264"); ok {
		return enc, dev, nil
	}
	return "libx264", "", nil
}

// EnsureSoftwareFallback 返回软件兜底的 H.264 和 H.265 编码器名称。
func EnsureSoftwareFallback() (h264 string, h265 string, err error) {
	return "libx264", "libx265", nil
}
