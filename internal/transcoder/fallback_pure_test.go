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

// TestBuildCapabilities_Nil nil 结果 → 软件兜底、空列表、未测。
func TestBuildCapabilities_Nil(t *testing.T) {
	info := BuildCapabilities(nil)
	require.NotNil(t, info)

	assert.Empty(t, info.Available, "无结果时家族列表应为空")
	assert.Empty(t, info.Codecs, "无结果时可输出编码并集应为空")
	assert.Equal(t, "libx264", info.Preferred)
	assert.True(t, info.SoftwareFallback, "无结果时应为软件兜底")
	assert.False(t, info.IntelGPU)
}

// TestBuildCapabilities_Empty 空切片同 nil。
func TestBuildCapabilities_Empty(t *testing.T) {
	info := BuildCapabilities([]EncoderProbeResult{})
	require.NotNil(t, info)
	assert.Empty(t, info.Available)
	assert.Empty(t, info.Codecs)
	assert.Equal(t, "libx264", info.Preferred)
	assert.True(t, info.SoftwareFallback)
}

// TestBuildCapabilities_SoftwareOnly 仅软件可用 → preferred 软件、family available。
func TestBuildCapabilities_SoftwareOnly(t *testing.T) {
	results := []EncoderProbeResult{
		{Encoder: "libx264", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
		{Encoder: "libx265", Family: "software", Codec: "h265", Compiled: true, TestedOK: true},
		{Encoder: "libsvtav1", Family: "software", Codec: "av1", Compiled: true, TestedOK: true},
		{Encoder: "libvpx-vp9", Family: "software", Codec: "vp9", Compiled: false, TestedOK: false},
	}
	info := BuildCapabilities(results)
	require.NotNil(t, info)

	require.Len(t, info.Available, 1)
	assert.Equal(t, "software", info.Available[0].Family)
	assert.Equal(t, "软件编码", info.Available[0].Name)
	assert.True(t, info.Available[0].Available, "至少一编码成功，家族应可用")
	assert.Len(t, info.Available[0].Codecs, 4)

	// 顶层 Codecs 并集按顺序 h264,h265,av1（vp9 未成功）
	assert.Equal(t, []string{"h264", "h265", "av1"}, info.Codecs)
	assert.Equal(t, "libx264", info.Preferred)
	assert.True(t, info.SoftwareFallback)
}

// TestBuildCapabilities_AMFFamilyAvailable amf 含 h264/h265 成功 + av1 失败 → 家族可用、av1 不入并集。
func TestBuildCapabilities_AMFFamilyAvailable(t *testing.T) {
	results := []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
		{Encoder: "hevc_amf", Family: "amf", Codec: "h265", Compiled: true, TestedOK: true},
		{Encoder: "av1_amf", Family: "amf", Codec: "av1", Compiled: true, TestedOK: false},
	}
	info := BuildCapabilities(results)
	require.NotNil(t, info)

	require.Len(t, info.Available, 1)
	cap := info.Available[0]
	assert.Equal(t, "amf", cap.Family)
	assert.Equal(t, "AMD AMF", cap.Name)
	assert.Equal(t, "d3d11va", cap.DeviceType)
	assert.True(t, cap.Available, "h264/h265 成功，家族应可用")

	// av1 试编码失败，不入顶层并集
	assert.Equal(t, []string{"h264", "h265"}, info.Codecs)

	// preferred 走硬件 H.264
	assert.Equal(t, "h264_amf", info.Preferred)
	assert.False(t, info.SoftwareFallback)
}

