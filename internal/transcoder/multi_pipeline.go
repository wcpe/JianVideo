package transcoder

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"
)

// QualityDefinition 定义一个码率档位。
type QualityDefinition struct {
	Name       string
	Width      int
	Height     int
	VideoRate  string
	AudioRate  string
}

// qualityLadders 预定义的码率阶梯。
var qualityLadders = []QualityDefinition{
	{"1080p", 1920, 1080, "5000k", "128k"},
	{"720p", 1280, 720, "2500k", "128k"},
	{"480p", 854, 480, "1000k", "96k"},
}

// QualitiesForResolution 根据源分辨率返回应输出的码率档位列表。
func QualitiesForResolution(width, height int) []string {
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
func (mp *MultiPipeline) RunMulti(ctx context.Context, inputPath string, qualities []string, dsts []io.Writer) error {
	if len(dsts) > 0 {
		log.Printf("[WARN] MultiPipeline.RunMulti: dsts 参数被忽略，多码率输出直接写入文件系统")
	}
	if len(qualities) != len(dsts) {
		return fmt.Errorf("码率数量(%d)与写入器数量(%d)不匹配", len(qualities), len(dsts))
	}

	// 构建码率定义映射
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

	args := mp.buildMultiArgs(inputPath, qualityDefs)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = io.Discard // 多码率模式不输出到 stdout
	cmd.Stderr = &logWriter{prefix: "[ffmpeg-multi]"}
	cmd.WaitDelay = 5 * time.Second

	setProcessGroup(cmd)

	log.Printf("[INFO] 启动多码率转码: ffmpeg %s", strings.Join(args, " "))
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

	log.Printf("[INFO] 多码率转码完成: %s", inputPath)
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

	// 硬件加速
	if mp.pipeline.hwAccel != "" {
		args = append(args, "-hwaccel", mp.pipeline.hwAccel)
	}

	args = append(args, "-i", inputPath)

	// 构建 filter_complex split + scale
	// [0:v]split=3[v1][v2][v3]; [v1]scale=1920:1080[v1out]; ...
	splitPart := fmt.Sprintf("[0:v]split=%d", n)
	outLabels := make([]string, n)
	for i := 0; i < n; i++ {
		outLabels[i] = fmt.Sprintf("[v%d]", i+1)
	}
	splitPart += strings.Join(outLabels, "")

	scaleParts := make([]string, n)
	for i, q := range qualities {
		scaleParts[i] = fmt.Sprintf("[v%d]scale=%d:%d[v%dout]", i+1, q.Width, q.Height, i+1)
	}

	filterComplex := splitPart + "; " + strings.Join(scaleParts, "; ")

	// 构建输出映射
	outputParts := make([]string, 0, n*6)
	for i, q := range qualities {
		outLabel := fmt.Sprintf("[v%dout]", i+1)
		segFilename := fmt.Sprintf("%s_%%03d.ts", q.Name)
		m3u8Filename := fmt.Sprintf("%s.m3u8", q.Name)

		outputParts = append(outputParts,
			"-map", outLabel,
			"-c:v", mp.pipeline.encoderName,
			"-b:v", q.VideoRate,
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
