package transcoder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifySliceOutputs_ErrorIncludesOutputDir 验证 m3u8 缺失时错误包含输出目录上下文。
// 修复前：错误仅「档位 X 的 m3u8 未生成」，不含输出目录，难定位是哪个媒体的哪个目录（红）。
func TestVerifySliceOutputs_ErrorIncludesOutputDir(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "media_42")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}

	err := verifySliceOutputs(outputDir, []string{"480p"})
	if err == nil {
		t.Fatal("目录内无 m3u8，应返回错误")
	}
	if !strings.Contains(err.Error(), outputDir) {
		t.Fatalf("错误应包含输出目录 %q，实际: %q", outputDir, err.Error())
	}
	if !strings.Contains(err.Error(), "480p") {
		t.Fatalf("错误应包含档位名，实际: %q", err.Error())
	}
}

// TestProbeResolution_ErrorIncludesFFmpegOutput 用真实 ffmpeg 探测不存在的输入，
// 验证解析失败时错误携带 ffmpeg 输出尾部，而非仅一句「无法解析分辨率」。
// 修复前：out 被丢弃，错误不含 ffmpeg 输出，无法区分「文件损坏」与「正则不匹配」（红）。
// 环境无 ffmpeg 时跳过。
func TestProbeResolution_ErrorIncludesFFmpegOutput(t *testing.T) {
	ffmpegPath := ffmpegPathFromEnvOrPath(t)
	if ffmpegPath == "" {
		t.Skip("环境无 ffmpeg，跳过分辨率探测错误上下文测试")
	}
	SetFFmpegPath(ffmpegPath)
	t.Cleanup(func() { SetFFmpegPath("") })

	_, _, err := probeResolution(context.Background(), "/nonexistent/__no_such_input__.mp4")
	if err == nil {
		t.Fatal("对不存在的输入，探测应失败")
	}
	if !strings.Contains(err.Error(), "ffmpeg 输出:") {
		t.Fatalf("错误应携带 ffmpeg 输出段，实际: %q", err.Error())
	}
	idx := strings.Index(err.Error(), "ffmpeg 输出:")
	if idx < 0 {
		t.Fatalf("错误应携带 ffmpeg 输出段，实际: %q", err.Error())
	}
	outPart := err.Error()[idx:]
	if strings.TrimSpace(strings.TrimPrefix(outPart, "ffmpeg 输出:")) == "" {
		t.Fatalf("ffmpeg 输出段为空，未保留具体原因，实际: %q", err.Error())
	}
}
