package transcoder

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProbeMetadata_Full 测试 ffprobe 完整探测（需要系统安装 ffprobe）。
// 若 ffprobe 不可用，跳过测试。
func TestProbeMetadata_Full(t *testing.T) {
	if !isFFprobeAvailable() {
		t.Skip("ffprobe 不可用，跳过集成测试")
	}

	// 创建一个最小的有效 MP4 文件（用 ffmpeg 生成）
	tmp := t.TempDir()
	videoPath := filepath.Join(tmp, "test_video.mp4")

	// 尝试用 ffmpeg 生成一个 1 秒测试视频
	if !isFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过生成测试视频")
	}
	if err := generateTestVideo(videoPath); err != nil {
		t.Fatalf("生成测试视频失败: %v", err)
	}

	info, err := ProbeMetadata(videoPath)
	if err != nil {
		t.Fatalf("探测失败: %v", err)
	}

	if info.ContainerFormat == "" {
		t.Error("容器格式不应为空")
	}
	if info.VideoCodec == "" {
		t.Error("视频编码不应为空")
	}
	if info.Duration <= 0 {
		t.Error("时长应大于 0")
	}
	if info.Width <= 0 || info.Height <= 0 {
		t.Errorf("分辨率应大于 0，实际 %dx%d", info.Width, info.Height)
	}
}

// TestProbeMetadata_FileNotFound 测试文件不存在时的错误处理。
func TestProbeMetadata_FileNotFound(t *testing.T) {
	_, err := ProbeMetadata("/nonexistent/path/video.mp4")
	if err == nil {
		t.Fatal("文件不存在时应返回错误")
	}
}

// TestIsBrowserCompatible 测试浏览器兼容性判断。
func TestIsBrowserCompatible(t *testing.T) {
	tests := []struct {
		name     string
		info     *MediaInfo
		expected bool
	}{
		{
			name: "H.264+AAC MP4 应兼容",
			info: &MediaInfo{
				ContainerFormat: "mov,mp4,m4a,3gp,3g2,mj2",
				VideoCodec:      "h264",
				AudioCodec:      "aac",
			},
			expected: true,
		},
		{
			name: "H.265+AAC MP4 不兼容",
			info: &MediaInfo{
				ContainerFormat: "mov,mp4,m4a,3gp,3g2,mj2",
				VideoCodec:      "hevc",
				AudioCodec:      "aac",
			},
			expected: false,
		},
		{
			name: "H.264+AAC MKV 不兼容",
			info: &MediaInfo{
				ContainerFormat: "matroska,webm",
				VideoCodec:      "h264",
				AudioCodec:      "aac",
			},
			expected: false,
		},
		{
			name: "H.264+AC3 MP4 不兼容",
			info: &MediaInfo{
				ContainerFormat: "mov,mp4,m4a,3gp,3g2,mj2",
				VideoCodec:      "h264",
				AudioCodec:      "ac3",
			},
			expected: false,
		},
		{
			name: "VP9+Opus WebM 不兼容",
			info: &MediaInfo{
				ContainerFormat: "matroska,webm",
				VideoCodec:      "vp9",
				AudioCodec:      "opus",
			},
			expected: false,
		},
		{
			name: "AV1+AAC MKV 不兼容",
			info: &MediaInfo{
				ContainerFormat: "matroska,webm",
				VideoCodec:      "av1",
				AudioCodec:      "aac",
			},
			expected: false,
		},
		{
			name: "空编码信息不兼容",
			info: &MediaInfo{
				ContainerFormat: "mov,mp4,m4a,3gp,3g2,mj2",
				VideoCodec:      "",
				AudioCodec:      "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBrowserCompatible(tt.info)
			if result != tt.expected {
				t.Errorf("期望 %v, 实际 %v", tt.expected, result)
			}
		})
	}
}

// TestContainerFormat 测试容器格式提取。
func TestContainerFormat(t *testing.T) {
	// ffprobe format_name 可能返回多个逗号分隔的格式
	// 我们取第一个作为主容器
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"MP4", "mov,mp4,m4a,3gp,3g2,mj2", "mov"},
		{"MKV", "matroska,webm", "matroska"},
		{"AVI", "avi", "avi"},
		{"WebM", "matroska,webm", "matroska"},
		{"TS", "mpegts", "mpegts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPrimaryFormat(tt.input)
			if result != tt.expected {
				t.Errorf("期望 %q, 实际 %q", tt.expected, result)
			}
		})
	}
}

// TestIsFFprobeAvailable 测试 ffprobe 可用性检测。
func TestIsFFprobeAvailable(t *testing.T) {
	available := isFFprobeAvailable()
	t.Logf("ffprobe 可用: %v", available)
	// 不断言结果，仅记录信息
}

// TestExtractSubtitlePath 测试字幕路径匹配。
func TestExtractSubtitlePath(t *testing.T) {
	// 创建临时目录和文件
	tmp := t.TempDir()
	videoPath := filepath.Join(tmp, "movie.mkv")

	// 创建视频文件占位
	_ = os.WriteFile(videoPath, []byte("fake"), 0o644)

	// 创建字幕文件
	srtPath := filepath.Join(tmp, "movie.srt")
	assPath := filepath.Join(tmp, "movie.ass")
	supPath := filepath.Join(tmp, "movie.sup")
	_ = os.WriteFile(srtPath, []byte("1\n00:00:01,000 --> 00:00:02,000\n测试\n"), 0o644)
	_ = os.WriteFile(assPath, []byte("[Script Info]\nTitle: test\n[V4+ Styles]\nFormat:..."), 0o644)
	_ = os.WriteFile(supPath, []byte("fake"), 0o644)

	subs, err := FindSubtitleFiles(videoPath)
	if err != nil {
		t.Fatalf("查找字幕失败: %v", err)
	}

	if len(subs) != 2 {
		t.Fatalf("期望找到 2 个文本字幕文件, 实际 %d", len(subs))
	}

	// 验证每个格式都被识别
	formatMap := make(map[string]string)
	for _, sub := range subs {
		formatMap[sub.Format] = sub.Path
	}

	if _, ok := formatMap["srt"]; !ok {
		t.Error("未找到 SRT 字幕")
	}
	if _, ok := formatMap["ass"]; !ok {
		t.Error("未找到 ASS 字幕")
	}
	if _, ok := formatMap["sup"]; ok {
		t.Error("图片字幕不应作为可转换外挂字幕返回")
	}
}
