package transcoder

import (
	"context"
	"strings"
	"testing"
)

func TestParseCompiledEncoders(t *testing.T) {
	out := `Encoders:
 V..... = Video
 A..... = Audio
 ------
 V....D libx264              libx264 H.264 / AVC
 V....D libx265              libx265 H.265 / HEVC
 V..... h264_qsv             H.264 / AVC (Intel Quick Sync Video)
 A....D aac                  AAC (Advanced Audio Coding)
`
	set := parseCompiledEncoders(out)

	for _, want := range []string{"libx264", "libx265", "h264_qsv", "aac"} {
		if !set[want] {
			t.Errorf("应解析出编码器 %q，但缺失", want)
		}
	}
	// 分隔线之前的说明行（如 "Video"）不应被当作编码器
	if set["Video"] || set["="] {
		t.Errorf("分隔线之前的内容不应被解析为编码器: %+v", set)
	}
	// 未出现的编码器不应存在
	if set["hevc_nvenc"] {
		t.Errorf("未出现的编码器不应被解析出")
	}
}

func TestBuildProbeArgs_Generic(t *testing.T) {
	args := buildProbeArgs(EncoderCandidate{Encoder: "h264_nvenc", Family: familyNVENC, Codec: "h264"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v h264_nvenc") {
		t.Errorf("通用试编码参数应包含编码器名，实际: %s", joined)
	}
	if !strings.Contains(joined, "-f null -") {
		t.Errorf("应输出到 null，实际: %s", joined)
	}
	// 通用模板不应包含 VAAPI 特有的 hwupload
	if strings.Contains(joined, "hwupload") {
		t.Errorf("非 VAAPI 不应包含 hwupload: %s", joined)
	}
}

func TestBuildProbeArgs_VAAPI(t *testing.T) {
	args := buildProbeArgs(EncoderCandidate{Encoder: "h264_vaapi", Family: familyVAAPI, Codec: "h264"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-init_hw_device") || !strings.Contains(joined, vaapiDevice) {
		t.Errorf("VAAPI 应初始化硬件设备，实际: %s", joined)
	}
	if !strings.Contains(joined, "format=nv12,hwupload") {
		t.Errorf("VAAPI 应包含 hwupload 滤镜，实际: %s", joined)
	}
	if !strings.Contains(joined, "-c:v h264_vaapi") {
		t.Errorf("应包含 VAAPI 编码器名，实际: %s", joined)
	}
}

func TestTailString(t *testing.T) {
	if got := tailString("  abc  ", 10); got != "abc" {
		t.Errorf("应去空白返回 abc，实际 %q", got)
	}
	if got := tailString("abcdef", 3); got != "def" {
		t.Errorf("应返回末尾 3 字符 def，实际 %q", got)
	}
}

// TestProbeEncoders_Integration 在有 ffmpeg 的环境下验证软件编码器 libx264
// 被报告为「编入 + 试编码成功」；无 ffmpeg 时跳过。
func TestProbeEncoders_Integration(t *testing.T) {
	if !IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过试编码集成测试")
	}
	results := ProbeEncoders(context.Background())
	var found bool
	for _, r := range results {
		if r.Encoder == "libx264" {
			found = true
			if !r.Compiled {
				t.Errorf("libx264 应被报告为已编入 ffmpeg")
			}
			if !r.TestedOK {
				t.Errorf("libx264 软件编码试编码应成功，detail=%s", r.Detail)
			}
		}
	}
	if !found {
		t.Errorf("结果中应包含 libx264 候选")
	}
}
