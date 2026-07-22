// Package library 的图片编辑/视频粗剪导出核心（FR2-038、FR2-039）。
//
// 设计原则：
//   - 不修改源文件，所有产物写入 exports/{space}/{media_id}/{task_id}.{ext}
//   - 图片导出由 ImageMagick（magick）执行；视频粗剪由 ffmpeg 执行。
//   - 幂等键含 Space/Media/参数指纹，避免重复产物。
//   - 产物可重建：与缩略图/HLS 缓存一致，归 storage 可清理策略处理。
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// 导出根目录名（位于数据目录下，与 thumbnails/hls/image_cache 平级）。
	exportDirName = "exports"
	// 默认粗剪最大时长上限（小时），防止滥用；可由调用方覆盖。
	defaultClipMaxDurationSec = 2 * 60 * 60
	// 导出执行超时（图片 60s、视频粗剪按时长估算最长 4h）。
	imageExportTimeout = 60 * time.Second
	videoClipTimeoutPerHour = 30 * time.Minute
)

var (
	// 支持的图片导出格式白名单。
	imageExportFormats = map[string]string{
		"jpg":  "jpeg",
		"jpeg": "jpeg",
		"png":  "png",
		"webp": "webp",
	}
	// 支持的视频粗剪输出格式白名单。
	videoClipFormats = map[string]bool{
		"mp4": true,
		"mkv": true,
		"mov": true,
	}
)

// ImageExportParams 描述图片编辑参数与导出格式（FR2-038）。
// 数值范围与前端滑杆对齐；首期固定四项参数。
type ImageExportParams struct {
	Exposure  float64 `json:"exposure"`  // 曝光补偿，[-100, 100]
	Contrast  float64 `json:"contrast"`  // 对比度，[-100, 100]
	Saturation float64 `json:"saturation"` // 饱和度，[-100, 100]
	Temp      float64 `json:"temperature"` // 色温，[-100, 100]
	Format    string  `json:"format"`     // jpeg/png/webp
}

// ImageExportResult 描述图片导出成功后的产物。
type ImageExportResult struct {
	OutputPath     string `json:"output_path"`
	OutputFilename string `json:"output_filename"`
	Format         string `json:"format"`
	SizeBytes      int64  `json:"size_bytes"`
}

// VideoClipParams 描述视频粗剪起止与输出格式（FR2-039）。
type VideoClipParams struct {
	StartSec float64 `json:"start_sec"`
	EndSec   float64 `json:"end_sec"`
	Format   string  `json:"format"` // mp4/mkv/mov
}

// VideoClipResult 描述视频粗剪成功后的产物。
type VideoClipResult struct {
	OutputPath     string  `json:"output_path"`
	OutputFilename string  `json:"output_filename"`
	Format         string  `json:"format"`
	SizeBytes      int64   `json:"size_bytes"`
	DurationSec    float64 `json:"duration_sec"`
}

// InitExportDir 初始化导出根目录（与其它缓存目录平级）。需在 main 启动期调用。
func InitExportDir(baseDir string) {
	dir := filepath.Join(baseDir, exportDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		// 目录创建失败不致命：后续导出任务会按需创建子目录。
		fmt.Fprintf(os.Stderr, "[WARN] 初始化导出根目录失败: dir=%s, err=%v\n", dir, err)
	}
}

// ExportDir 返回导出根目录绝对路径。
func ExportDir(baseDir string) string {
	return filepath.Join(baseDir, exportDirName)
}

// SanitizeImageFormat 归一化图片导出格式。
func SanitizeImageFormat(format string) (string, bool) {
	f := strings.ToLower(strings.TrimSpace(format))
	// 兼容 "image/jpeg"、"jpeg" 等写法。
	f = strings.TrimPrefix(f, "image/")
	f = strings.TrimPrefix(f, ".")
	if _, ok := imageExportFormats[f]; !ok {
		return "", false
	}
	return f, true
}

// SanitizeVideoFormat 归一化视频输出格式。
func SanitizeVideoFormat(format string) (string, bool) {
	f := strings.ToLower(strings.TrimSpace(format))
	f = strings.TrimPrefix(f, "video/")
	f = strings.TrimPrefix(f, ".")
	if !videoClipFormats[f] {
		return "", false
	}
	return f, true
}

// ValidateImageParams 校验图片编辑参数范围。
func ValidateImageParams(p ImageExportParams) error {
	if _, ok := SanitizeImageFormat(p.Format); !ok {
		return fmt.Errorf("不支持的图片导出格式: %s", p.Format)
	}
	if p.Exposure < -100 || p.Exposure > 100 {
		return fmt.Errorf("曝光参数超出范围")
	}
	if p.Contrast < -100 || p.Contrast > 100 {
		return fmt.Errorf("对比度参数超出范围")
	}
	if p.Saturation < -100 || p.Saturation > 100 {
		return fmt.Errorf("饱和度参数超出范围")
	}
	if p.Temp < -100 || p.Temp > 100 {
		return fmt.Errorf("色温参数超出范围")
	}
	return nil
}

