package transcoder

import (
	"testing"
)

// TestSelectBestEncoder_NoHardware 验证无硬件加速时降级为软件编码。
func TestSelectBestEncoder_NoHardware(t *testing.T) {
	// 在无 CGO 环境中，所有硬件检测返回不可用
	// SelectBestEncoder 应降级为软件编码
	enc, deviceType, err := SelectBestEncoder()
	if err != nil {
		t.Fatalf("SelectBestEncoder 返回错误: %v", err)
	}

	// 应返回软件编码器
	if enc == "" {
		t.Fatal("SelectBestEncoder 应返回非空编码器名称")
	}

	// 软件编码器的 deviceType 应为空
	if deviceType != "" {
		t.Errorf("软件编码器 deviceType 应为空, 实际 %q", deviceType)
	}

	t.Logf("选定编码器: %s, 设备类型: %q", enc, deviceType)
}

// TestSelectBestEncoder_ReturnsH264 验证 SelectBestEncoder 返回 H.264 编码器。
func TestSelectBestEncoder_ReturnsH264(t *testing.T) {
	enc, _, err := SelectBestEncoder()
	if err != nil {
		t.Fatalf("SelectBestEncoder 返回错误: %v", err)
	}

	// 应返回 H.264 编码器（软件为 libx264，硬件为 h264_nvenc/h264_qsv 等）
	validH264 := []string{"libx264", "h264_nvenc", "h264_qsv", "h264_vaapi", "h264_amf", "h264_videotoolbox", "h264_vulkan"}
	found := false
	for _, v := range validH264 {
		if enc == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("返回的编码器 %q 不是有效的 H.264 编码器", enc)
	}
}

// TestDetectAllHardwareAccels_ReturnsFullList 验证检测返回完整的硬件加速列表。
func TestDetectAllHardwareAccels_ReturnsFullList(t *testing.T) {
	accels, err := DetectAllHardwareAccels()
	if err != nil {
		t.Fatalf("DetectAllHardwareAccels 返回错误: %v", err)
	}

	// 应至少返回 NVIDIA 和 Intel 两项
	if len(accels) < 2 {
		t.Fatalf("期望至少返回 2 个硬件加速器, 实际 %d", len(accels))
	}

	// 验证第一项是 NVIDIA
	if accels[0].DeviceType != "cuda" {
		t.Errorf("期望第一个为 cuda, 实际 %s", accels[0].DeviceType)
	}

	// 验证第二项是 Intel QSV
	if accels[1].DeviceType != "qsv" {
		t.Errorf("期望第二个为 qsv, 实际 %s", accels[1].DeviceType)
	}

	for _, a := range accels {
		t.Logf("硬件加速: name=%s, type=%s, available=%v", a.Name, a.DeviceType, a.Available)
	}
}

// TestBuildHWAccelInfo 验证 HWAccelInfo 构建逻辑。
func TestBuildHWAccelInfo(t *testing.T) {
	info := BuildHWAccelInfo()

	// available 列表应与 DetectAllHardwareAccels 一致
	if len(info.Available) < 2 {
		t.Fatalf("期望至少 2 个硬件加速记录, 实际 %d", len(info.Available))
	}

	// 在无硬件环境中，preferred 应为软件编码器
	if info.Preferred == "" {
		t.Error("preferred 不应为空，应至少为软件编码器")
	}

	// 在无 CGO 环境中，h264 和 h265 应通过软件支持
	if !info.H264Supported {
		t.Error("H264 应通过软件支持")
	}
	if !info.H265Supported {
		t.Error("H265 应通过软件支持")
	}

	// software_fallback 应为 true（无硬件时）
	if !info.SoftwareFallback {
		t.Error("无硬件加速时 software_fallback 应为 true")
	}

	t.Logf("HWAccelInfo: preferred=%s, intel_gpu=%v, h264=%v, h265=%v, sw_fallback=%v",
		info.Preferred, info.IntelGPU, info.H264Supported, info.H265Supported, info.SoftwareFallback)
}

// TestBuildHWAccelInfo_NoHardware_NoNVENC 验证无 NVIDIA GPU 时的降级。
func TestBuildHWAccelInfo_NoHardware_NoNVENC(t *testing.T) {
	info := BuildHWAccelInfo()

	// 检查 NVIDIA NVENC 在无 CGO 环境中标记为不可用
	found := false
	for _, a := range info.Available {
		if a.DeviceType == "cuda" {
			found = true
			if a.Available {
				t.Error("无 CGO 环境中 NVIDIA NVENC 不应标记为可用")
			}
		}
	}
	if !found {
		t.Error("应包含 NVIDIA NVENC 条目")
	}
}

// TestEnsureSoftwareFallback 验证软件兜底编码器始终可用。
func TestEnsureSoftwareFallback(t *testing.T) {
	h264, h265, err := EnsureSoftwareFallback()
	if err != nil {
		t.Fatalf("EnsureSoftwareFallback 返回错误: %v", err)
	}

	if h264 != "libx264" {
		t.Errorf("H.264 兜底编码器应为 libx264, 实际 %s", h264)
	}
	if h265 != "libx265" {
		t.Errorf("H.265 兜底编码器应为 libx265, 实际 %s", h265)
	}
}

// TestSelectBestEncoder_ConsistencyWithFirstAvailable 验证 SelectBestEncoder 与 FirstAvailable 一致。
func TestSelectBestEncoder_ConsistencyWithFirstAvailable(t *testing.T) {
	accels, err := DetectAllHardwareAccels()
	if err != nil {
		t.Fatalf("DetectAllHardwareAccels 返回错误: %v", err)
	}

	best := FirstAvailable(accels)
	enc, deviceType, err := SelectBestEncoder()
	if err != nil {
		t.Fatalf("SelectBestEncoder 返回错误: %v", err)
	}

	if best == nil {
		// 无硬件可用时，应降级为软件
		if deviceType != "" {
			t.Errorf("无硬件时 deviceType 应为空, 实际 %q", deviceType)
		}
	} else {
		// 有硬件可用时，deviceType 应匹配
		if deviceType != best.DeviceType {
			t.Errorf("deviceType 不一致: SelectBestEncoder=%q, FirstAvailable=%q", deviceType, best.DeviceType)
		}
	}

	t.Logf("FirstAvailable: %v, SelectBestEncoder: enc=%s, device=%q",
		best, enc, deviceType)
}
