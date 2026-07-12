package transcoder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BuildABRArgs 构建单 FFmpeg 进程生成多档 MPEG-TS HLS 的参数。
func (p *MultiPipeline) BuildABRArgs(inputPath string, ladder []QualityDefinition) []string {
	return p.buildABRArgs(inputPath, ladder, "")
}

func (p *MultiPipeline) buildABRArgs(inputPath string, ladder []QualityDefinition, outputDir string) []string {
	deviceType := p.pipeline.hardwareDeviceType()
	params, _ := CodecOutputParams(p.pipeline.pipelineCodec())
	args := []string{"-hide_banner", "-loglevel", "warning", "-y"}
	args = appendHardwareInputArgs(args, deviceType)
	outputFilter := "format=" + params.PixFmt
	if rule, ok := hardwareUploadRuleFor(deviceType); ok {
		outputFilter = rule.uploadFilter
	}
	args = append(args, "-i", inputPath, "-filter_complex", buildABRFilter(ladder, outputFilter))
	for index, variant := range ladder {
		args = append(args,
			"-map", fmt.Sprintf("[v%dout]", index+1), "-map", "0:a:0?",
			"-c:v", p.pipeline.encoderName, "-b:v", variant.VideoRate,
		)
		args = append(args, params.ExtraArgs...)
		args = append(args,
			"-g", "48", "-keyint_min", "48", "-sc_threshold", "0",
			"-c:a", "aac", "-b:a", variant.AudioRate,
			"-f", "hls", "-hls_time", "6", "-hls_list_size", "0", "-hls_playlist_type", "vod",
			"-hls_flags", "independent_segments",
			"-hls_segment_filename", abrOutputPath(outputDir, variant.Name, "segment_%03d.ts"),
			abrOutputPath(outputDir, variant.Name, "index.m3u8"),
		)
	}
	return args
}

func buildABRFilter(ladder []QualityDefinition, outputFilter string) string {
	outputs := make([]string, len(ladder))
	filters := make([]string, 0, len(ladder)+1)
	for index := range ladder {
		outputs[index] = fmt.Sprintf("[v%d]", index+1)
	}
	filters = append(filters, fmt.Sprintf("[0:v]split=%d%s", len(ladder), strings.Join(outputs, "")))
	for index, variant := range ladder {
		filters = append(filters, fmt.Sprintf("[v%d]scale=%d:%d,%s[v%dout]", index+1, variant.Width, variant.Height, outputFilter, index+1))
	}
	return strings.Join(filters, ";")
}

func abrOutputPath(root, variant, name string) string {
	if root == "" {
		return variant + "/" + name
	}
	return filepath.Join(root, variant, name)
}

// RunABRToDir 在一个 FFmpeg 进程中生成全部 ABR 档位。
func (p *MultiPipeline) RunABRToDir(ctx context.Context, inputPath string, ladder []QualityDefinition, outputDir string) error {
	if len(ladder) == 0 {
		return fmt.Errorf("ABR ladder 不能为空")
	}
	for _, variant := range ladder {
		if err := os.MkdirAll(filepath.Join(outputDir, variant.Name), 0o750); err != nil {
			return fmt.Errorf("创建 ABR 档位目录失败: %w", err)
		}
	}
	return p.runFFmpeg(ctx, p.buildABRArgs(inputPath, ladder, ""), outputDir)
}

// BuildABRMasterM3U8 生成引用档位子目录的 HLS master playlist。
func BuildABRMasterM3U8(ladder []QualityDefinition) string {
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for _, variant := range ladder {
		bandwidth := parseBitrate(variant.VideoRate) + parseBitrate(variant.AudioRate)
		builder.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n", bandwidth, variant.Width, variant.Height))
		builder.WriteString(variant.Name + "/index.m3u8\n")
	}
	return builder.String()
}

// PreSliceABRWithPolicyToDir 复用预切片清理、超时、校验和硬件回退生命周期生成 ABR。
func PreSliceABRWithPolicyToDir(ctx context.Context, mediaID int64, inputPath string, ladder []QualityDefinition, policy HardwarePolicy, outputDir string) (*PreSliceResult, error) {
	if err := resetHLSOutputDir(outputDir); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := runABRToDirWithPolicy(runCtx, inputPath, ladder, outputDir, policy); err != nil {
		_ = os.RemoveAll(outputDir)
		log.Printf("[ERROR] ABR 转码失败: mediaID=%d, outputDir=%s, err=%v", mediaID, outputDir, err)
		return nil, fmt.Errorf("ffmpeg ABR 转码失败: %w", err)
	}
	if err := verifyABROutputs(outputDir, ladder); err != nil {
		_ = os.RemoveAll(outputDir)
		return nil, err
	}
	master := BuildABRMasterM3U8(ladder)
	if err := os.WriteFile(filepath.Join(outputDir, "master.m3u8"), []byte(master), 0o640); err != nil {
		_ = os.RemoveAll(outputDir)
		return nil, fmt.Errorf("写入 ABR master 失败: %w", err)
	}
	names := make([]string, len(ladder))
	for index := range ladder {
		names[index] = ladder[index].Name
	}
	return &PreSliceResult{OutputDir: outputDir, Qualities: names}, nil
}

func runABRToDirWithPolicy(ctx context.Context, inputPath string, ladder []QualityDefinition, outputDir string, policy HardwarePolicy) error {
	pipeline, err := NewPipelineForCodecWithPolicy(DefaultTargetCodec, policy)
	if err != nil {
		return err
	}
	err = NewMultiPipeline(pipeline).RunABRToDir(ctx, inputPath, ladder, outputDir)
	if err == nil || pipeline.deviceType == "" || !policy.Fallback {
		return err
	}
	log.Printf("[WARN] ABR 硬件转码失败，改用软件回退: encoder=%s, err=%v", pipeline.encoderName, err)
	_ = os.RemoveAll(outputDir)
	software, softwareErr := NewPipelineForCodecWithPolicy(DefaultTargetCodec, HardwarePolicy{Mode: HWAccelModeSoftware, Fallback: true})
	if softwareErr != nil {
		return err
	}
	return NewMultiPipeline(software).RunABRToDir(ctx, inputPath, ladder, outputDir)
}

func verifyABROutputs(outputDir string, ladder []QualityDefinition) error {
	for _, variant := range ladder {
		path := filepath.Join(outputDir, variant.Name, "index.m3u8")
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("ABR 档位 %s 未生成: %w", variant.Name, err)
		}
	}
	return nil
}

func parseBitrate(value string) int {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	multiplier := 1
	if strings.HasSuffix(trimmed, "k") {
		multiplier = 1000
		trimmed = strings.TrimSuffix(trimmed, "k")
	}
	parsed, _ := strconv.Atoi(trimmed)
	return parsed * multiplier
}
