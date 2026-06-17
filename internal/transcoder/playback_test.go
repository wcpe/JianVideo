package transcoder

import (
	"testing"
)

func TestDecidePlayback_Direct(t *testing.T) {
	// H.264+AAC 的 MP4 → 直出
	info := DecidePlayback("movie.mp4", "mp4", "h264", "aac")
	if info.Format != "direct" {
		t.Errorf("期望 format=direct, 实际 %s", info.Format)
	}
	if info.TranscodeRequired {
		t.Error("期望 transcode_required=false")
	}
	if info.URL != "movie.mp4" {
		t.Errorf("期望 url=movie.mp4, 实际 %s", info.URL)
	}
	if info.HWAccel != "" {
		t.Errorf("期望 hw_accel 为空, 实际 %s", info.HWAccel)
	}
}

func TestDecidePlayback_TranscodeH265MKV(t *testing.T) {
	// H.265+MKV → 转码
	info := DecidePlayback("movie.mkv", "mkv", "hevc", "aac")
	if info.Format != "hls" {
		t.Errorf("期望 format=hls, 实际 %s", info.Format)
	}
	if !info.TranscodeRequired {
		t.Error("期望 transcode_required=true")
	}
	if info.HWAccel != "software" {
		t.Errorf("期望 hw_accel=software, 实际 %s", info.HWAccel)
	}
}

func TestDecidePlayback_TranscodeH264MKV(t *testing.T) {
	// H.264+AAC 但容器是 MKV → 转码（容器不符合）
	info := DecidePlayback("movie.mkv", "mkv", "h264", "aac")
	if info.Format != "hls" {
		t.Errorf("期望 format=hls, 实际 %s", info.Format)
	}
	if !info.TranscodeRequired {
		t.Error("期望 transcode_required=true")
	}
}

func TestDecidePlayback_UppercaseCodec(t *testing.T) {
	// 编码名大写也应正确识别为直出
	info := DecidePlayback("movie.mp4", "mp4", "H264", "AAC")
	if info.Format != "direct" {
		t.Errorf("期望 format=direct, 实际 %s", info.Format)
	}
}

func TestDecidePlayback_EmptyCodec(t *testing.T) {
	// 缺少编码信息 → 安全降级为转码
	info := DecidePlayback("movie.mp4", "mp4", "", "")
	if info.Format != "hls" {
		t.Errorf("期望 format=hls, 实际 %s", info.Format)
	}
	if !info.TranscodeRequired {
		t.Error("期望 transcode_required=true")
	}
}

func TestDecidePlayback_WebM(t *testing.T) {
	// WebM+VP9 → 转码
	info := DecidePlayback("movie.webm", "webm", "vp9", "opus")
	if info.Format != "hls" {
		t.Errorf("期望 format=hls, 实际 %s", info.Format)
	}
}