// ValidateVideoClipParams 校验视频粗剪起止与时长上限。
func ValidateVideoClipParams(p VideoClipParams, mediaDurationSec float64, maxDurationSec float64) error {
	format, ok := SanitizeVideoFormat(p.Format)
	if !ok {
		return fmt.Errorf("不支持的视频导出格式: %s", p.Format)
	}
	if p.StartSec < 0 || p.EndSec <= p.StartSec {
		return fmt.Errorf("起止时间不合法")
	}
	clipLen := p.EndSec - p.StartSec
	if maxDurationSec <= 0 {
		maxDurationSec = defaultClipMaxDurationSec
	}
	if clipLen > maxDurationSec {
		return fmt.Errorf("导出片段时长超过上限 %.0fs", maxDurationSec)
	}
	if mediaDurationSec > 0 && p.EndSec > mediaDurationSec+1 {
		return fmt.Errorf("结束时间超过媒体时长")
	}
	_ = format // 已校验
	return nil
}

// ImageExportFingerprint 生成图片导出幂等键指纹（与原文件内容无关，避免脏读）。
func ImageExportFingerprint(spaceID string, mediaID int64, params ImageExportParams) string {
	canon := ImageExportParams{
		Exposure:   round1(params.Exposure),
		Contrast:   round1(params.Contrast),
		Saturation: round1(params.Saturation),
		Temp:       round1(params.Temp),
		Format:     strings.ToLower(strings.TrimSpace(params.Format)),
	}
	buf, _ := json.Marshal(canon)
	h := sha256.Sum256(append([]byte(spaceID+"|"+strconv.FormatInt(mediaID, 10)+"|"), buf...))
	return "image-export:" + spaceID + ":" + strconv.FormatInt(mediaID, 10) + ":" + hex.EncodeToString(h[:12])
}

// VideoClipFingerprint 生成视频粗剪幂等键指纹。
func VideoClipFingerprint(spaceID string, mediaID int64, params VideoClipParams) string {
	canon := VideoClipParams{
		StartSec: round2(params.StartSec),
		EndSec:   round2(params.EndSec),
		Format:   strings.ToLower(strings.TrimSpace(params.Format)),
	}
	buf, _ := json.Marshal(canon)
	h := sha256.Sum256(append([]byte(spaceID+"|"+strconv.FormatInt(mediaID, 10)+"|"), buf...))
	return "video-clip:" + spaceID + ":" + strconv.FormatInt(mediaID, 10) + ":" + hex.EncodeToString(h[:12])
}

// ImageExportOutputPath 生成图片导出文件路径：exports/{space}/{media_id}/{task_id}.{ext}。
func ImageExportOutputPath(baseDir, spaceID string, mediaID, taskID int64, format string) string {
	return filepath.Join(
		ExportDir(baseDir), normalizeSpaceID(spaceID),
		strconv.FormatInt(mediaID, 10),
		strconv.FormatInt(taskID, 10)+"."+format,
	)
}

// VideoClipOutputPath 生成视频粗剪文件路径。
func VideoClipOutputPath(baseDir, spaceID string, mediaID, taskID int64, format string) string {
	return ImageExportOutputPath(baseDir, spaceID, mediaID, taskID, format)
}

// ExportImage 生成图片编辑导出产物（FR2-038）。
// baseDir 为数据目录；sourcePath 为源文件绝对路径；taskID 用于产物命名。
func ExportImage(ctx context.Context, baseDir, spaceID string, mediaID, taskID int64, sourcePath string, params ImageExportParams) (*ImageExportResult, error) {
	if err := ValidateImageParams(params); err != nil {
		return nil, err
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("源文件不可访问: %w", err)
	}
	if !IsMagickAvailable() {
		return nil, errors.New("ImageMagick 不可用，无法执行图片导出")
	}
	format, _ := SanitizeImageFormat(params.Format)
	out := ImageExportOutputPath(baseDir, spaceID, mediaID, taskID, format)
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return nil, fmt.Errorf("创建导出目录失败: %w", err)
	}
	args := buildImageExportArgs(sourcePath, out, format, params)

	runCtx, cancel := context.WithTimeout(ctx, imageExportTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, GetMagickPath(), args...)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(out) // 失败清理半成品
		return nil, fmt.Errorf("图片导出失败: %w, 输出: %s", err, strings.TrimSpace(string(outBytes)))
	}
	info, err := os.Stat(out)
	if err != nil {
		return nil, fmt.Errorf("读取导出产物失败: %w", err)
	}
	return &ImageExportResult{
		OutputPath:     out,
		OutputFilename: filepath.Base(out),
		Format:         format,
		SizeBytes:      info.Size(),
	}, nil
}

