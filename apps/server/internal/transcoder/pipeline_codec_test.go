package transcoder

import (
	"testing"
)

// argValueAfter 返回 args 中 flag 紧随其后的值；不存在返回空串。
func argValueAfter(args []string, flag string) string {
	for i, v := range args {
		if v == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// argHasPair 判断 args 中是否存在紧邻的 flag value 对。
func argHasPair(args []string, flag, value string) bool {
	for i, v := range args {
		if v == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// TestNewPipelineForCodec_DefaultH264 默认 h264 与 NewPipeline 等价（软件兜底 libx264）。
func TestNewPipelineForCodec_DefaultH264(t *testing.T) {
	setProbeSnapshot(nil)
	p := NewPipelineForCodec("h264")
	if p.codec != "h264" {
		t.Errorf("codec = %q，期望 h264", p.codec)
	}
	if p.encoderName != "libx264" {
		t.Errorf("encoderName = %q，期望 libx264（冷态软件兜底）", p.encoderName)
	}
}

// TestNewPipelineForCodec_AV1Software 无硬件快照时 av1 选软件 libsvtav1。
func TestNewPipelineForCodec_AV1Software(t *testing.T) {
	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "libsvtav1", Family: "software", Codec: "av1", Compiled: true, TestedOK: true},
	})
	defer setProbeSnapshot(nil)
	p := NewPipelineForCodec("av1")
	if p.codec != "av1" {
		t.Errorf("codec = %q，期望 av1", p.codec)
	}
	if p.encoderName != "libsvtav1" {
		t.Errorf("encoderName = %q，期望 libsvtav1", p.encoderName)
	}
}

// TestNewPipelineForCodec_AV1Hardware 有硬件 av1 编码器时优先选硬件。
func TestNewPipelineForCodec_AV1Hardware(t *testing.T) {
	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "libsvtav1", Family: "software", Codec: "av1", Compiled: true, TestedOK: true},
		{Encoder: "av1_nvenc", Family: "nvenc", Codec: "av1", Compiled: true, TestedOK: true},
	})
	defer setProbeSnapshot(nil)
	p := NewPipelineForCodec("av1")
	if p.encoderName != "av1_nvenc" {
		t.Errorf("encoderName = %q，期望 av1_nvenc（硬件优先）", p.encoderName)
	}
	if p.deviceType != "cuda" || p.hwAccel != "cuda" {
		t.Errorf("deviceType/hwAccel = %q/%q，期望 cuda/cuda", p.deviceType, p.hwAccel)
	}
}

func TestNewPipelineForCodecWithPolicy_UsesRequestedHardware(t *testing.T) {
	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "h264_nvenc", Family: "nvenc", Codec: "h264", TestedOK: true},
		{Encoder: "h264_qsv", Family: "qsv", Codec: "h264", TestedOK: true},
	})
	defer setProbeSnapshot(nil)

	p, err := NewPipelineForCodecWithPolicy("h264", HardwarePolicy{Mode: "qsv", Fallback: true})
	if err != nil {
		t.Fatalf("按策略创建管道失败: %v", err)
	}
	args := p.buildArgs("/tmp/a.mp4", 0)
	if argValueAfter(args, "-c:v") != "h264_qsv" {
		t.Errorf("-c:v = %q，期望 h264_qsv", argValueAfter(args, "-c:v"))
	}
	if !argHasPair(args, "-hwaccel", "qsv") {
		t.Errorf("指定 qsv 策略时应包含 -hwaccel qsv，args=%v", args)
	}
}

func TestNewPipelineForCodecWithPolicy_UnavailableNoFallback(t *testing.T) {
	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "h264_nvenc", Family: "nvenc", Codec: "h264", TestedOK: false},
	})
	defer setProbeSnapshot(nil)

	if _, err := NewPipelineForCodecWithPolicy("h264", HardwarePolicy{Mode: "nvenc", Fallback: false}); err == nil {
		t.Fatal("指定不可用硬件且关闭回退时应返回错误")
	}
}

// TestNewPipelineForCodec_UnknownFallback 未知/空编码回落默认 h264。
func TestNewPipelineForCodec_UnknownFallback(t *testing.T) {
	setProbeSnapshot(nil)
	for _, codec := range []string{"", "mpeg2"} {
		p := NewPipelineForCodec(codec)
		if p.codec != "h264" {
			t.Errorf("codec=%q 应回落 h264，实际 %q", codec, p.codec)
		}
	}
}

