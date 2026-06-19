package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureSoftwareFallback_Pure 验证软件兜底编码器名称。
func TestEnsureSoftwareFallback_Pure(t *testing.T) {
	h264, h265, err := EnsureSoftwareFallback()
	require.NoError(t, err)
	assert.Equal(t, "libx264", h264)
	assert.Equal(t, "libx265", h265)
}

// TestSelectBestEncoder_FallbackPure 验证无硬件加速时降级为软件编码。
func TestSelectBestEncoder_FallbackPure(t *testing.T) {
	encoderName, deviceType, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", encoderName)
	assert.Equal(t, "", deviceType)
}

// TestBuildInfoFromAccels_Nil 传入 nil 切片时验证软件兜底。
func TestBuildInfoFromAccels_Nil(t *testing.T) {
	info := buildInfoFromAccels(nil)
	require.NotNil(t, info)

	assert.True(t, info.H264Supported, "H264 应通过软件兜底支持")
	assert.True(t, info.H265Supported, "H265 应通过软件兜底支持")
	assert.True(t, info.SoftwareFallback, "无硬件时应为软件兜底")
	assert.Equal(t, "libx264", info.Preferred)
	assert.Len(t, info.Available, 0)
}

// TestBuildInfoFromAccels_Empty 传入空切片时验证软件兜底。
func TestBuildInfoFromAccels_Empty(t *testing.T) {
	info := buildInfoFromAccels([]HwAccelInfo{})
	require.NotNil(t, info)

	assert.True(t, info.H264Supported, "H264 应通过软件兜底支持")
	assert.True(t, info.H265Supported, "H265 应通过软件兜底支持")
	assert.True(t, info.SoftwareFallback, "无硬件时应为软件兜底")
	assert.Equal(t, "libx264", info.Preferred)
	assert.Len(t, info.Available, 0)
}

// TestBuildInfoFromAccels_WithNVIDIA 传入 NVIDIA 加速时验证硬件优先。
func TestBuildInfoFromAccels_WithNVIDIA(t *testing.T) {
	accels := []HwAccelInfo{
		{
			Name:        "NVIDIA NVENC",
			DeviceType:  "cuda",
			H264Encoder: "h264_nvenc",
			H265Encoder: "hevc_nvenc",
			Available:   true,
		},
	}

	info := buildInfoFromAccels(accels)
	require.NotNil(t, info)

	assert.True(t, info.H264Supported)
	assert.True(t, info.H265Supported)
	assert.False(t, info.SoftwareFallback, "有硬件时不应为软件兜底")
	assert.Equal(t, "h264_nvenc", info.Preferred)
	assert.Len(t, info.Available, 1)
	assert.Equal(t, "NVIDIA NVENC", info.Available[0].Name)
	assert.False(t, info.IntelGPU, "NVIDIA 不是 Intel GPU")
}

// TestBuildInfoFromAccels_WithIntelQSV 传入 Intel QSV 加速时验证 QSV 被识别。
func TestBuildInfoFromAccels_WithIntelQSV(t *testing.T) {
	accels := []HwAccelInfo{
		{
			Name:        "Intel QSV",
			DeviceType:  "qsv",
			H264Encoder: "h264_qsv",
			H265Encoder: "hevc_qsv",
			Available:   true,
		},
	}

	info := buildInfoFromAccels(accels)
	require.NotNil(t, info)

	assert.True(t, info.H264Supported)
	assert.True(t, info.H265Supported)
	assert.False(t, info.SoftwareFallback, "有硬件时不应为软件兜底")
	assert.Equal(t, "h264_qsv", info.Preferred)

	// IntelGPU 在 DeviceType=qsv 时始终为 true
	assert.True(t, info.IntelGPU, "QSV 设备类型应识别为 Intel GPU")
	// IntelGPUDetail 仅在 Linux Intel 平台上非空，Windows/macOS 上可能为空
	if info.IntelGPUDetail != "" {
		t.Logf("IntelGPUDetail: %s", info.IntelGPUDetail)
	}
}

// TestBuildInfoFromAccels_Multiple 传入多个加速时验证首选为第一个可用的。
func TestBuildInfoFromAccels_Multiple(t *testing.T) {
	accels := []HwAccelInfo{
		{
			Name:        "NVIDIA NVENC",
			DeviceType:  "cuda",
			H264Encoder: "h264_nvenc",
			H265Encoder: "hevc_nvenc",
			Available:   true,
		},
		{
			Name:        "Intel QSV",
			DeviceType:  "qsv",
			H264Encoder: "h264_qsv",
			H265Encoder: "hevc_qsv",
			Available:   true,
		},
	}

	info := buildInfoFromAccels(accels)
	require.NotNil(t, info)

	assert.False(t, info.SoftwareFallback)
	// 第一个可用的是 NVIDIA NVENC
	assert.Equal(t, "h264_nvenc", info.Preferred)
	assert.Len(t, info.Available, 2)
}

// TestBuildInfoFromAccels_Unavailable 传入所有不可用时验证降级为软件兜底。
func TestBuildInfoFromAccels_Unavailable(t *testing.T) {
	accels := []HwAccelInfo{
		{
			Name:        "NVIDIA NVENC",
			DeviceType:  "cuda",
			H264Encoder: "h264_nvenc",
			H265Encoder: "hevc_nvenc",
			Available:   false,
		},
		{
			Name:        "Intel QSV",
			DeviceType:  "qsv",
			H264Encoder: "h264_qsv",
			H265Encoder: "hevc_qsv",
			Available:   false,
		},
	}

	info := buildInfoFromAccels(accels)
	require.NotNil(t, info)

	assert.True(t, info.H264Supported, "H264 应通过软件兜底支持")
	assert.True(t, info.H265Supported, "H265 应通过软件兜底支持")
	assert.True(t, info.SoftwareFallback, "所有硬件不可用时应为软件兜底")
	assert.Equal(t, "libx264", info.Preferred)
	assert.Len(t, info.Available, 2)
}
