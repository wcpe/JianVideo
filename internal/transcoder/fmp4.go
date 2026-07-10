package transcoder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OutputPath 表示一次转码输出的播放路径分发结果。
type OutputPath string

const (
	// OutputPathTS H.264 走现有 mpegts.js + MPEG-TS + HLS 路径（FR-06/16，分支实现不动）。
	OutputPathTS OutputPath = "ts"
	// OutputPathFMP4 非 H.264（h265/av1/vp9）走 fMP4/CMAF + 原生 MSE 路径（FR-51）。
	OutputPathFMP4 OutputPath = "fmp4"
)

// fMP4 产物的固定文件名（与前端契约一致，见 spec §3.4）。
const (
	// fmp4InitFilename init segment（含 moov，无媒体数据）文件名。
	fmp4InitFilename = "init.mp4"
	// fmp4ManifestFilename HLS-fMP4 清单文件名。
	fmp4ManifestFilename = "index.m3u8"
	// fmp4SegmentPattern media segment（.m4s）命名模板。
	fmp4SegmentPattern = "seg_%03d.m4s"
	// fmp4SegmentSeconds 单个 fMP4 分片目标时长（秒）。
	fmp4SegmentSeconds = "4"
)

// softwareFMP4Encoder 各高级编码的软件兜底编码器（无硬件可用时使用）。
var softwareFMP4Encoder = map[string]string{
	"h265": "libx265",
	"av1":  "libsvtav1",
	"vp9":  "libvpx-vp9",
}

// fmp4CodecMIME 各高级编码在 fMP4 容器下的 MSE codec MIME 串（前端 isTypeSupported 用）。
// 参数串为各编码的通用安全档：HEVC Main@L3.1、AV1 Main 8-bit、VP9 Profile0 8-bit。
var fmp4CodecMIME = map[string]string{
	"h265": `video/mp4; codecs="hvc1.1.6.L93.B0"`,
	"av1":  `video/mp4; codecs="av01.0.05M.08"`,
	"vp9":  `video/mp4; codecs="vp09.00.10.08"`,
}

// normalizeCodec 归一化目标编码标识：大小写无关，hevc 视为 h265。
func normalizeCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	if c == "hevc" {
		return "h265"
	}
	return c
}

// IsAdvancedCodec 判断目标编码是否为非 H.264 的高级编码（h265/av1/vp9）。
// h264、空串、未知编码均返回 false（保守走 H.264/TS 路径）。
func IsAdvancedCodec(codec string) bool {
	_, ok := softwareFMP4Encoder[normalizeCodec(codec)]
	return ok
}

// SelectOutputPath 分发点核心决策（纯函数）：按目标编码选择播放/转码输出路径。
// h264（含空、未知）→ TS 路径（现有 mpegts.js 不动）；h265/av1/vp9 → fMP4 路径。
func SelectOutputPath(codec string) OutputPath {
	if IsAdvancedCodec(codec) {
		return OutputPathFMP4
	}
	return OutputPathTS
}

// FMP4CodecMIME 返回高级编码在 fMP4 容器下的 MSE codec MIME 串；非高级编码返回空串。
func FMP4CodecMIME(codec string) string {
	return fmp4CodecMIME[normalizeCodec(codec)]
}

// SelectFMP4Encoder 为高级编码选取实测可用的 ffmpeg 编码器（纯函数）。
// 复用 FR-49 的 SelectEncoderForCodec 硬件优先级；无硬件可用时回软件兜底。
// 非高级编码（如 h264）返回 ok=false（不归 fMP4 路径管）。
func SelectFMP4Encoder(results []EncoderProbeResult, codec string) (encoder, deviceType string, ok bool) {
	c := normalizeCodec(codec)
	swFallback, isAdvanced := softwareFMP4Encoder[c]
	if !isAdvanced {
		return "", "", false
	}
	if enc, dev, found := SelectEncoderForCodec(results, c); found {
		return enc, dev, true
	}
	return swFallback, "", true
}

// SelectFMP4EncoderWithPolicy 为高级编码按用户硬件策略选取 ffmpeg 编码器。
func SelectFMP4EncoderWithPolicy(results []EncoderProbeResult, codec string, policy HardwarePolicy) (encoder, deviceType string, hardware bool, ok bool, err error) {
	c := normalizeCodec(codec)
	if !IsAdvancedCodec(c) {
		return "", "", false, false, nil
	}
	encoder, deviceType, hardware, err = SelectEncoderForCodecWithPolicy(results, c, policy)
	if err != nil {
		return "", "", false, false, err
	}
	return encoder, deviceType, hardware, true, nil
}

// BuildFMP4Args 构建 fMP4/CMAF 输出的 ffmpeg 命令行参数（纯函数）。
// 产出 HLS-fMP4（CMAF）：init.mp4 + seg_NNN.m4s + index.m3u8（VOD，含 EXT-X-MAP/ENDLIST）。
// VAAPI/Vulkan 使用显式设备初始化与上传规则，其余设备保持原有 -hwaccel 行为。
func BuildFMP4Args(inputPath, encoder, codec, deviceType string) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
	}
	args = appendHardwareInputArgs(args, deviceType)
	args = append(args, "-i", inputPath)
	args = appendHardwareUploadArgs(args, deviceType)

	args = append(args,
		"-map", "0:v:0",
		"-c:v", encoder,
		// 强制 8-bit yuv420p：与 TS 路径同策略，避免 10-bit 源解码兼容问题
		"-pix_fmt", "yuv420p",
	)
	// 仅 HEVC 打 hvc1 tag，保证 fMP4 内编码 tag 为 hvc1（Safari/MSE 兼容性优于 hev1）
	if normalizeCodec(codec) == "h265" {
		args = append(args, "-tag:v", "hvc1")
	}
	args = append(args,
		// 固定 GOP（与 TS 路径一致，便于分片边界对齐与 Seek）
		"-g", "48",
		"-keyint_min", "48",
		"-sc_threshold", "0",
		// 音频统一转 AAC：fMP4 不能 copy 裸流任意编码
		"-map", "0:a:0?",
		"-c:a", "aac",
		// fMP4/CMAF 封装：hls muxer + fmp4 分片，VOD 播放列表（本期不实时追播）
		"-f", "hls",
		"-hls_segment_type", "fmp4",
		"-hls_time", fmp4SegmentSeconds,
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_fmp4_init_filename", fmp4InitFilename,
		"-hls_segment_filename", fmp4SegmentPattern,
		fmp4ManifestFilename,
	)
	return args
}

