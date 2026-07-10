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

// TestCandidateEncoders_FullFamilies 验证候选清单已补齐全硬件家族及 AV1/VP9。
func TestCandidateEncoders_FullFamilies(t *testing.T) {
	cands := candidateEncoders()
	byEncoder := make(map[string]EncoderCandidate, len(cands))
	for _, c := range cands {
		byEncoder[c.Encoder] = c
	}

	// 必须涵盖各家族的 AV1/VP9 等扩展编码器
	wantEncoders := []string{
		"libsvtav1", "libvpx-vp9", // 软件 AV1/VP9
		"av1_qsv", "vp9_qsv", // QSV AV1/VP9
		"av1_nvenc",              // NVENC AV1
		"av1_amf",                // AMD AMF AV1
		"av1_vaapi", "vp9_vaapi", // VAAPI AV1/VP9
		"h264_vulkan", "hevc_vulkan", "av1_vulkan", // Vulkan 全家族
	}
	for _, enc := range wantEncoders {
		if _, ok := byEncoder[enc]; !ok {
			t.Errorf("候选清单应包含编码器 %q，但缺失", enc)
		}
	}

	// hevc 编码器的 codec 字段统一记为 "h265"（与现有约定一致）
	if c, ok := byEncoder["hevc_amf"]; ok && c.Codec != "h265" {
		t.Errorf("hevc_amf 的 codec 应为 h265，实际 %q", c.Codec)
	}
	// av1 编码器的 codec 字段应为 "av1"
	if c, ok := byEncoder["av1_amf"]; ok && c.Codec != "av1" {
		t.Errorf("av1_amf 的 codec 应为 av1，实际 %q", c.Codec)
	}
	// vp9 编码器的 codec 字段应为 "vp9"
	if c, ok := byEncoder["libvpx-vp9"]; ok && c.Codec != "vp9" {
		t.Errorf("libvpx-vp9 的 codec 应为 vp9，实际 %q", c.Codec)
	}
	// av1_vulkan 应归属 vulkan 家族
	if c, ok := byEncoder["av1_vulkan"]; ok && c.Family != familyVulkan {
		t.Errorf("av1_vulkan 应归属 vulkan 家族，实际 %q", c.Family)
	}
}

// TestBuildProbeArgs_Vulkan 验证 Vulkan 分支带设备初始化与 hwupload。
func TestBuildProbeArgs_Vulkan(t *testing.T) {
	args := buildProbeArgs(EncoderCandidate{Encoder: "h264_vulkan", Family: familyVulkan, Codec: "h264"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-init_hw_device vulkan=vk:0") {
		t.Errorf("Vulkan 应初始化与生产一致的硬件设备，实际: %s", joined)
	}
	if !strings.Contains(joined, "-filter_hw_device vk") {
		t.Errorf("Vulkan 应指定 filter_hw_device，实际: %s", joined)
	}
	if !strings.Contains(joined, "-vf format=nv12,hwupload") {
		t.Errorf("Vulkan 应包含与生产一致的 hwupload 滤镜，实际: %s", joined)
	}
	if !strings.Contains(joined, "-c:v h264_vulkan") {
		t.Errorf("应包含 Vulkan 编码器名，实际: %s", joined)
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
