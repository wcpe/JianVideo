package transcoder

import (
	"testing"
)

// TestDetectNVIDIA_EncoderLookup 验证 NVIDIA 编码器查找逻辑。
// 在无 CGO 或没有 NVIDIA GPU 的环境中，编码器应为 nil，Available 为 false。
func TestDetectNVIDIA_EncoderLookup(t *testing.T) {
	nvidia, err := DetectNVIDIA()
	if err != nil {
		t.Fatalf("DetectNVIDIA 返回错误: %v", err)
	}

	if nvidia == nil {
		t.Fatal("DetectNVIDIA 不应返回 nil")
	}

	// 验证编码器名称字段始终正确
	if nvidia.H264Encoder != "h264_nvenc" {
		t.Errorf("H264Encoder 期望 h264_nvenc, 实际 %s", nvidia.H264Encoder)
	}
	if nvidia.H265Encoder != "hevc_nvenc" {
		t.Errorf("H265Encoder 期望 hevc_nvenc, 实际 %s", nvidia.H265Encoder)
	}

	t.Logf("NVIDIA NVENC 可用: %v", nvidia.Available)
}

// TestDetectNVIDIA_RequiresBothCodecs 验证 H.264 和 H.265 必须同时支持才算可用。
func TestDetectNVIDIA_RequiresBothCodecs(t *testing.T) {
	nvidia, err := DetectNVIDIA()
	if err != nil {
		t.Fatalf("DetectNVIDIA 返回错误: %v", err)
	}

	// 在无 CGO 环境中，两个编码器都找不到，Available 应为 false。
	// 在有 CGO 且有 NVIDIA GPU 的环境中，两个编码器都找到，Available 应为 true。
	h264, err := findEncoderByName("h264_nvenc")
	if err != nil {
		t.Fatalf("查找 h264_nvenc 失败: %v", err)
	}
	h265, err := findEncoderByName("hevc_nvenc")
	if err != nil {
		t.Fatalf("查找 hevc_nvenc 失败: %v", err)
	}

	bothFound := h264 != nil && h265 != nil
	if nvidia.Available != bothFound {
		t.Errorf("NVIDIA 可用性判定不一致: Available=%v, bothCodecsFound=%v", nvidia.Available, bothFound)
	}
}

// TestDetectAllHardwareAccels 验证统一检测接口返回所有硬件加速信息。
func TestDetectAllHardwareAccels(t *testing.T) {
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
	if accels[0].Name != "NVIDIA NVENC" {
		t.Errorf("期望名称为 NVIDIA NVENC, 实际 %s", accels[0].Name)
	}

	// 验证第二项是 Intel QSV
	if accels[1].DeviceType != "qsv" {
		t.Errorf("期望第二个为 qsv, 实际 %s", accels[1].DeviceType)
	}

	for _, a := range accels {
		t.Logf("硬件加速: name=%s, type=%s, available=%v, h264=%s, h265=%s",
			a.Name, a.DeviceType, a.Available, a.H264Encoder, a.H265Encoder)
	}
}

// TestFirstAvailable 验证优先选择第一个可用的硬件加速器。
func TestFirstAvailable(t *testing.T) {
	tests := []struct {
		name     string
		accels   []HwAccelInfo
		expected *HwAccelInfo
	}{
		{
			name: "第一个可用",
			accels: []HwAccelInfo{
				{Name: "NVIDIA NVENC", DeviceType: "cuda", Available: true},
				{Name: "Intel QSV", DeviceType: "qsv", Available: true},
			},
			expected: &HwAccelInfo{Name: "NVIDIA NVENC", DeviceType: "cuda", Available: true},
		},
		{
			name: "第一个不可用第二个可用",
			accels: []HwAccelInfo{
				{Name: "NVIDIA NVENC", DeviceType: "cuda", Available: false},
				{Name: "Intel QSV", DeviceType: "qsv", Available: true},
			},
			expected: &HwAccelInfo{Name: "Intel QSV", DeviceType: "qsv", Available: true},
		},
		{
			name: "全部不可用",
			accels: []HwAccelInfo{
				{Name: "NVIDIA NVENC", DeviceType: "cuda", Available: false},
				{Name: "Intel QSV", DeviceType: "qsv", Available: false},
			},
			expected: nil,
		},
		{
			name:     "空列表",
			accels:   []HwAccelInfo{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FirstAvailable(tt.accels)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("期望 nil, 实际 %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("期望 %+v, 实际 nil", tt.expected)
			}
			if result.Name != tt.expected.Name {
				t.Errorf("期望 Name=%s, 实际 %s", tt.expected.Name, result.Name)
			}
		})
	}
}