// TestBuildArgs_H264UnchangedDefault 默认 h264 管道 buildArgs 与现状一致：pix_fmt yuv420p、无 hvc1。
func TestBuildArgs_H264UnchangedDefault(t *testing.T) {
	p := Pipeline{encoderName: "libx264", codec: "h264"}
	args := p.buildArgs("/tmp/a.mp4", 0)
	if !argHasPair(args, "-pix_fmt", "yuv420p") {
		t.Error("h264 应含 -pix_fmt yuv420p")
	}
	if argValueAfter(args, "-c:v") != "libx264" {
		t.Errorf("-c:v = %q，期望 libx264", argValueAfter(args, "-c:v"))
	}
	if argHasPair(args, "-tag:v", "hvc1") {
		t.Error("h264 不应含 -tag:v hvc1")
	}
}

// TestBuildArgs_EmptyCodecBehavesAsH264 直接构造 Pipeline{} 时 codec 为空，buildArgs 按 h264 处理。
func TestBuildArgs_EmptyCodecBehavesAsH264(t *testing.T) {
	p := Pipeline{encoderName: "libx264"}
	args := p.buildArgs("/tmp/a.mp4", 0)
	if !argHasPair(args, "-pix_fmt", "yuv420p") {
		t.Error("空 codec 应回落 h264 的 -pix_fmt yuv420p")
	}
	if argHasPair(args, "-tag:v", "hvc1") {
		t.Error("空 codec 不应含 hvc1")
	}
}

// TestBuildArgs_H265AddsTag h265 管道 buildArgs 含 hvc1 标记与正确编码器。
func TestBuildArgs_H265AddsTag(t *testing.T) {
	p := Pipeline{encoderName: "libx265", codec: "h265"}
	args := p.buildArgs("/tmp/a.mp4", 0)
	if argValueAfter(args, "-c:v") != "libx265" {
		t.Errorf("-c:v = %q，期望 libx265", argValueAfter(args, "-c:v"))
	}
	if !argHasPair(args, "-pix_fmt", "yuv420p") {
		t.Error("h265 应含 -pix_fmt yuv420p")
	}
	if !argHasPair(args, "-tag:v", "hvc1") {
		t.Error("h265 应含 -tag:v hvc1")
	}
}

// TestBuildArgs_AV1Encoder av1 管道 buildArgs 用 av1 编码器与 pix_fmt。
func TestBuildArgs_AV1Encoder(t *testing.T) {
	p := Pipeline{encoderName: "libsvtav1", codec: "av1"}
	args := p.buildArgs("/tmp/a.mp4", 0)
	if argValueAfter(args, "-c:v") != "libsvtav1" {
		t.Errorf("-c:v = %q，期望 libsvtav1", argValueAfter(args, "-c:v"))
	}
	if !argHasPair(args, "-pix_fmt", "yuv420p") {
		t.Error("av1 应含 -pix_fmt yuv420p")
	}
}

