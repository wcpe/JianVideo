package transcoder

import (
	"testing"
)

// TestSelectBestEncoder_NoSnapshot 验证未设快照时降级为软件编码。
func TestSelectBestEncoder_NoSnapshot(t *testing.T) {
	// 清空快照，模拟冷态（预热未完成）
	probeSnapshot.Store(nil)

	enc, deviceType, err := SelectBestEncoder()
	if err != nil {
		t.Fatalf("SelectBestEncoder 返回错误: %v", err)
	}
	if enc != "libx264" {
		t.Errorf("无快照时应返回 libx264，实际 %q", enc)
	}
	if deviceType != "" {
		t.Errorf("软件编码器 deviceType 应为空, 实际 %q", deviceType)
	}
}

// TestDetectAllHardwareAccels_ReturnsFullList 验证 CGO 检测返回完整的硬件加速列表（hwaccel.go 保留）。
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
