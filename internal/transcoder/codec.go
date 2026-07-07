package transcoder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MediaInfo 存储 ffprobe 探测结果。
type MediaInfo struct {
	ContainerFormat string  `json:"container_format"`
	VideoCodec      string  `json:"video_codec"`
	AudioCodec      string  `json:"audio_codec"`
	Duration        float64 `json:"duration"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Bitrate         int     `json:"bitrate"`
}

// SubtitleFile 表示一个外挂字幕文件。
type SubtitleFile struct {
	Path   string
	Format string // srt, ass, sup
}

// ffprobeOutput 对应 ffprobe JSON 输出结构。
type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
	BitRate    string `json:"bit_rate"`
}

type ffprobeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// ProbeMetadata 使用 ffprobe 探测视频文件元数据。
func ProbeMetadata(filePath string) (*MediaInfo, error) {
	if !isFFprobeAvailable() {
		return nil, fmt.Errorf("ffprobe 未安装或不在 PATH 中")
	}

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, GetFFprobePath(), args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe 执行失败: %w", err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		return nil, fmt.Errorf("ffprobe 输出解析失败: %w", err)
	}

	info := &MediaInfo{
		ContainerFormat: probe.Format.FormatName,
	}

	// 解析时长
	if probe.Format.Duration != "" {
		if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			info.Duration = d
		}
	}

	// 解析码率
	if probe.Format.BitRate != "" {
		if b, err := strconv.Atoi(probe.Format.BitRate); err == nil {
			info.Bitrate = b
		}
	}

	// 从流中提取视频/音频编码和分辨率
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec = stream.CodecName
				info.Width = stream.Width
				info.Height = stream.Height
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = stream.CodecName
			}
		}
	}

	return info, nil
}

// IsBrowserCompatible 判断视频是否可以在浏览器中直接播放。
// 只有 H.264 + AAC 的 MP4 容器才能直出。
func IsBrowserCompatible(info *MediaInfo) bool {
	if info == nil {
		return false
	}
	if strings.ToLower(info.VideoCodec) != "h264" {
		return false
	}
	if strings.ToLower(info.AudioCodec) != "aac" {
		return false
	}
	// 检查容器格式是否包含 mp4/mov
	container := strings.ToLower(info.ContainerFormat)
	if !strings.Contains(container, "mp4") && !strings.Contains(container, "mov") {
		return false
	}
	return true
}

// extractPrimaryFormat 从 ffprobe format_name 中提取主容器格式。
func extractPrimaryFormat(formatName string) string {
	parts := strings.Split(formatName, ",")
	if len(parts) == 0 {
		return formatName
	}
	return strings.TrimSpace(parts[0])
}

// isFFprobeAvailable 检查 ffprobe 是否可用。
func isFFprobeAvailable() bool {
	_, err := exec.LookPath("ffprobe")
	return err == nil
}

// isFFmpegAvailable 检查 ffmpeg 是否可用。
func isFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// generateTestVideo 用 ffmpeg 生成一个 1 秒测试视频。
func generateTestVideo(outputPath string) error {
	if !isFFmpegAvailable() {
		return fmt.Errorf("ffmpeg 不可用")
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "testsrc=duration=1:size=320x240:rate=24",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-t", "1",
		outputPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 生成测试视频失败: %s, 输出: %s", err, string(out))
	}
	return nil
}

// FindSubtitleFiles 查找视频文件同目录下的外挂字幕。
func FindSubtitleFiles(videoPath string) ([]SubtitleFile, error) {
	dir := filepath.Dir(videoPath)
	baseName := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	subtitleExts := map[string]string{
		"srt": "srt",
		"ass": "ass",
		"ssa": "ass",
		"sup": "sup",
	}

	var results []SubtitleFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Name()), "."))
		format, ok := subtitleExts[ext]
		if !ok {
			continue
		}

		// 检查是否是同名文件（去掉扩展名后匹配）
		entryBase := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !strings.EqualFold(entryBase, baseName) {
			continue
		}

		results = append(results, SubtitleFile{
			Path:   filepath.Join(dir, entry.Name()),
			Format: format,
		})
	}

	return results, nil
}

// ProbeAndCacheResult 探测视频元数据并返回兼容性信息。
// 返回 MediaInfo 指针和是否兼容。
func ProbeAndCacheResult(filePath string) (*MediaInfo, bool, error) {
	info, err := ProbeMetadata(filePath)
	if err != nil {
		return nil, false, err
	}
	return info, IsBrowserCompatible(info), nil
}

// formatContainerForStorage 将 ffprobe 的 format_name 简化为存储用格式名。
func formatContainerForStorage(formatName string) string {
	primary := extractPrimaryFormat(formatName)
	// 统一映射
	switch primary {
	case "mov", "mp4", "m4a", "3gp", "3g2", "mj2":
		return "mp4"
	case "matroska", "webm":
		// 进一步区分：需要查看流编码
		return "mkv"
	case "avi":
		return "avi"
	case "mpegts":
		return "ts"
	default:
		return strings.ToLower(primary)
	}
}

// detectContainerFormat 根据文件路径和 ffprobe 结果确定存储格式。
func detectContainerFormat(filePath string, probeFormatName string) string {
	// 优先使用文件扩展名（更可靠）
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	switch ext {
	case "mp4", "m4v":
		return "mp4"
	case "mkv":
		return "mkv"
	case "avi":
		return "avi"
	case "mov":
		return "mov"
	case "webm":
		return "webm"
	case "ts", "mts", "m2ts":
		return "ts"
	case "rm", "rmvb":
		return "rmvb"
	}

	// 回退到 ffprobe 结果
	return formatContainerForStorage(probeFormatName)
}

// EnsureDir 确保目录存在。
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o750)
}

// Now 返回当前时间（可测试性包装）。
func Now() time.Time {
	return time.Now()
}
