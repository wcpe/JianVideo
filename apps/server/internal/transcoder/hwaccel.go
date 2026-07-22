// Package transcoder 提供 FFmpeg 转码管道和硬件加速检测。
package transcoder

import (
	"log"
)

// vaapiDevice 默认 VAAPI 渲染设备路径（Linux）。
const vaapiDevice = "/dev/dri/renderD128"

// hardwareUploadRule 描述需要显式设备初始化与硬件帧上传的编码家族参数。
type hardwareUploadRule struct {
	initDevice   string
	filterDevice string
	uploadFilter string
}

var hardwareUploadRules = map[string]hardwareUploadRule{
	"vaapi": {
		initDevice:   "vaapi=va:" + vaapiDevice,
		filterDevice: "va",
		uploadFilter: "format=nv12,hwupload",
	},
	"vulkan": {
		initDevice:   "vulkan=vk:0",
		filterDevice: "vk",
		uploadFilter: "format=nv12,hwupload",
	},
}

// hardwareUploadRuleFor 返回设备类型对应的显式硬件帧上传规则。
func hardwareUploadRuleFor(deviceType string) (hardwareUploadRule, bool) {
	rule, ok := hardwareUploadRules[deviceType]
	return rule, ok
}

// appendHardwareInputArgs 追加输入前的硬件设备参数；普通家族保持原有 -hwaccel 行为。
func appendHardwareInputArgs(args []string, deviceType string) []string {
	if rule, ok := hardwareUploadRuleFor(deviceType); ok {
		return append(args, "-init_hw_device", rule.initDevice, "-filter_hw_device", rule.filterDevice)
	}
	if deviceType != "" {
		return append(args, "-hwaccel", deviceType)
	}
	return args
}

// appendHardwareUploadArgs 为需要软件帧上传的家族追加视频滤镜参数。
func appendHardwareUploadArgs(args []string, deviceType string) []string {
	if rule, ok := hardwareUploadRuleFor(deviceType); ok {
		return append(args, "-vf", rule.uploadFilter)
	}
	return args
}

// HwAccelInfo 描述一种硬件加速能力。
type HwAccelInfo struct {
	// Name 为硬件加速显示名称，如 "NVIDIA NVENC"。
	Name string
	// DeviceType 为 FFmpeg 设备类型，如 "cuda"、"qsv"。
	DeviceType string
	// H264Encoder 为 H.264 编码器名称。
	H264Encoder string
	// H265Encoder 为 H.265 编码器名称。
	H265Encoder string
	// Available 表示当前环境是否可用该硬件加速。
	Available bool
}

// DetectAllHardwareAccels 检测所有可用的硬件加速能力。
// 按优先级遍历：NVIDIA NVENC → Intel QSV → 软件兜底。
// 返回可用硬件加速器列表；若无任何硬件加速可用，返回空列表（降级为软件）。
func DetectAllHardwareAccels() ([]HwAccelInfo, error) {
	var results []HwAccelInfo

	// 检测 NVIDIA NVENC
	nvidia, err := DetectNVIDIA()
	if err != nil {
		log.Printf("[WARN] NVIDIA 检测失败: %v，跳过", err)
	} else {
		results = append(results, HwAccelInfo{
			Name:        "NVIDIA NVENC",
			DeviceType:  "cuda",
			H264Encoder: nvidia.H264Encoder,
			H265Encoder: nvidia.H265Encoder,
			Available:   nvidia.Available,
		})
	}

	// 检测 Intel QSV
	qsvAvailable, err := QSVAvailable()
	if err != nil {
		log.Printf("[WARN] Intel QSV 检测失败: %v，跳过", err)
	} else {
		results = append(results, HwAccelInfo{
			Name:        "Intel QSV",
			DeviceType:  "qsv",
			H264Encoder: "h264_qsv",
			H265Encoder: "hevc_qsv",
			Available:   qsvAvailable,
		})
	}

	return results, nil
}

// FirstAvailable 返回第一个可用的硬件加速器。
// 若无任何硬件加速可用，返回 nil（调用方应降级为软件编码）。
func FirstAvailable(accels []HwAccelInfo) *HwAccelInfo {
	for i := range accels {
		if accels[i].Available {
			return &accels[i]
		}
	}
	return nil
}
