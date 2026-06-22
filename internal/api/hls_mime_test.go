package api

import "testing"

// detectHLSMimeType 须识别 TS（原有）与 fMP4/CMAF（FR-51 新增）两类产物。
func TestDetectHLSMimeType(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		// 原有 TS 路径
		{"5/index.m3u8", "application/vnd.apple.mpegurl"},
		{"5/720p_segment_000.ts", "video/mp2t"},
		// FR-51 fMP4/CMAF 路径
		{"5/init.mp4", "video/mp4"},
		{"5/seg_000.m4s", "video/iso.segment"},
		// 兜底
		{"5/unknown.bin", "application/octet-stream"},
	}
	for _, c := range cases {
		if got := detectHLSMimeType(c.path); got != c.want {
			t.Errorf("detectHLSMimeType(%q) = %q, 期望 %q", c.path, got, c.want)
		}
	}
}
