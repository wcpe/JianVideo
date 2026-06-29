package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// thumbnailFFmpegTimeout 单次 ffmpeg 缩略图命令超时，防止个别损坏文件挂死进程。
	thumbnailFFmpegTimeout = 30 * time.Second
	// thumbnailMaxConcurrency 缩略图生成并发上限的硬顶，避免扫描大目录瞬间炸出大量进程。
	thumbnailMaxConcurrency = 4
)

var (
	thumbnailDirMu sync.Once
	thumbnailDir   string

	// thumbnailSemOnce 确保信号量只初始化一次。
	thumbnailSemOnce sync.Once
	// thumbnailSem 固定容量信号量，限制并发生成的缩略图任务数。
	thumbnailSem chan struct{}
)

// thumbnailKind 缩略图任务类型，决定走哪条生成路径。
type thumbnailKind int

const (
	kindImage thumbnailKind = iota
	kindVideo
	kindMagick
)

// thumbnailJob 描述一个待生成的缩略图任务。
type thumbnailJob struct {
	filePath string
	kind     thumbnailKind
	// size 目标缩略图宽度（FR-81 P12 多尺寸）；默认 thumbnailWidth。
	size int
}

// thumbnailSizes 受支持的缩略图尺寸白名单（宽度，像素）。
// 默认 thumbnailWidth(320) 必须在内，且其缓存路径保持与历史一致（见 thumbnailPathForSize）。
var thumbnailSizes = map[int]bool{160: true, 320: true, 640: true}

// normalizeThumbnailSize 协商目标尺寸：白名单内原样返回，否则回落默认 thumbnailWidth。
func normalizeThumbnailSize(size int) int {
	if thumbnailSizes[size] {
		return size
	}
	return thumbnailWidth
}

// runThumbnail 执行实际的缩略图生成动作，抽成可注入的函数变量以便测试限并发行为。
var runThumbnail = realRunThumbnail

// runFFmpegThumbnail 执行一次缩略图 ffmpeg 命令并捕获 stderr，
// 抽成可注入的函数变量以便测试在不依赖真实 ffmpeg 的情况下注入失败桩。
// 失败时返回的错误已包含 stderr 关键尾部，便于上层按 ERROR 级别记录具体原因。
var runFFmpegThumbnail = realRunFFmpegThumbnail

// thumbnailStderrTailLimit 失败日志中保留的 ffmpeg stderr 尾部最大字符数。
const thumbnailStderrTailLimit = 500

// thumbnailConcurrency 返回缩略图生成并发上限：min(thumbnailMaxConcurrency, CPU 核数)。
func thumbnailConcurrency() int {
	n := runtime.NumCPU()
	if n > thumbnailMaxConcurrency {
		n = thumbnailMaxConcurrency
	}
	if n < 1 {
		n = 1
	}
	return n
}

// thumbnailSemaphore 惰性初始化并返回全局缩略图信号量。
func thumbnailSemaphore() chan struct{} {
	thumbnailSemOnce.Do(func() {
		thumbnailSem = make(chan struct{}, thumbnailConcurrency())
	})
	return thumbnailSem
}

// submitThumbnail 在固定容量信号量约束下异步执行一个缩略图任务，不阻塞调用方（扫描）。
func submitThumbnail(job thumbnailJob) {
	sem := thumbnailSemaphore()
	go func() {
		sem <- struct{}{}        // 获取令牌（超出上限时在此排队）
		defer func() { <-sem }() // 释放令牌
		runThumbnail(job)
	}()
}

// realRunThumbnail 按任务类型分派到具体的生成实现。
func realRunThumbnail(job thumbnailJob) {
	size := normalizeThumbnailSize(job.size)
	switch job.kind {
	case kindMagick:
		generateMagickThumbnail(job.filePath, size)
	case kindImage:
		generateImageThumbnail(job.filePath, size)
	case kindVideo:
		generateVideoThumbnail(job.filePath, size)
	}
}

// InitThumbnailDir 初始化缩略图存储目录（只需调用一次）。
func InitThumbnailDir(baseDir string) {
	thumbnailDirMu.Do(func() {
		thumbnailDir = filepath.Join(baseDir, "thumbnails")
		if err := os.MkdirAll(thumbnailDir, 0755); err != nil {
			log.Printf("[WARN] 创建缩略图目录失败: %s, err=%v", thumbnailDir, err)
		}
	})
}

// GetThumbnailDir 返回缩略图存储目录路径。
func GetThumbnailDir() string {
	return thumbnailDir
}

