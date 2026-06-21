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
	switch job.kind {
	case kindMagick:
		generateMagickThumbnail(job.filePath)
	case kindImage:
		generateImageThumbnail(job.filePath)
	case kindVideo:
		generateVideoThumbnail(job.filePath)
	}
}

// InitThumbnailDir 初始化缩略图存储目录（只需调用一次）。
func InitThumbnailDir(baseDir string) {
	thumbnailDirMu.Do(func() {
		thumbnailDir = filepath.Join(baseDir, "thumbnails")
		os.MkdirAll(thumbnailDir, 0755)
	})
}

// GetThumbnailDir 返回缩略图存储目录路径。
func GetThumbnailDir() string {
	return thumbnailDir
}

// GenerateThumbnail 根据文件类型异步生成缩略图。
// 所有任务统一经固定容量信号量限并发，避免扫描大目录时炸出大量并发进程导致资源耗尽。
func GenerateThumbnail(filePath string) {
	// HEIC/RAW 浏览器无法直接渲染，经外部 magick 生成缩略图（FR-37）
	if needsMagickConvert(filePath) {
		submitThumbnail(thumbnailJob{filePath: filePath, kind: kindMagick})
		return
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		submitThumbnail(thumbnailJob{filePath: filePath, kind: kindImage})
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".ts", ".m4v", ".flv":
		submitThumbnail(thumbnailJob{filePath: filePath, kind: kindVideo})
	}
}

// generateMagickThumbnail 用 ImageMagick 为 HEIC/RAW 生成缩略图。
// magick 不可用或转换失败仅记日志，不阻塞入库（与 ffmpeg 缩略图一致）。
func generateMagickThumbnail(filePath string) {
	outputPath := getThumbnailPath(filePath)
	if err := runMagick(buildMagickThumbnailArgs(filePath, outputPath, thumbnailWidth)); err != nil {
		log.Printf("[WARN] HEIC/RAW 缩略图生成失败: %s, err=%v", filePath, err)
	}
}

func generateImageThumbnail(filePath string) {
	outputPath := getThumbnailPath(filePath)
	// 使用 ffmpeg 缩放图片（比引入 imaging 库更轻量，项目已依赖 ffmpeg）
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailFFmpegTimeout)
	defer cancel()
	args := []string{"-i", filePath, "-vf", "scale=320:-1", "-vframes", "1", "-y", outputPath}
	if err := runFFmpegThumbnail(ctx, args); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[WARN] 图片缩略图生成超时（已终止 ffmpeg）: %s", filePath)
			return
		}
		// 记录 ffmpeg stderr 关键尾部，便于定位具体失败原因（如格式不支持、文件损坏）
		log.Printf("[ERROR] 图片缩略图生成失败: %s, err=%v", filePath, err)
	}
}

func generateVideoThumbnail(filePath string) {
	outputPath := getThumbnailPath(filePath)
	// 提取第 2 秒帧（第 1 秒常是黑屏）
	ctx, cancel := context.WithTimeout(context.Background(), thumbnailFFmpegTimeout)
	defer cancel()
	args := []string{"-i", filePath, "-ss", "00:00:02", "-vframes", "1", "-vf", "scale=320:-1", "-y", outputPath}
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
	// 用文件路径的 hash 作为缩略图名，避免特殊字符
	hash := sha256.Sum256([]byte(filePath))
	name := hex.EncodeToString(hash[:16])
	return filepath.Join(thumbnailDir, name+".jpg")
}

// FindThumbnailPath 根据原始文件路径查找对应的缩略图路径。
func FindThumbnailPath(filePath string) string {
	return getThumbnailPath(filePath)
}
