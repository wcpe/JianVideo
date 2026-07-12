package transcoder

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// FFmpegPath 全局可执行文件路径，可由 SetFFmpegPath 注入。
// 默认通过 exec.LookPath("ffmpeg") 解析；找不到则 RunMulti 系列会返回错误。
var (
	ffmpegPath           = "ffmpeg"
	ffmpegPathGeneration uint64
	ffmpegPathMu         sync.RWMutex
)

// SetFFmpegPath 显式设置 ffmpeg 可执行文件路径（绝对或相对路径均可）。
// 通常由 main.go 从环境变量 JIANVIDEO_FFMPEG_PATH 注入。
func SetFFmpegPath(path string) {
	if path == "" {
		return
	}
	ffmpegPathMu.Lock()
	defer ffmpegPathMu.Unlock()
	ffmpegPath = path
	ffmpegPathGeneration++
	invalidateFFmpegContentDigest()
	clearProbeSnapshot()
}

// ffmpegPathSnapshot 原子读取当前路径与路径代次。
func ffmpegPathSnapshot() (string, uint64) {
	ffmpegPathMu.RLock()
	defer ffmpegPathMu.RUnlock()
	return ffmpegPath, ffmpegPathGeneration
}

// GetFFmpegPath 返回当前 ffmpeg 可执行文件路径。
func GetFFmpegPath() string {
	path, _ := ffmpegPathSnapshot()
	return path
}

// ffmpegCommandContext 使用当前配置路径创建 ffmpeg 命令，路径可包含空格。
func ffmpegCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, GetFFmpegPath(), args...)
}

// ffprobePath 全局 ffprobe 可执行文件路径，可由 SetFFprobePath 注入。
// 默认 "ffprobe"（PATH 查找）；随包附带时由 main.go 解析为同目录捆绑版。
var ffprobePath = "ffprobe"

// SetFFprobePath 显式设置 ffprobe 可执行文件路径。
// 通常由 main.go 从环境变量 JIANVIDEO_FFPROBE_PATH 或同目录捆绑版注入。
func SetFFprobePath(path string) {
	if path != "" {
		ffprobePath = path
	}
}

// GetFFprobePath 返回当前 ffprobe 可执行文件路径。
func GetFFprobePath() string {
	return ffprobePath
}

// IsFFmpegAvailable 检查当前配置的 ffmpeg 是否可执行。
func IsFFmpegAvailable() bool {
	path, _ := ffmpegPathSnapshot()
	return isFFmpegPathAvailable(path)
}

// isFFmpegPathAvailable 检查指定路径是否可执行。
func isFFmpegPathAvailable(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	// 退化到 PATH 查找
	_, err := exec.LookPath(path)
	return err == nil
}

// QualityDefinition 定义一个码率档位。
type QualityDefinition struct {
	Name      string `json:"name"` // 如 "1080p"
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	VideoRate string `json:"video_rate"` // 如 "5000k"
	AudioRate string `json:"audio_rate"` // 如 "192k"
}

// qualityLadders 预定义的码率阶梯。
var qualityLadders = []QualityDefinition{
	{"1080p", 1920, 1080, "5000k", "128k"},
	{"720p", 1280, 720, "2500k", "128k"},
	{"480p", 854, 480, "1000k", "96k"},
}

// QualitiesForResolution 根据源分辨率返回应输出的码率档位列表。
func QualitiesForResolution(_, height int) []string {
	var names []string
	for _, q := range qualityLadders {
		// 只输出不高于源分辨率的档位
		if q.Height <= height {
			names = append(names, q.Name)
		}
	}
	// 兜底：至少输出最低档
	if len(names) == 0 {
		names = append(names, qualityLadders[len(qualityLadders)-1].Name)
	}
	return names
}

// MultiPipeline 管理多个 Pipeline 实例，通过单进程多输出同时生成多码率 HLS。
type MultiPipeline struct {
	pipeline *Pipeline
}

// NewMultiPipeline 创建多码率管道。
func NewMultiPipeline(p *Pipeline) *MultiPipeline {
	return &MultiPipeline{pipeline: p}
}

// RunMulti 启动多码率转码，将 ffmpeg stdout 写入对应 dst 写入器。
// qualities 为码率档位名列表（如 ["1080p","720p","480p"]）。
// dsts 为每个码率对应的 io.Writer，与 qualities 一一对应。
// 多码率模式下 ffmpeg 直接写文件到 cwd，dsts 参数被忽略。
func (mp *MultiPipeline) RunMulti(ctx context.Context, inputPath string, qualities []string, dsts []io.Writer) error {
	if len(dsts) > 0 {
		log.Printf("[WARN] MultiPipeline.RunMulti: dsts 参数被忽略，多码率输出直接写入文件系统")
	}
	return mp.RunMultiToDir(ctx, inputPath, qualities, "")
}