// GenerateThumbnail 根据文件类型异步生成缩略图。
// 所有任务统一经固定容量信号量限并发，避免扫描大目录时炸出大量并发进程导致资源耗尽。
func GenerateThumbnail(filePath string) {
	GenerateThumbnailSize(filePath, thumbnailWidth)
}

// GenerateThumbnailSize 按指定尺寸异步生成缩略图（FR-81 P12）。
// size 非白名单值回落默认尺寸；同步生成请走 generateThumbnailSync。
func GenerateThumbnailSize(filePath string, size int) {
	job, ok := resolveThumbnailJob(filePath)
	if !ok {
		return
	}
	job.size = normalizeThumbnailSize(size)
	submitThumbnail(job)
}

// generateThumbnailSync 同步生成一次缩略图（不经异步队列、不限并发），
// 供去重扫描（FR-70）在缩略图缺失时现场补齐后立即读取计算 dHash。
// 不支持的类型直接返回（与 GenerateThumbnail 的派发一致）。
func generateThumbnailSync(filePath string) {
	job, ok := resolveThumbnailJob(filePath)
	if !ok {
		return
	}
	runThumbnail(job)
}

// resolveThumbnailJob 按文件类型解析出缩略图任务，类型不支持时返回 ok=false。
// 抽出供异步（GenerateThumbnail）与同步（generateThumbnailSync）两条路径共用，避免派发逻辑重复。
func resolveThumbnailJob(filePath string) (thumbnailJob, bool) {
	// HEIC/RAW 浏览器无法直接渲染，经外部 magick 生成缩略图（FR-37）
	if needsMagickConvert(filePath) {
		return thumbnailJob{filePath: filePath, kind: kindMagick}, true
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return thumbnailJob{filePath: filePath, kind: kindImage}, true
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".ts", ".m4v", ".flv":
		return thumbnailJob{filePath: filePath, kind: kindVideo}, true
	}
	return thumbnailJob{}, false
}

// TryGenerateThumbnail 同步尝试生成缩略图并返回生成结果错误，供健康巡检判定缩略图是否可生成（FR-73）。
// 与异步 GenerateThumbnail 互不影响：异步路径失败只记日志，本入口把失败作为返回值上抛。
// 文件不存在 / 不是受支持的缩略图类型 / 生成失败时返回非 nil 错误。
func TryGenerateThumbnail(filePath string) error {
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("源文件不可访问: %w", err)
	}
	if needsMagickConvert(filePath) {
		outputPath := getThumbnailPath(filePath)
		return runMagick(buildMagickThumbnailArgs(filePath, outputPath, thumbnailWidth))
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return tryRunThumbnailFFmpeg(buildImageThumbnailArgs(filePath, getThumbnailPath(filePath), thumbnailWidth))
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".ts", ".m4v", ".flv":
		return tryRunThumbnailFFmpeg(buildVideoThumbnailArgs(filePath, getThumbnailPath(filePath), thumbnailWidth))
	}
	return fmt.Errorf("不支持的缩略图类型: %s", ext)
}

// tryRunThumbnailFFmpeg 带超时执行一次 ffmpeg 缩略图命令并返回错误（含超时区分），供同步生成入口复用。
func tryRunThumbnailFFmpeg(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailFFmpegTimeout)
	defer cancel()
	if err := runFFmpegThumbnail(ctx, args); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("缩略图生成超时: %w", err)
		}
		return err
	}
	return nil
}

// matteFilterComplex 构造「缩放至宽 width 后合成到中性灰底」的 ffmpeg filter_complex（FR-81 P1）。
// 以 color 源生成灰底，经 scale2ref 适配前景尺寸后 overlay 合成：
// 带 alpha 的源透明区透出灰底（消除纯黑），无 alpha 的源叠加灰底不可见、结果不变。
func matteFilterComplex(width int) string {
	w := strconv.Itoa(width)
	return "[0]scale=" + w + ":-1[fg];" +
		"[1][fg]scale2ref[bg][fg2];" +
		"[bg][fg2]overlay=format=auto"
}

// buildImageThumbnailArgs 构造图片缩略图的 ffmpeg 参数（缩放至 width 宽并合成中性灰底）。
func buildImageThumbnailArgs(filePath, outputPath string, width int) []string {
	return []string{
		"-i", filePath,
		"-f", "lavfi", "-i", "color=c=0x" + thumbnailMatteColor,
		"-filter_complex", matteFilterComplex(width),
		"-frames:v", "1", "-y", outputPath,
	}
}