// TestBuildCapabilities_MultiFamilyPriority 多家族时 preferred 按硬件优先级（nvenc 先于 amf）。
func TestBuildCapabilities_MultiFamilyPriority(t *testing.T) {
	results := []EncoderProbeResult{
		{Encoder: "libx264", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
		{Encoder: "h264_nvenc", Family: "nvenc", Codec: "h264", Compiled: true, TestedOK: true},
	}
	info := BuildCapabilities(results)
	require.NotNil(t, info)

	// 优先级 nvenc > qsv > amf，故 preferred 为 nvenc
	assert.Equal(t, "h264_nvenc", info.Preferred)
	assert.False(t, info.SoftwareFallback)

	// 家族顺序固定 software, nvenc, amf（按 familyOrder）
	require.Len(t, info.Available, 3)
	assert.Equal(t, "software", info.Available[0].Family)
	assert.Equal(t, "nvenc", info.Available[1].Family)
	assert.Equal(t, "amf", info.Available[2].Family)
}

// TestBuildCapabilities_AllUnavailable 全部试编码失败 → 软件兜底。
func TestBuildCapabilities_AllUnavailable(t *testing.T) {
	results := []EncoderProbeResult{
		{Encoder: "h264_nvenc", Family: "nvenc", Codec: "h264", Compiled: true, TestedOK: false},
		{Encoder: "h264_qsv", Family: "qsv", Codec: "h264", Compiled: false, TestedOK: false},
	}
	info := BuildCapabilities(results)
	require.NotNil(t, info)

	assert.Empty(t, info.Codecs, "无任何编码成功，并集应为空")
	assert.Equal(t, "libx264", info.Preferred)
	assert.True(t, info.SoftwareFallback)

	// qsv 实测不可用 → 不再因候选含 qsv 而误报 IntelGPU（仅 sysfs 确认时才可能为真）。
	// 本机非 Intel 核显，sysfs 检测返回否，故应为 false。
	assert.False(t, info.IntelGPU, "qsv 不可用且非 Intel 核显，IntelGPU 不应误报")
}

// TestSelectEncoderForCodec_Priority 验证按硬件优先级选码。
func TestSelectEncoderForCodec_Priority(t *testing.T) {
	results := []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true},
		{Encoder: "h264_qsv", Family: "qsv", Codec: "h264", TestedOK: true},
		{Encoder: "hevc_amf", Family: "amf", Codec: "h265", TestedOK: true},
	}

	// qsv 优先于 amf
	enc, dev, ok := SelectEncoderForCodec(results, "h264")
	require.True(t, ok)
	assert.Equal(t, "h264_qsv", enc)
	assert.Equal(t, "qsv", dev)

	// h265 仅 amf 成功
	enc, dev, ok = SelectEncoderForCodec(results, "h265")
	require.True(t, ok)
	assert.Equal(t, "hevc_amf", enc)
	assert.Equal(t, "d3d11va", dev)

	// 不存在的编码 → 未命中
	_, _, ok = SelectEncoderForCodec(results, "vp9")
	assert.False(t, ok)
}

// TestSelectEncoderForCodec_OnlyTestedOK 验证仅在 tested_ok 中选取。
func TestSelectEncoderForCodec_OnlyTestedOK(t *testing.T) {
	results := []EncoderProbeResult{
		{Encoder: "h264_nvenc", Family: "nvenc", Codec: "h264", Compiled: true, TestedOK: false},
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	}
	enc, _, ok := SelectEncoderForCodec(results, "h264")
	require.True(t, ok)
	assert.Equal(t, "h264_amf", enc, "nvenc 未通过试编码，应跳过选 amf")
}

// TestSelectBestEncoder_WithSnapshot 设快照后 SelectBestEncoder 走硬件。
func TestSelectBestEncoder_WithSnapshot(t *testing.T) {
	t.Cleanup(func() { probeSnapshot.Store(nil) })

	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true},
	})
	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "h264_amf", enc)
	assert.Equal(t, "d3d11va", dev)
}

// TestSelectBestEncoder_SnapshotNoH264 快照有结果但无可用 H.264 → 软件兜底。
func TestSelectBestEncoder_SnapshotNoH264(t *testing.T) {
	t.Cleanup(func() { probeSnapshot.Store(nil) })

	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "hevc_amf", Family: "amf", Codec: "h265", TestedOK: true},
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: false},
	})
	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", enc)
	assert.Equal(t, "", dev)
}
