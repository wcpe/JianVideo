package transcoder

import (
	"reflect"
	"testing"
)

// TestCodecOutputParams_KnownCodecs 验证四种目标编码各自的输出参数。
func TestCodecOutputParams_KnownCodecs(t *testing.T) {
	cases := []struct {
		codec      string
		wantPixFmt string
		wantExtra  []string
	}{
		// h264：与改动前现状一致，不附加额外参数（保证默认行为字节级不变）
		{"h264", "yuv420p", nil},
		// h265：附 hvc1 标记，便于后续容器封装
		{"h265", "yuv420p", []string{"-tag:v", "hvc1"}},
		{"av1", "yuv420p", nil},
		{"vp9", "yuv420p", nil},
	}
	for _, c := range cases {
		t.Run(c.codec, func(t *testing.T) {
			params, ok := CodecOutputParams(c.codec)
			if !ok {
				t.Fatalf("CodecOutputParams(%q) ok=false，期望已知编码", c.codec)
			}
			if params.PixFmt != c.wantPixFmt {
				t.Errorf("PixFmt = %q，期望 %q", params.PixFmt, c.wantPixFmt)
			}
			if !reflect.DeepEqual(params.ExtraArgs, c.wantExtra) {
				t.Errorf("ExtraArgs = %v，期望 %v", params.ExtraArgs, c.wantExtra)
			}
		})
	}
}

// TestCodecOutputParams_Unknown 未知编码返回 ok=false。
func TestCodecOutputParams_Unknown(t *testing.T) {
	if _, ok := CodecOutputParams("mpeg2"); ok {
		t.Error("未知编码 mpeg2 应返回 ok=false")
	}
	if _, ok := CodecOutputParams(""); ok {
		t.Error("空编码应返回 ok=false")
	}
}

// TestCodecOutputParams_H264NoExtra h264 不得引入任何额外参数（默认行为不变红线）。
func TestCodecOutputParams_H264NoExtra(t *testing.T) {
	params, _ := CodecOutputParams("h264")
	if len(params.ExtraArgs) != 0 {
		t.Errorf("h264 ExtraArgs 应为空，实际 %v", params.ExtraArgs)
	}
}
