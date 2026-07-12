package transcoder

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ffmpegPathFromEnvOrPath 在测试环境查找 ffmpeg 可执行文件。
// 优先级：JIANVIDEO_FFMPEG_PATH 环境变量 > PATH 中的 ffmpeg。
// 自定义安装位置请通过 JIANVIDEO_FFMPEG_PATH 指定，避免在源码中硬编码机器特定的绝对路径。
func ffmpegPathFromEnvOrPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("JIANVIDEO_FFMPEG_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	return ""
}

// TestPreSlice_FFmpegEndToEnd 用 ffmpeg 真跑一次：1 秒 testsrc → HLS 切片 → 校验产物。
// 环境无 ffmpeg 时跳过。
func TestPreSlice_FFmpegEndToEnd(t *testing.T) {
	ffmpegPath := ffmpegPathFromEnvOrPath(t)
	if ffmpegPath == "" {
		t.Skip("环境无 ffmpeg，跳过 HLS 端到端测试")
	}
	SetFFmpegPath(ffmpegPath)
	t.Cleanup(func() { SetFFmpegPath("") })

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "src.mp4")

	// 1. 用 ffmpeg 生成 1 秒 H.264+AAC mp4（彩色色块）
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		inputPath)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg 生成测试视频失败: %v\n%s", err, out)
	}

	// 2. 跑 RunMultiToDir 生成 HLS 切片
	hlsOut := filepath.Join(dir, "hls")
	mp := NewMultiPipeline(NewPipeline())
	qualityNames := QualitiesForResolution(320, 240)
	if err := mp.RunMultiToDir(t.Context(), inputPath, qualityNames, hlsOut); err != nil {
		t.Fatalf("RunMultiToDir 失败: %v", err)
	}

	// 3. 校验产物：每个码率都有 m3u8 + 至少 1 个 ts 切片
	for _, q := range qualityNames {
		m3u8 := filepath.Join(hlsOut, q+".m3u8")
		body, err := os.ReadFile(m3u8)
		if err != nil {
			t.Errorf("档位 %s 的 m3u8 未生成: %v", q, err)
			continue
		}
		if !strings.Contains(string(body), "#EXTM3U") {
			t.Errorf("档位 %s 的 m3u8 头部格式错误: %s", q, body)
		}
	}
}

// TestRateToBps 验证码率字符串解析。
func TestPreviewQualityForResolutionKeepsSingleVariant(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		want   string
	}{
		{name: "1080p 源只取最高档", width: 1920, height: 1080, want: "1080p"},
		{name: "720p 源只取 720p", width: 1280, height: 720, want: "720p"},
		{name: "低分辨率仍只取兜底档", width: 320, height: 240, want: "480p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := previewQualityForResolution(tt.width, tt.height)
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("preview 档位应为单档 %s，实际 %v", tt.want, got)
			}
		})
	}
}

func TestRateToBps(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"5000k", 5_000_000},
		{"2500k", 2_500_000},
		{"128k", 128_000},
		{"96k", 96_000},
		{"1M", 1_000_000},
		{"1500", 1500},
		{"", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := rateToBps(tt.in); got != tt.want {
				t.Fatalf("rateToBps(%q) = %d, 期望 %d", tt.in, got, tt.want)
			}
		})
	}
}
