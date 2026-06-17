package transcoder

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"jianvideo/internal/hwaccel"
)

// Pipeline 封装一次 ffmpeg 转码会话。
type Pipeline struct {
	encoder *hwaccel.Info
}

// NewPipeline 创建转码管道，自动选择最佳编码器。
func NewPipeline() *Pipeline {
	encoder := hwaccel.SelectBestEncoder("h264")
	return &Pipeline{encoder: encoder}
}

// Run 启动转码管道，将 ffmpeg stdout 写入 dst。
// ctx 取消时自动终止 ffmpeg 进程。
func (p *Pipeline) Run(ctx context.Context, inputPath string, dst io.Writer) error {
	args := p.buildArgs(inputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = dst
	cmd.Stderr = &logWriter{prefix: "[ffmpeg]"}

	setProcessGroup(cmd)

	log.Printf("[INFO] 启动转码: ffmpeg %s", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 失败: %w", err)
	}

	// 等待 ffmpeg 结束或 context 取消
	err := cmd.Wait()
	if ctx.Err() != nil {
		// context 被取消，尝试杀进程
		_ = killProcessGroup(cmd)
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg 转码失败: %w", err)
	}

	log.Printf("[INFO] 转码完成: %s", inputPath)
	return nil
}

// buildArgs 构建 ffmpeg 命令行参数。
func (p *Pipeline) buildArgs(inputPath string) []string {
	enc := p.encoder
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// 硬件加速设备初始化
	if enc.HWAccel != "" {
		args = append(args, "-hwaccel", enc.HWAccel)
	}

	args = append(args,
		"-i", inputPath,
		"-c:v", enc.Name,
		// 固定 GOP = 48 帧
		"-g", "48",
		"-keyint_min", "48",
		"-sc_threshold", "0",
		// 音频直接复制（不重编码）
		"-c:a", "copy",
		// 强制输出 mpegts 裸流
		"-f", "mpegts",
		// 输出到 stdout
		"-",
	)
	return args
}

// Transcoder 接口抽象，便于未来替换为 CGO 实现。
type Transcoder interface {
	Run(ctx context.Context, inputPath string, dst io.Writer) error
}

// 确保 Pipeline 实现 Transcoder 接口。
var _ Transcoder = (*Pipeline)(nil)

// --- 内部工具 ---

// logWriter 将 ffmpeg stderr 重定向到 log。
type logWriter struct {
	prefix string
}

func (w *logWriter) Write(p []byte) (int, error) {
	lines := strings.Split(strings.TrimSpace(string(p)), "\n")
	for _, line := range lines {
		if line != "" {
			log.Printf("%s %s", w.prefix, line)
		}
	}
	return len(p), nil
}

// init 包初始化时记录启动时间（用于未来健康检查）。
func init() {
	_ = time.Now()
}
