package transcoder

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipelineBuildArgs_NoSeek_NoHwAccel(t *testing.T) {
	p := Pipeline{encoderName: "libx264", deviceType: "", hwAccel: ""}
	args := p.buildArgs("/tmp/test.mp4", 0)

	assert.Contains(t, args, "-hide_banner")
	assert.Contains(t, args, "-loglevel")
	assert.Contains(t, args, "warning")
	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "/tmp/test.mp4")
	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "libx264")
	// 强制 8-bit yuv420p，避免 10-bit 源编出浏览器/mpegts.js 无法播放的 High 10
	assert.Contains(t, args, "-pix_fmt")
	assert.Contains(t, args, "yuv420p")
	assert.Greater(t, indexOf(args, "-pix_fmt"), indexOf(args, "-i"), "-pix_fmt 应作用于输出（位于 -i 之后）")
	assert.Contains(t, args, "-g")
	assert.Contains(t, args, "48")
	assert.Contains(t, args, "-keyint_min")
	assert.Contains(t, args, "-sc_threshold")
	assert.Contains(t, args, "0")
	assert.Contains(t, args, "-c:a")
	assert.Contains(t, args, "copy")
	assert.Contains(t, args, "-f")
	assert.Contains(t, args, "mpegts")
	assert.Contains(t, args, "-")
	assert.NotContains(t, args, "-ss")
	assert.NotContains(t, args, "-hwaccel")
}

func TestPipelineBuildArgs_WithSeek(t *testing.T) {
	p := Pipeline{encoderName: "libx264", deviceType: "", hwAccel: ""}
	args := p.buildArgs("/tmp/test.mp4", 10.5)

	assert.Contains(t, args, "-ss")
	assert.Contains(t, args, "10.50")

	// 验证 "-ss" 在 "-i" 之前
	ssIdx := indexOf(args, "-ss")
	iIdx := indexOf(args, "-i")
	assert.True(t, ssIdx < iIdx, "-ss 应该出现在 -i 之前")
}

func TestPipelineBuildArgs_WithHwAccel(t *testing.T) {
	p := Pipeline{encoderName: "h264_nvenc", deviceType: "cuda", hwAccel: "cuda"}
	args := p.buildArgs("/tmp/test.mp4", 0)

	assert.Contains(t, args, "-hwaccel")
	assert.Contains(t, args, "cuda")

	// 验证 "-hwaccel" 在 "-i" 之前
	hwIdx := indexOf(args, "-hwaccel")
	iIdx := indexOf(args, "-i")
	assert.True(t, hwIdx < iIdx, "-hwaccel 应该出现在 -i 之前")

	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "h264_nvenc")
}

func TestPipelineBuildArgs_WithSeekAndHwAccel(t *testing.T) {
	p := Pipeline{encoderName: "h264_qsv", deviceType: "qsv", hwAccel: "qsv"}
	args := p.buildArgs("/tmp/test.mp4", 5.0)

	// 验证顺序：-ss → -hwaccel → -i
	ssIdx := indexOf(args, "-ss")
	hwIdx := indexOf(args, "-hwaccel")
	iIdx := indexOf(args, "-i")
	assert.True(t, ssIdx < hwIdx, "-ss 应该出现在 -hwaccel 之前")
	assert.True(t, hwIdx < iIdx, "-hwaccel 应该出现在 -i 之前")
}

func TestPipelineBuildArgs_SeekZero(t *testing.T) {
	p := Pipeline{encoderName: "libx264", deviceType: "", hwAccel: ""}
	args := p.buildArgs("/tmp/test.mp4", 0)

	assert.NotContains(t, args, "-ss", "seekPosition=0 时不应生成 -ss 参数")
}

func TestNewPipeline(t *testing.T) {
	p := NewPipeline()
	assert.NotNil(t, p)
	assert.Equal(t, "libx264", p.encoderName)
}

func TestConfiguredFFmpegPathSharedByAllExecutionPaths(t *testing.T) {
	binaryPath, recordPath := buildFFmpegPathRecorder(t)
	oldPath := GetFFmpegPath()
	SetFFmpegPath(binaryPath)
	t.Cleanup(func() { SetFFmpegPath(oldPath) })
	t.Setenv("JIANVIDEO_FFMPEG_RECORD", recordPath)
	ctx := context.Background()

	requireNoError(t, newPipelineForEncoder("h264", "libx264", "").Run(ctx, "普通 TS 输入.mp4", io.Discard))
	if ok, detail := runProbe(ctx, []string{"probe-marker"}); !ok {
		t.Fatalf("探测路径执行失败: %s", detail)
	}
	requireNoError(t, NewMultiPipeline(newPipelineForEncoder("h264", "libx264", "")).runFFmpeg(ctx, []string{"multi-marker"}, ""))
	requireNoError(t, runFMP4FFmpeg(ctx, []string{"fmp4-marker"}, ""))

	raw, err := os.ReadFile(recordPath)
	requireNoError(t, err)
	records := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(records) != 4 {
		t.Fatalf("四条执行路径应使用同一配置二进制，实际记录 %d 条: %q", len(records), string(raw))
	}
	for _, marker := range []string{"普通 TS 输入.mp4", "probe-marker", "multi-marker", "fmp4-marker"} {
		if !strings.Contains(string(raw), marker) {
			t.Fatalf("配置路径执行记录缺少 %q: %q", marker, string(raw))
		}
	}
}

func buildFFmpegPathRecorder(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "带空格的 ffmpeg 目录")
	requireNoError(t, os.MkdirAll(dir, 0o750))
	sourcePath := filepath.Join(dir, "main.go")
	binaryPath := filepath.Join(dir, "fake ffmpeg.exe")
	recordPath := filepath.Join(dir, "执行记录.txt")
	source := `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	path := os.Getenv("JIANVIDEO_FFMPEG_RECORD")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	fmt.Fprintln(file, strings.Join(os.Args[1:], "\\x1f"))
}
`
	requireNoError(t, os.WriteFile(sourcePath, []byte(source), 0o600))
	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构建带空格路径的 ffmpeg 记录器失败: %v, 输出: %s", err, output)
	}
	return binaryPath, recordPath
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// indexOf 返回切片中指定元素的索引，不存在时返回 -1。
func indexOf(slice []string, target string) int {
	for i, v := range slice {
		if strings.TrimSpace(v) == target {
			return i
		}
	}
	return -1
}
