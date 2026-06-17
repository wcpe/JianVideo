package transcoder

import (
	"testing"
)

// TestDetectQSV_IntelVendorCheck 验证 Intel 核显 vendor ID 检测逻辑。
func TestDetectQSV_IntelVendorCheck(t *testing.T) {
	ok, err := isIntelGPU()
	if err != nil {
		t.Fatalf("检测 Intel GPU 时出错: %v", err)
	}
	// 在 CI/无核显环境中结果可能为 false，但不应该报错。
	t.Logf("Intel GPU 检测: isIntel=%v", ok)
}

// TestDetectQSV_H264Encoder 验证 H.264 QSV 编码器查找。
func TestDetectQSV_H264Encoder(t *testing.T) {
	// 需要 CGO 环境，在无 CGO 或没有 QSV 的环境中应返回 false。
	enc, err := findQSVEncoder("h264_qsv")
	if err != nil {
		t.Fatalf("查找 H.264 QSV 编码器时出错: %v", err)
	}
	t.Logf("H.264 QSV 编码器: found=%v", enc != nil)
}

// TestDetectQSV_H265Encoder 验证 H.265 QSV 编码器查找。
func TestDetectQSV_H265Encoder(t *testing.T) {
	enc, err := findQSVEncoder("hevc_qsv")
	if err != nil {
		t.Fatalf("查找 H.265 QSV 编码器时出错: %v", err)
	}
	t.Logf("H.265 QSV 编码器: found=%v", enc != nil)
}

// TestQSVAvailable 验证 QSV 整体可用性判断（需要 H.264 + H.265 同时可用）。
func TestQSVAvailable(t *testing.T) {
	available, err := QSVAvailable()
	if err != nil {
		t.Fatalf("QSV 可用性检测出错: %v", err)
	}
	t.Logf("QSV 可用: %v", available)
}

// TestDetectQSV_RequiresBothCodecs 验证 H.264 和 H.265 必须同时支持才算可用。
func TestDetectQSV_RequiresBothCodecs(t *testing.T) {
	available, err := QSVAvailable()
	if err != nil {
		t.Fatalf("QSV 可用性检测出错: %v", err)
	}
	// 当且仅当 H.264 和 H.265 编码器都找到时，QSV 才应标记为可用。
	h264, err := findQSVEncoder("h264_qsv")
	if err != nil {
		t.Fatalf("查找 H.264 QSV 编码器时出错: %v", err)
	}
	h265, err := findQSVEncoder("hevc_qsv")
	if err != nil {
		t.Fatalf("查找 H.265 QSV 编码器时出错: %v", err)
	}

	bothFound := h264 != nil && h265 != nil
	if available != bothFound {
		t.Errorf("QSV 可用性判定不一致: available=%v, bothCodecsFound=%v", available, bothFound)
	}
}