// RunMultiToDir 启动多码率转码，将切片与 m3u8 输出到指定目录。
// 目录不存在会自动创建；outputDir 为空时使用 ffmpeg 默认 cwd。
// qualities 为码率档位名列表（如 ["1080p","720p","480p"]）。
func (mp *MultiPipeline) RunMultiToDir(ctx context.Context, inputPath string, qualities []string, outputDir string) error {
	qualityDefs := make([]QualityDefinition, 0, len(qualities))
	for _, name := range qualities {
		found := false
		for _, q := range qualityLadders {
			if q.Name == name {
				qualityDefs = append(qualityDefs, q)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("未知码率档位: %s", name)
		}
	}

	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o750); err != nil {
			return fmt.Errorf("创建输出目录失败: %w", err)
		}
	}

	args := mp.buildMultiArgs(inputPath, qualityDefs)
	return mp.runFFmpeg(ctx, args, outputDir)
}

// runFFmpeg 用指定 ffmpeg 路径执行参数化命令。
// dir 为 ffmpeg 的工作目录（影响相对路径输出文件位置）。
func (mp *MultiPipeline) runFFmpeg(ctx context.Context, args []string, dir string) error {
	if !IsFFmpegAvailable() {
		return fmt.Errorf("ffmpeg 不可用（路径: %s）", GetFFmpegPath())
	}

	cmd := ffmpegCommandContext(ctx, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &logWriter{prefix: "[ffmpeg-multi]"}
	cmd.WaitDelay = 5 * time.Second
	if dir != "" {
		cmd.Dir = dir
	}

	setProcessGroup(cmd)

	log.Printf("[INFO] 启动 HLS 转码: ffmpeg %s", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动多码率 ffmpeg 失败: %w", err)
	}

	err := cmd.Wait()
	if ctx.Err() != nil {
		if err != nil {
			log.Printf("[INFO] context 取消后 ffmpeg 返回: %v", err)
		}
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("多码率 ffmpeg 转码失败: %w", err)
	}

	log.Printf("[INFO] HLS 转码完成: %s", dir)
	return nil
}

// BuildArgs 构建多码率 ffmpeg 命令行参数（公开，供测试和外部调用）。
func (mp *MultiPipeline) BuildArgs(inputPath string, qualityNames []string) []string {
	// 构建码率定义映射
	qualityDefs := make([]QualityDefinition, 0, len(qualityNames))
	for _, name := range qualityNames {
		for _, q := range qualityLadders {
			if q.Name == name {
				qualityDefs = append(qualityDefs, q)
				break
			}
		}
	}
	return mp.buildMultiArgs(inputPath, qualityDefs)
}

// buildMultiArgs 构建多码率 ffmpeg 命令行参数。
func (mp *MultiPipeline) buildMultiArgs(inputPath string, qualities []QualityDefinition) []string {
	n := len(qualities)
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}

	deviceType := mp.pipeline.hardwareDeviceType()
	args = appendHardwareInputArgs(args, deviceType)
	args = append(args, "-i", inputPath)

	// 按目标编码取输出参数（编码器名来自 pipeline，像素格式 + 关键参数来自映射），默认 h264 行为不变。
	params, _ := CodecOutputParams(mp.pipeline.pipelineCodec())

	// 构建 filter_complex split + scale
	// [0:v]split=3[v1][v2][v3]; [v1]scale=1920:1080[v1out]; ...
	splitPart := fmt.Sprintf("[0:v]split=%d", n)
	outLabels := make([]string, n)
	for i := 0; i < n; i++ {
		outLabels[i] = fmt.Sprintf("[v%d]", i+1)
	}
	splitPart += strings.Join(outLabels, "")

	scaleParts := make([]string, n)
	outputFilter := "format=" + params.PixFmt
	if rule, ok := hardwareUploadRuleFor(deviceType); ok {
		outputFilter = rule.uploadFilter
	}
	for i, q := range qualities {
		// 普通编码保持 8-bit yuv420p；VAAPI/Vulkan 转为 nv12 后上传硬件帧。
		scaleParts[i] = fmt.Sprintf("[v%d]scale=%d:%d,%s[v%dout]", i+1, q.Width, q.Height, outputFilter, i+1)
	}

	filterComplex := splitPart + "; " + strings.Join(scaleParts, "; ")

	// 构建输出映射
	outputParts := make([]string, 0, n*6)
	for i, q := range qualities {
		outLabel := fmt.Sprintf("[v%dout]", i+1)
		// 切片与 m3u8 同目录，hls.js 相对路径拼出的 URL = playlist/{quality}.m3u8 的 base + {quality}_segment_NNN.ts
		// 即 /api/play/hls/:id/playlist/{quality}_segment_NNN.ts
		segFilename := fmt.Sprintf("%s_%%03d.ts", q.Name)
		m3u8Filename := fmt.Sprintf("%s.m3u8", q.Name)

		outputParts = append(outputParts,
			"-map", outLabel,
			"-c:v", mp.pipeline.encoderName,
			"-b:v", q.VideoRate,
		)
		// 该编码的额外输出参数（如 h265 的 -tag:v hvc1）
		outputParts = append(outputParts, params.ExtraArgs...)
		outputParts = append(outputParts,
			"-g", "48",
			"-keyint_min", "48",
			"-sc_threshold", "0",
			"-map", "0:a",
			"-c:a", "aac",
			"-b:a", q.AudioRate,
			"-f", "hls",
			"-hls_time", "3",
			"-hls_playlist_type", "event",
			"-hls_flags", "independent_segments",
			"-hls_segment_filename", segFilename,
			m3u8Filename,
		)
	}

	args = append(args, "-filter_complex", filterComplex)
	args = append(args, outputParts...)

	return args
}