// TestBuildMultiArgs_CodecParameterized 多码率管道按目标编码参数化编码器与 pix_fmt。
func TestBuildMultiArgs_CodecParameterized(t *testing.T) {
	p := &Pipeline{encoderName: "libsvtav1", codec: "av1"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/a.mp4", []string{"480p"})
	if argValueAfter(args, "-c:v") != "libsvtav1" {
		t.Errorf("多码率 -c:v = %q，期望 libsvtav1", argValueAfter(args, "-c:v"))
	}
	// 多码率走 filter_complex format=...，pix_fmt 体现在 scale 链中
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if !containsSub(joined, "format=yuv420p") {
		t.Error("多码率 filter 应含 format=yuv420p")
	}
}

// TestBuildMultiArgs_H264Unchanged 默认 h264 多码率参数与现状一致（编码器 + format）。
func TestBuildMultiArgs_H264Unchanged(t *testing.T) {
	p := &Pipeline{encoderName: "libx264", codec: "h264"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/a.mp4", []string{"480p"})
	if argValueAfter(args, "-c:v") != "libx264" {
		t.Errorf("多码率 -c:v = %q，期望 libx264", argValueAfter(args, "-c:v"))
	}
}

func TestBuildArgs_VAAPIAndVulkanUseUploadParameters(t *testing.T) {
	tests := []struct {
		name       string
		encoder    string
		deviceType string
		initValue  string
		filterName string
	}{
		{name: "VAAPI", encoder: "h264_vaapi", deviceType: "vaapi", initValue: "vaapi=va:" + vaapiDevice, filterName: "va"},
		{name: "Vulkan", encoder: "h264_vulkan", deviceType: "vulkan", initValue: "vulkan=vk:0", filterName: "vk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPipelineForEncoder("h264", tt.encoder, tt.deviceType)
			args := p.buildArgs("/tmp/a.mp4", 0)
			if !argHasPair(args, "-init_hw_device", tt.initValue) {
				t.Fatalf("生产管道应初始化硬件设备，args=%v", args)
			}
			if !argHasPair(args, "-filter_hw_device", tt.filterName) {
				t.Fatalf("生产管道应指定滤镜硬件设备，args=%v", args)
			}
			if !argHasPair(args, "-vf", "format=nv12,hwupload") {
				t.Fatalf("生产管道应上传硬件帧，args=%v", args)
			}
			if argValueAfter(args, "-c:v") != tt.encoder {
				t.Fatalf("生产管道编码器错误，args=%v", args)
			}
		})
	}
}

func TestBuildMultiArgs_VAAPIAndVulkanUseUploadParameters(t *testing.T) {
	for _, tt := range []struct {
		name       string
		encoder    string
		deviceType string
		initValue  string
		filterName string
	}{
		{name: "VAAPI", encoder: "h264_vaapi", deviceType: "vaapi", initValue: "vaapi=va:" + vaapiDevice, filterName: "va"},
		{name: "Vulkan", encoder: "h264_vulkan", deviceType: "vulkan", initValue: "vulkan=vk:0", filterName: "vk"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := NewMultiPipeline(newPipelineForEncoder("h264", tt.encoder, tt.deviceType)).BuildArgs("/tmp/a.mp4", []string{"480p"})
			joined := ""
			for _, arg := range args {
				joined += arg + " "
			}
			if !argHasPair(args, "-init_hw_device", tt.initValue) || !argHasPair(args, "-filter_hw_device", tt.filterName) {
				t.Fatalf("多码率生产管道缺少硬件设备参数，args=%v", args)
			}
			if !containsSub(joined, "format=nv12,hwupload") {
				t.Fatalf("多码率生产管道应上传硬件帧，args=%v", args)
			}
			if argValueAfter(args, "-c:v") != tt.encoder {
				t.Fatalf("多码率生产管道编码器错误，args=%v", args)
			}
		})
	}
}

func TestBuildArgs_AMFKeepsWindowsProductionPath(t *testing.T) {
	p := newPipelineForEncoder("h264", "h264_amf", "d3d11va")
	args := p.buildArgs("D:/media/input.mp4", 0)
	if !argHasPair(args, "-hwaccel", "d3d11va") {
		t.Fatalf("AMF 生产管道应保留 d3d11va 解码路径，args=%v", args)
	}
	if argValueAfter(args, "-c:v") != "h264_amf" {
		t.Fatalf("AMF 生产管道应使用 h264_amf，args=%v", args)
	}
	if argHasPair(args, "-vf", "format=nv12,hwupload") || argValueAfter(args, "-init_hw_device") != "" {
		t.Fatalf("AMF 不应误用 VAAPI/Vulkan 上传参数，args=%v", args)
	}
}

func TestBuildMultiArgs_AMFKeepsWindowsProductionPath(t *testing.T) {
	args := NewMultiPipeline(newPipelineForEncoder("h264", "h264_amf", "d3d11va")).BuildArgs("D:/media/input.mp4", []string{"480p"})
	if !argHasPair(args, "-hwaccel", "d3d11va") || argValueAfter(args, "-c:v") != "h264_amf" {
		t.Fatalf("AMF 多码率生产参数应保留真实 Windows 路径，args=%v", args)
	}
}

// containsSub 简单子串判断（避免引入 strings 仅为测试）。
func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
