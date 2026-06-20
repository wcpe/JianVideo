package library

import (
	"log"
	"os/exec"
	"path/filepath"
	"strings"
)

const thumbnailDir = "./thumbnails"
const thumbnailWidth = 320

// generateThumbnail 根据文件类型分发到对应的缩略图生成函数。
// 异步调用，不阻塞入库流程。
func generateThumbnail(filePath string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		generateImageThumbnail(filePath)
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".ts":
		generateVideoThumbnail(filePath)
	}
}

// generateImageThumbnail 生成图片缩略图（待实现）。
func generateImageThumbnail(filePath string) {
	// TODO: 使用 imaging 库或 ffmpeg 生成缩略图
	log.Printf("[Thumbnail] 图片缩略图待实现: %s", filePath)
}

// generateVideoThumbnail 使用 ffmpeg 提取第 1 秒帧作为视频缩略图。
func generateVideoThumbnail(filePath string) {
	// ffmpeg -i input.mp4 -ss 00:00:01 -vframes 1 -vf "scale=320:-1" -y thumb.jpg
	outputPath := filepath.Join(thumbnailDir, filepath.Base(filePath)+".jpg")
	cmd := exec.Command("ffmpeg", "-i", filePath, "-ss", "00:00:01", "-vframes", "1", "-vf", "scale=320:-1", "-y", outputPath)
	if err := cmd.Run(); err != nil {
		log.Printf("[Thumbnail] 视频缩略图生成失败: %s, err=%v", filePath, err)
	}
}