// buildImageExportArgs 构造 magick 命令行参数：先按源文件解码，再用 -auto-orient 校正方向，
// 接着以 -modulate 实现饱和度/亮度/色相，再以 -brightness-contrast 与 -modulate 拆分曝光/对比，
// 最后输出为指定格式。色温通过 -color-matrix 偏移近似实现，避免引入 GPU 依赖。
func buildImageExportArgs(src, dst, format string, p ImageExportParams) []string {
	args := []string{
		src + "[0]",
		"-auto-orient",
	}
	// 饱和度与色相：以 -modulate 100 100 <sat%> 应用，0~200 范围。
	satPercent := int(100 + p.Saturation)
	if satPercent < 0 {
		satPercent = 0
	} else if satPercent > 200 {
		satPercent = 200
	}
	// 曝光：偏置亮度，叠加在 modulate 的亮度分量上。
	brightnessPercent := int(100 + p.Exposure)
	if brightnessPercent < 0 {
		brightnessPercent = 0
	} else if brightnessPercent > 200 {
		brightnessPercent = 200
	}
	// 色温：偏移蓝/黄通道。
	blueShift := p.Temp
	redShift := -p.Temp
	args = append(args,
		"-modulate", strconv.Itoa(brightnessPercent)+","+strconv.Itoa(int(satPercent))+",100",
		"-brightness-contrast", strconv.FormatFloat(p.Contrast, 'f', -1, 64)+"x0",
		"-channel", "R", "-evaluate", "add", strconv.FormatFloat(redShift, 'f', -1, 64)+"%",
		"+channel",
		"-channel", "B", "-evaluate", "add", strconv.FormatFloat(blueShift, 'f', -1, 64)+"%",
		"+channel",
	)
	switch format {
	case "jpeg", "jpg":
		args = append(args, "-quality", "92")
	case "png":
		args = append(args, "-define", "png:compression-level=6")
	case "webp":
		args = append(args, "-quality", "90")
	}
	args = append(args, dst)
	return args
}

// ExportVideoClip 生成视频粗剪导出产物（FR2-039）。
// 首期策略：优先 stream copy（-c copy）保持原码流；若失败则回退到 H.264/AAC 重编码。
func ExportVideoClip(ctx context.Context, baseDir, spaceID string, mediaID, taskID int64, sourcePath string, params VideoClipParams, mediaDurationSec float64) (*VideoClipResult, error) {
	if err := ValidateVideoClipParams(params, mediaDurationSec, defaultClipMaxDurationSec); err != nil {
		return nil, err
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("源文件不可访问: %w", err)
	}
	if !IsFFmpegAvailable() {
		return nil, errors.New("ffmpeg 不可用，无法执行视频粗剪")
	}
	format, _ := SanitizeVideoFormat(params.Format)
	out := VideoClipOutputPath(baseDir, spaceID, mediaID, taskID, format)
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return nil, fmt.Errorf("创建导出目录失败: %w", err)
	}
	clipLen := params.EndSec - params.StartSec
	timeout := time.Duration(clipLen/3600.0*float64(videoClipTimeoutPerHour)) + 2*time.Minute
	if timeout < 2*time.Minute {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 优先尝试 stream copy。
	args := []string{
		"-y",
		"-v", "error",
		"-ss", strconv.FormatFloat(params.StartSec, 'f', 3, 64),
		"-i", sourcePath,
		"-t", strconv.FormatFloat(clipLen, 'f', 3, 64),
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		out,
	}
	cmd := exec.CommandContext(runCtx, GetFFmpegPath(), args...)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		// stream copy 失败（关键帧未对齐/容器不兼容等），回退到重编码。
		args2 := []string{
			"-y",
			"-v", "error",
			"-ss", strconv.FormatFloat(params.StartSec, 'f', 3, 64),
			"-i", sourcePath,
			"-t", strconv.FormatFloat(clipLen, 'f', 3, 64),
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
			"-c:a", "aac", "-b:a", "128k",
			"-movflags", "+faststart",
			out,
		}
		cmd2 := exec.CommandContext(runCtx, GetFFmpegPath(), args2...)
		if outBytes2, err2 := cmd2.CombinedOutput(); err2 != nil {
			_ = os.Remove(out)
			return nil, fmt.Errorf("视频粗剪失败: stream copy=%v, 重编码=%v, copy 输出: %s, encode 输出: %s",
				err, err2, strings.TrimSpace(string(outBytes)), strings.TrimSpace(string(outBytes2)))
		}
	}
	info, err := os.Stat(out)
	if err != nil {
		return nil, fmt.Errorf("读取导出产物失败: %w", err)
	}
	return &VideoClipResult{
		OutputPath:     out,
		OutputFilename: filepath.Base(out),
		Format:         format,
		SizeBytes:      info.Size(),
		DurationSec:    clipLen,
	}, nil
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}