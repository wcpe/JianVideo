package library

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	thumbnailDirMu sync.Once
	thumbnailDir   string
)

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
func GenerateThumbnail(filePath string) {
	// HEIC/RAW 浏览器无法直接渲染，经外部 magick 生成缩略图（FR-37）
	if needsMagickConvert(filePath) {
		go generateMagickThumbnail(filePath)
		return
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		go generateImageThumbnail(filePath)
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".ts", ".m4v", ".flv":
		go generateVideoThumbnail(filePath)
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
	cmd := exec.Command("ffmpeg", "-i", filePath, "-vf", "scale=320:-1", "-vframes", "1", "-y", outputPath)
	if err := cmd.Run(); err != nil {
		log.Printf("[Thumbnail] 图片缩略图生成失败: %s, err=%v", filePath, err)
	}
}

func generateVideoThumbnail(filePath string) {
	outputPath := getThumbnailPath(filePath)
	// 提取第 2 秒帧（第 1 秒常是黑屏）
	cmd := exec.Command("ffmpeg", "-i", filePath, "-ss", "00:00:02", "-vframes", "1", "-vf", "scale=320:-1", "-y", outputPath)
	if err := cmd.Run(); err != nil {
		log.Printf("[Thumbnail] 视频缩略图生成失败: %s, err=%v", filePath, err)
	}
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
