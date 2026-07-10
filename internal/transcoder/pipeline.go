package transcoder

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
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
	encoderName string
	deviceType  string
	hwAccel     string
	codec       string // 目标输出编码（"h264"/"h265"/"av1"/"vp9"）；空按 h264 处理
}

// NewPipeline 创建转码管道，自动选择最佳 H.264 编码器（默认目标编码 H.264，行为不变）。
func NewPipeline() *Pipeline {
	return NewPipelineForCodec(DefaultTargetCodec)
}

// NewPipelineForCodec 按目标编码创建转码管道（FR-50）。
// 编码器经 SelectEncoderForCodec 按 codec 选硬件优先、无则软件兜底；
// 未知编码回落默认 H.264，保证默认行为不变。
func NewPipelineForCodec(codec string) *Pipeline {
	if _, ok := CodecOutputParams(codec); !ok {
		codec = DefaultTargetCodec
	}
	name, deviceType := selectEncoder(codec)
	hwAccel := ""
	if deviceType != "" {
		hwAccel = deviceType
	}
	return &Pipeline{
		encoderName: name,
		deviceType:  deviceType,
		hwAccel:     hwAccel,
		codec:       codec,
	}
}

// NewPipelineForCodecWithPolicy 按目标编码与硬件策略创建转码管道。
func NewPipelineForCodecWithPolicy(codec string, policy HardwarePolicy) (*Pipeline, error) {
	if _, ok := CodecOutputParams(codec); !ok {
		codec = DefaultTargetCodec
	}
	var results []EncoderProbeResult
	if snap := probeSnapshot.Load(); snap != nil {
		results = *snap
	}
	name, deviceType, _, err := SelectEncoderForCodecWithPolicy(results, codec, policy)
	if err != nil {
		return nil, err
	}
	return newPipelineForEncoder(codec, name, deviceType), nil
}

func newPipelineForEncoder(codec, name, deviceType string) *Pipeline {
	hwAccel := ""
	if deviceType != "" {
		hwAccel = deviceType
	}
	return &Pipeline{encoderName: name, deviceType: deviceType, hwAccel: hwAccel, codec: codec}
}

// selectEncoder 读实测快照按硬件优先级为 codec 选编码器，冷态/无可用时软件兜底。
func selectEncoder(codec string) (encoderName, deviceType string) {
	// h264 维持既有入口（含冷态 libx264 兜底），保证默认行为不变
	if codec == DefaultTargetCodec {
		name, dev, _ := SelectBestEncoder()
		return name, dev
	}
	snap := probeSnapshot.Load()
	if snap != nil {
		if enc, dev, ok := SelectEncoderForCodec(*snap, codec); ok {
			return enc, dev
		}
	}
	return softwareEncoderForCodec(codec), ""
}

// softwareEncoderForCodec 返回各编码的软件兜底编码器名。
func softwareEncoderForCodec(codec string) string {
	switch codec {
	case "h265":
		return "libx265"
	case "av1":
		return "libsvtav1"
	case "vp9":
		return "libvpx-vp9"
	default:
		return "libx264"
	}
}

// pipelineCodec 返回管道的有效目标编码，空值按默认 H.264 处理。
func (p *Pipeline) pipelineCodec() string {
	if p.codec == "" {
		return DefaultTargetCodec
	}
	return p.codec
}

// hardwareDeviceType 返回管道的有效硬件设备类型，并兼容旧代码直接设置 hwAccel。
func (p *Pipeline) hardwareDeviceType() string {
	if p.deviceType != "" {
		return p.deviceType
	}
	return p.hwAccel
}

// Run 启动转码管道，将 ffmpeg stdout 写入 dst。
// ctx 取消时自动终止 ffmpeg 进程。
func (p *Pipeline) Run(ctx context.Context, inputPath string, dst io.Writer) error {
	return p.RunWithSeek(ctx, inputPath, dst, 0)
}

// RunWithSeek 启动转码管道，支持 Seek 位置（秒）。
func (p *Pipeline) RunWithSeek(ctx context.Context, inputPath string, dst io.Writer, seekPosition float64) error {
	args := p.buildArgs(inputPath, seekPosition)

	cmd := ffmpegCommandContext(ctx, args...)
	cmd.Stdout = dst
	cmd.Stderr = &logWriter{prefix: "[ffmpeg]"}
	cmd.WaitDelay = 5 * time.Second

	setProcessGroup(cmd)

	log.Printf("[INFO] 启动转码: ffmpeg %s", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 失败: %w", err)
	}

	// 等待 ffmpeg 结束或 context 取消
	err := cmd.Wait()
	if ctx.Err() != nil {
		// context 已被取消，exec.CommandContext 已自动终止进程，无需额外 kill
		if err != nil {
			log.Printf("[INFO] context 取消后 ffmpeg 返回: %v", err)
		}
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg 转码失败: %w", err)
	}

	log.Printf("[INFO] 转码完成: %s", inputPath)
	return nil
}

// buildArgs 构建 ffmpeg 命令行参数。
// seekPosition 为 Seek 位置（秒），0 表示从头开始。
func (p *Pipeline) buildArgs(inputPath string, seekPosition float64) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	// Seek 位置（在 -i 之前，加速定位）
	if seekPosition > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.2f", seekPosition))
	}

	deviceType := p.hardwareDeviceType()
	args = appendHardwareInputArgs(args, deviceType)

	// 按目标编码取输出参数（编码器名 + 像素格式 + 关键参数），默认 h264 行为不变。
	params, _ := CodecOutputParams(p.pipelineCodec())

	args = append(args, "-i", inputPath)
	args = appendHardwareUploadArgs(args, deviceType)
	args = append(args,
		"-c:v", p.encoderName,
		// 强制 8-bit yuv420p：10-bit 源（如 HEVC Main 10）否则编出 10-bit High/Main 10，
		// 浏览器与 mpegts.js 无法解码播放。
		"-pix_fmt", params.PixFmt,
	)
	// 该编码的额外输出参数（如 h265 的 -tag:v hvc1）
	args = append(args, params.ExtraArgs...)

	args = append(args,
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
