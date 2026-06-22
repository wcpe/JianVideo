package transcoder

import (
	"errors"
	"fmt"
)

// NVIDIA NVENC 硬件加速检测。
//
// 编码器查找使用 CGO 条件编译：encoder_cgo.go（有 CGO 时调用 FFmpeg C API）、
// encoder_stub.go（无 CGO 时返回 nil）。
// 必须同时支持 H.264 和 H.265 编码器才算可用。

// NVIDIAHWAccel 返回 NVIDIA 硬件加速信息。
type NVIDIAHWAccel struct {
	// H264Encoder 为 H.264 编码器名称，即 "h264_nvenc"。
	H264Encoder string
	// H265Encoder 为 H.265 编码器名称，即 "hevc_nvenc"。
	H265Encoder string
	// Available 表示当前环境是否可用 NVIDIA 硬件加速。
	Available bool
}

// DetectNVIDIA 检测 NVIDIA GPU 是否支持 NVENC 硬件编码。
// 需要同时找到 h264_nvenc 和 hevc_nvenc 编码器。
func DetectNVIDIA() (*NVIDIAHWAccel, error) {
	h264, err := findEncoderByName("h264_nvenc")
	if err != nil {
		return nil, fmt.Errorf("查找 H.264 NVENC 编码器失败: %w", err)
	}
	h265, err := findEncoderByName("hevc_nvenc")
	if err != nil {
		return nil, fmt.Errorf("查找 H.265 NVENC 编码器失败: %w", err)
	}

	return &NVIDIAHWAccel{
		H264Encoder: "h264_nvenc",
		H265Encoder: "hevc_nvenc",
		Available:   h264 && h265,
	}, nil
}

// ErrNVIDIAUnavailable 表示 NVIDIA 硬件加速不可用。
var ErrNVIDIAUnavailable = errors.New("NVIDIA NVENC 不可用")