// buildVideoThumbnailArgs 构造视频缩略图的 ffmpeg 参数（取第 2 秒帧、缩放至 width 宽并合成中性灰底）。
func buildVideoThumbnailArgs(filePath, outputPath string, width int) []string {
	return []string{
		"-ss", "00:00:02", "-i", filePath,
		"-f", "lavfi", "-i", "color=c=0x" + thumbnailMatteColor,
		"-filter_complex", matteFilterComplex(width),
		"-frames:v", "1", "-y", outputPath,
	}
}

// generateMagickThumbnail 用 ImageMagick 为 HEIC/RAW 生成缩略图。
// magick 不可用或转换失败仅记日志，不阻塞入库（与 ffmpeg 缩略图一致）。
func generateMagickThumbnail(filePath string, size int) {
	outputPath := thumbnailPathForSize(filePath, size)
	if err := runMagick(buildMagickThumbnailArgs(filePath, outputPath, size)); err != nil {
		log.Printf("[WARN] HEIC/RAW 缩略图生成失败: %s, err=%v", filePath, err)
	}
}

func generateImageThumbnail(filePath string, size int) {
	outputPath := thumbnailPathForSize(filePath, size)
	// 使用 ffmpeg 缩放图片（比引入 imaging 库更轻量，项目已依赖 ffmpeg）
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailFFmpegTimeout)
	defer cancel()
	args := buildImageThumbnailArgs(filePath, outputPath, size)
	if err := runFFmpegThumbnail(ctx, args); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[WARN] 图片缩略图生成超时（已终止 ffmpeg）: %s", filePath)
			return
		}
		// 记录 ffmpeg stderr 关键尾部，便于定位具体失败原因（如格式不支持、文件损坏）
		log.Printf("[ERROR] 图片缩略图生成失败: %s, err=%v", filePath, err)
	}
}

func generateVideoThumbnail(filePath string, size int) {
	outputPath := thumbnailPathForSize(filePath, size)
	// 提取第 2 秒帧（第 1 秒常是黑屏）
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailFFmpegTimeout)
	defer cancel()
	args := buildVideoThumbnailArgs(filePath, outputPath, size)
	if err := runFFmpegThumbnail(ctx, args); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[WARN] 视频缩略图生成超时（已终止 ffmpeg）: %s", filePath)
			return
		}
		// 记录 ffmpeg stderr 关键尾部，便于定位具体失败原因（如格式不支持、文件损坏）
		log.Printf("[ERROR] 视频缩略图生成失败: %s, err=%v", filePath, err)
	}
}

// realRunFFmpegThumbnail 用给定参数执行 ffmpeg 缩略图命令，捕获 stderr。
// 命令失败时返回的错误包含 stderr 关键尾部（截断至 thumbnailStderrTailLimit 字符），
// 以便上层日志说明「为什么失败」，而非仅一句 exit status。
func realRunFFmpegThumbnail(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if tail := tailString(stderr.String(), thumbnailStderrTailLimit); tail != "" {
			return fmt.Errorf("%w; ffmpeg stderr: %s", err, tail)
		}
		return err
	}
	return nil
}

// tailString 返回字符串末尾至多 n 个字符（用于截断 ffmpeg stderr）。
func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func getThumbnailPath(filePath string) string {
	return thumbnailPathForSize(filePath, thumbnailWidth)
}

// thumbnailPathForSize 返回指定尺寸的缩略图缓存路径（FR-81 P12）。
// 默认尺寸 thumbnailWidth 的路径保持历史命名（无尺寸后缀），以不破坏 dHash 去重（dhash.go）
// 与 FR-73 健康巡检读取的固定缩略图路径；非默认尺寸用带尺寸后缀的新名，与默认产物并存。
func thumbnailPathForSize(filePath string, size int) string {
	// 用文件路径的 hash 作为缩略图名，避免特殊字符
	hash := sha256.Sum256([]byte(filePath))
	name := hex.EncodeToString(hash[:16])
	if size != thumbnailWidth {
		name = name + "_" + strconv.Itoa(size)
	}
	return filepath.Join(thumbnailDir, name+".jpg")
}

// ThumbnailPathForSize 按尺寸返回缩略图缓存路径（供 api 层 size 协商）。
// 非白名单尺寸回落默认尺寸。
func ThumbnailPathForSize(filePath string, size int) string {
	return thumbnailPathForSize(filePath, normalizeThumbnailSize(size))
}

// FindThumbnailPath 根据原始文件路径查找对应的默认尺寸缩略图路径。
func FindThumbnailPath(filePath string) string {
	return getThumbnailPath(filePath)
}

// NormalizeThumbnailSize 协商缩略图尺寸（供 api 层校验请求参数）。
func NormalizeThumbnailSize(size int) int {
	return normalizeThumbnailSize(size)
}
