package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const metadataParseTimeout = 20 * time.Second

var ffprobeVersionCache = struct {
	sync.Mutex
	path    string
	version string
}{}

type ffprobeEmbeddedOutput struct {
	Format  ffprobeFormatMetadata   `json:"format"`
	Streams []ffprobeStreamMetadata `json:"streams"`
}

type ffprobeFormatMetadata struct {
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	Bitrate        string            `json:"bit_rate"`
	Size           string            `json:"size"`
	Tags           map[string]string `json:"tags"`
}

type ffprobeStreamMetadata struct {
	Index            int               `json:"index"`
	CodecType        string            `json:"codec_type"`
	CodecName        string            `json:"codec_name"`
	CodecLongName    string            `json:"codec_long_name"`
	Profile          string            `json:"profile"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	PixelFormat      string            `json:"pix_fmt"`
	FrameRate        string            `json:"r_frame_rate"`
	AverageFrameRate string            `json:"avg_frame_rate"`
	Bitrate          string            `json:"bit_rate"`
	SampleRate       string            `json:"sample_rate"`
	Channels         int               `json:"channels"`
	ChannelLayout    string            `json:"channel_layout"`
	ColorRange       string            `json:"color_range"`
	ColorSpace       string            `json:"color_space"`
	ColorTransfer    string            `json:"color_transfer"`
	ColorPrimaries   string            `json:"color_primaries"`
	Tags             map[string]string `json:"tags"`
	Disposition      struct {
		Default int `json:"default"`
		Forced  int `json:"forced"`
	} `json:"disposition"`
}

func defaultEmbeddedMetadataParser(ctx context.Context, media models.MediaFile) (ParsedEmbeddedMetadata, error) {
	var parsed ParsedEmbeddedMetadata
	var err error
	switch builtInMediaExtensions[normalizeExtension(media.Format)] {
	case MediaTypeVideo:
		parsed, err = probeEmbeddedVideoMetadata(ctx, media.FilePath)
	case MediaTypeImage:
		parsed, err = parseEmbeddedImageMetadata(media.FilePath)
	default:
		return ParsedEmbeddedMetadata{}, errors.New("媒体类型不支持元数据解析")
	}
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	return finalizeEmbeddedMetadata(media, parsed)
}

func finalizeEmbeddedMetadata(media models.MediaFile, parsed ParsedEmbeddedMetadata) (ParsedEmbeddedMetadata, error) {
	normalized := parsed.Normalized
	normalized.FileSize = media.FileSize
	normalized.FileMTime = media.ModifiedAt
	if info, err := os.Stat(filepath.FromSlash(media.FilePath)); err == nil {
		normalized.FileSize = info.Size()
		normalized.FileMTime = info.ModTime()
	}
	if media.ContentHash != "" && !media.ContentHashStale {
		normalized.FileHash = media.ContentHash
		normalized.FileHashAlgo = media.ContentHashAlgo
	}
	normalizedJSON, err := marshalMetadataJSON(normalized)
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	parsed.Normalized = normalized
	parsed.NormalizedJSON = normalizedJSON
	return parsed, nil
}

func probeEmbeddedVideoMetadata(ctx context.Context, path string) (ParsedEmbeddedMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataParseTimeout)
	defer cancel()
	stdout, stderr, err := runFFprobeMetadata(ctx, path)
	if err != nil {
		return ParsedEmbeddedMetadata{}, metadataProbeError(ctx, err, stderr)
	}
	normalized, err := parseVideoEmbeddedMetadata(stdout)
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	return buildParsedVideoMetadata(ctx, stdout, normalized)
}

func runFFprobeMetadata(ctx context.Context, path string) ([]byte, string, error) {
	args := []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams", filepath.FromSlash(path)}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, getFFprobePath(), args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.String(), err
}

func metadataProbeError(ctx context.Context, err error, stderr string) error {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("ffprobe 解析超时: %w", err)
	}
	if detail := tailString(stderr, thumbnailStderrTailLimit); detail != "" {
		return fmt.Errorf("ffprobe 解析失败: %w; stderr: %s", err, detail)
	}
	return fmt.Errorf("ffprobe 解析失败: %w", err)
}

func buildParsedVideoMetadata(ctx context.Context, raw []byte, normalized NormalizedEmbeddedMetadata) (ParsedEmbeddedMetadata, error) {
	rawJSON, err := compactJSON(raw)
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	normalizedJSON, err := marshalMetadataJSON(normalized)
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	return ParsedEmbeddedMetadata{
		Source: MetadataSourceFFprobe, Tool: "ffprobe", ToolVersion: ffprobeToolVersion(ctx),
		RawJSON: rawJSON, NormalizedJSON: normalizedJSON, Normalized: normalized,
	}, nil
}

func parseVideoEmbeddedMetadata(raw []byte) (NormalizedEmbeddedMetadata, error) {
	var probe ffprobeEmbeddedOutput
	if err := json.Unmarshal(raw, &probe); err != nil {
		return NormalizedEmbeddedMetadata{}, fmt.Errorf("解析 ffprobe JSON 失败: %w", err)
	}
	result := NormalizedEmbeddedMetadata{MediaType: MediaTypeVideo, Tags: copyStringMap(probe.Format.Tags)}
	result.Container = normalizeContainerMetadata(probe.Format)
	for _, stream := range probe.Streams {
		normalizeVideoStream(&result, stream)
	}
	return result, nil
}

func normalizeContainerMetadata(format ffprobeFormatMetadata) ContainerMetadata {
	return ContainerMetadata{
		FormatName: format.FormatName, FormatLongName: format.FormatLongName,
		Duration: parseFloatMetadata(format.Duration), Bitrate: parseInt64Metadata(format.Bitrate),
		Size: parseInt64Metadata(format.Size), Tags: copyStringMap(format.Tags),
	}
}

func normalizeVideoStream(result *NormalizedEmbeddedMetadata, stream ffprobeStreamMetadata) {
	switch stream.CodecType {
	case "video":
		result.VideoStreams = append(result.VideoStreams, videoStreamMetadata(stream))
	case "audio":
		result.AudioStreams = append(result.AudioStreams, audioStreamMetadata(stream))
	case "subtitle":
		result.SubtitleStreams = append(result.SubtitleStreams, subtitleStreamMetadata(stream))
	}
}

func videoStreamMetadata(stream ffprobeStreamMetadata) VideoStreamMetadata {
	return VideoStreamMetadata{
		Index: stream.Index, CodecName: stream.CodecName, CodecLongName: stream.CodecLongName,
		Profile: stream.Profile, Width: stream.Width, Height: stream.Height, PixelFormat: stream.PixelFormat,
		FrameRate: stream.FrameRate, AverageFrameRate: stream.AverageFrameRate,
		FrameRateFPS: parseFrameRate(stream.FrameRate), Bitrate: parseInt64Metadata(stream.Bitrate),
		Language: stream.Tags["language"], Title: stream.Tags["title"],
		Default: stream.Disposition.Default == 1, Forced: stream.Disposition.Forced == 1,
		Color: ColorMetadata{Range: stream.ColorRange, Space: stream.ColorSpace, Transfer: stream.ColorTransfer, Primaries: stream.ColorPrimaries},
	}
}

func audioStreamMetadata(stream ffprobeStreamMetadata) AudioStreamMetadata {
	return AudioStreamMetadata{
		Index: stream.Index, CodecName: stream.CodecName, CodecLongName: stream.CodecLongName,
		Profile: stream.Profile, SampleRate: int(parseInt64Metadata(stream.SampleRate)), Channels: stream.Channels,
		ChannelLayout: stream.ChannelLayout, Bitrate: parseInt64Metadata(stream.Bitrate),
		Language: stream.Tags["language"], Title: stream.Tags["title"],
		Default: stream.Disposition.Default == 1, Forced: stream.Disposition.Forced == 1,
	}
}

func subtitleStreamMetadata(stream ffprobeStreamMetadata) SubtitleStreamMetadata {
	return SubtitleStreamMetadata{
		Index: stream.Index, CodecName: stream.CodecName, CodecLongName: stream.CodecLongName,
		Language: stream.Tags["language"], Title: stream.Tags["title"],
		Default: stream.Disposition.Default == 1, Forced: stream.Disposition.Forced == 1,
	}
}

func parseFrameRate(raw string) float64 {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 2 {
		return parseFloatMetadata(raw)
	}
	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	denominator, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func parseInt64Metadata(raw string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return value
}

func parseFloatMetadata(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

func compactJSON(raw []byte) (string, error) {
	var output bytes.Buffer
	if err := json.Compact(&output, raw); err != nil {
		return "", fmt.Errorf("压缩原始元数据 JSON 失败: %w", err)
	}
	return output.String(), nil
}

func marshalMetadataJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("序列化规范化元数据失败: %w", err)
	}
	return string(data), nil
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func ffprobeToolVersion(ctx context.Context) string {
	path := getFFprobePath()
	ffprobeVersionCache.Lock()
	defer ffprobeVersionCache.Unlock()
	if ffprobeVersionCache.path == path && ffprobeVersionCache.version != "" {
		return ffprobeVersionCache.version
	}
	version := detectFFprobeVersion(ctx, path)
	ffprobeVersionCache.path, ffprobeVersionCache.version = path, version
	return version
}

func detectFFprobeVersion(ctx context.Context, path string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, path, "-version").Output()
	if err != nil {
		return "unknown"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	return strings.TrimSpace(line)
}
