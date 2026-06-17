package transcoder

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
)

// logWriter 将 ffmpeg  stderr 输出重定向到日志。
type logWriter struct {
	prefix string
}

func (w *logWriter) Write(p []byte) (int, error) {
	log.Printf("%s %s", w.prefix, string(p))
	return len(p), nil
}

// Pipeline 封装一次 ffmpeg 转码会话。
type Pipeline struct {
	encoderName  string
	deviceType   string
	hwAccel      string
}

// NewPipeline 创建转码管道，自动选择最佳编码器。
func NewPipeline() *Pipeline {
	name, deviceType, _ := SelectBestEncoder()
	hwAccel := ""
	if deviceType != "" {
		hwAccel = deviceType
	}
	return &Pipeline{
		encoderName: name,
		deviceType:  deviceType,
		hwAccel:     hwAccel,
	}
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
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// 硬件加速设备初始化
	if p.hwAccel != "" {
		args = append(args, "-hwaccel", p.hwAccel)
	}

	args = append(args,
		"-i", inputPath,
		"-c:v", p.encoderName,
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