// FMP4Result fMP4 输出执行结果摘要。
type FMP4Result struct {
	MediaID      int64
	OutputDir    string
	Codec        string
	Encoder      string
	ManifestPath string
	CodecMIME    string
}

// RunFMP4ToDir 为单个媒体文件同步执行 fMP4/CMAF 转码，输出产物到 outputDir。
// 选实测可用编码器 → 构建参数 → 跑外部 ffmpeg → 校验 init/manifest 产出。
// 失败时清理 outputDir 避免脏数据。codec 须为高级编码（h265/av1/vp9）。
func RunFMP4ToDir(ctx context.Context, mediaID int64, inputPath, codec, outputDir string, results []EncoderProbeResult) (*FMP4Result, error) {
	return RunFMP4ToDirWithPolicy(ctx, mediaID, inputPath, codec, outputDir, results, DefaultHardwarePolicy())
}

// RunFMP4ToDirWithPolicy 按硬件策略执行 fMP4/CMAF 转码。
func RunFMP4ToDirWithPolicy(ctx context.Context, mediaID int64, inputPath, codec, outputDir string, results []EncoderProbeResult, policy HardwarePolicy) (*FMP4Result, error) {
	if !IsFFmpegAvailable() {
		return nil, fmt.Errorf("ffmpeg 不可用，无法生成 fMP4 分片")
	}
	if outputDir == "" {
		return nil, fmt.Errorf("outputDir 不能为空")
	}
	encoder, deviceType, hardware, ok, err := SelectFMP4EncoderWithPolicy(results, codec, policy)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("编码 %q 不是受支持的高级编码（h265/av1/vp9）", codec)
	}

	if err := os.RemoveAll(outputDir); err != nil {
		return nil, fmt.Errorf("清理旧 fMP4 产物失败: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建 fMP4 输出目录失败: %w", err)
	}

	// 转码硬上限，避免卡死调用方
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	args := BuildFMP4Args(inputPath, encoder, codec, deviceType)
	if err := runFMP4FFmpeg(runCtx, args, outputDir); err != nil {
		_ = os.RemoveAll(outputDir)
		if hardware && policy.Fallback {
			log.Printf("[WARN] fMP4 硬件转码失败，改用软件回退: mediaID=%d, codec=%s, encoder=%s, err=%v",
				mediaID, codec, encoder, err)
			return RunFMP4ToDirWithPolicy(ctx, mediaID, inputPath, codec, outputDir, results, HardwarePolicy{Mode: HWAccelModeSoftware, Fallback: true})
		}
		log.Printf("[ERROR] fMP4 转码失败: mediaID=%d, codec=%s, encoder=%s, outputDir=%s, err=%v",
			mediaID, codec, encoder, outputDir, err)
		return nil, fmt.Errorf("fMP4 转码失败: %w", err)
	}

	if err := verifyFMP4Outputs(outputDir); err != nil {
		_ = os.RemoveAll(outputDir)
		log.Printf("[ERROR] fMP4 产物校验失败: mediaID=%d, codec=%s, outputDir=%s, err=%v",
			mediaID, codec, outputDir, err)
		return nil, err
	}

	log.Printf("[INFO] fMP4 转码完成: mediaID=%d, codec=%s, encoder=%s, outputDir=%s",
		mediaID, codec, encoder, outputDir)
	return &FMP4Result{
		MediaID:      mediaID,
		OutputDir:    outputDir,
		Codec:        normalizeCodec(codec),
		Encoder:      encoder,
		ManifestPath: filepath.Join(outputDir, fmp4ManifestFilename),
		CodecMIME:    FMP4CodecMIME(codec),
	}, nil
}

// runFMP4FFmpeg 在指定工作目录执行 fMP4 转码命令（进程组/取消语义与现有管道一致）。
func runFMP4FFmpeg(ctx context.Context, args []string, dir string) error {
	cmd := ffmpegCommandContext(ctx, args...)
	cmd.Stderr = &logWriter{prefix: "[ffmpeg-fmp4]"}
	cmd.WaitDelay = 5 * time.Second
	if dir != "" {
		cmd.Dir = dir
	}
	setProcessGroup(cmd)

	log.Printf("[INFO] 启动 fMP4 转码: ffmpeg %s", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 fMP4 ffmpeg 失败: %w", err)
	}
	err := cmd.Wait()
	if ctx.Err() != nil {
		if err != nil {
			log.Printf("[INFO] context 取消后 fMP4 ffmpeg 返回: %v", err)
		}
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("fMP4 ffmpeg 转码失败: %w", err)
	}
	return nil
}

// verifyFMP4Outputs 校验 ffmpeg 真的产出了 init segment 与清单。
func verifyFMP4Outputs(outputDir string) error {
	for _, name := range []string{fmp4InitFilename, fmp4ManifestFilename} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			return fmt.Errorf("fMP4 产物 %s 未生成（输出目录: %s）: %w", name, outputDir, err)
		}
	}
	return nil
}
