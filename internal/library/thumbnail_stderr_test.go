package library

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"testing"
)

// TestGenerateThumbnail_LogsFFmpegStderr 验证缩略图 ffmpeg 失败时，
// 日志按 ERROR 级别记录文件路径 + stderr 关键内容。
// 修复前：仅 log.Printf WARN 记录 err（即 exit status，无具体原因）→ 日志不含 stderr 内容（红）。
func TestGenerateThumbnail_LogsFFmpegStderr(t *testing.T) {
	const stderrMarker = "Invalid data found when processing input"

	// 注入失败桩：返回带 stderr 关键内容的错误（模拟 realRunFFmpegThumbnail 的输出契约）
	orig := runFFmpegThumbnail
	runFFmpegThumbnail = func(ctx context.Context, args []string) error {
		return errWithStderr(stderrMarker)
	}
	t.Cleanup(func() { runFFmpegThumbnail = orig })

	cases := []struct {
		name string
		run  func(string)
	}{
		{"图片缩略图", func(p string) { generateImageThumbnail(p, thumbnailWidth) }},
		{"视频缩略图", func(p string) { generateVideoThumbnail(p, thumbnailWidth) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			restore := captureLog(&buf)
			defer restore()

			c.run("/fake/path/broken.mp4")

			out := buf.String()
			if !strings.Contains(out, stderrMarker) {
				t.Fatalf("失败日志应包含 ffmpeg stderr 关键内容 %q，实际日志: %q", stderrMarker, out)
			}
			if !strings.Contains(out, "[ERROR]") {
				t.Fatalf("缩略图生成失败应按 ERROR 级别记录，实际日志: %q", out)
			}
			if !strings.Contains(out, "/fake/path/broken.mp4") {
				t.Fatalf("失败日志应包含文件路径，实际日志: %q", out)
			}
		})
	}
}

// TestTailString 验证 stderr 尾部截断：短串原样返回、长串只保留末尾 n 字符、首尾空白被裁剪。
func TestTailString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"短串原样", "abc", 10, "abc"},
		{"恰好等长", "abcde", 5, "abcde"},
		{"超长取尾部", "0123456789", 4, "6789"},
		{"裁剪首尾空白", "  hello  ", 10, "hello"},
		{"空串", "   ", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tailString(tt.in, tt.n); got != tt.want {
				t.Fatalf("tailString(%q, %d) = %q, 期望 %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

// TestRealRunFFmpegThumbnail_CapturesStderr 用真实 ffmpeg 喂入不存在的输入文件，
// 验证 realRunFFmpegThumbnail 失败时返回的错误确实携带 ffmpeg stderr，而非仅一句 exit status。
// 修复前 cmd.Run() 不捕获 stderr，错误只含 exit status（不含「No such file」等原因）→ 红。
// 环境无 ffmpeg 时跳过。
func TestRealRunFFmpegThumbnail_CapturesStderr(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("环境无 ffmpeg，跳过 stderr 捕获端到端测试")
	}

	// 不存在的输入文件，ffmpeg 必然失败并向 stderr 写明原因
	args := []string{"-i", "/nonexistent/__no_such_input__.mp4", "-vframes", "1", "-y", t.TempDir() + "/out.jpg"}
	err := realRunFFmpegThumbnail(context.Background(), args)
	if err == nil {
		t.Fatal("对不存在的输入，ffmpeg 应失败返回错误")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ffmpeg stderr:") {
		t.Fatalf("错误应携带 ffmpeg stderr 段，实际: %q", msg)
	}
	// stderr 段除「exit status」外应含具体原因文本（ffmpeg 对找不到文件的报错）
	stderrPart := msg[strings.Index(msg, "ffmpeg stderr:"):]
	if strings.TrimSpace(strings.TrimPrefix(stderrPart, "ffmpeg stderr:")) == "" {
		t.Fatalf("ffmpeg stderr 段为空，未捕获到具体原因，实际: %q", msg)
	}
}

// errWithStderr 构造一个携带 stderr 文本的错误，模拟 realRunFFmpegThumbnail 的错误契约。
func errWithStderr(stderr string) error {
	return &stderrErr{stderr: stderr}
}

type stderrErr struct{ stderr string }

func (e *stderrErr) Error() string {
	return "exit status 1; ffmpeg stderr: " + e.stderr
}

// captureLog 临时把标准 logger 输出重定向到 buf，返回恢复函数。
func captureLog(buf *bytes.Buffer) func() {
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	return func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	}
}
