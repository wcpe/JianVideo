package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 经 ImageMagick 转 JPEG 的相关常量与状态（FR-37）。
// HEIC/RAW 浏览器无法直接渲染，统一经外部 magick 进程转成 JPEG，与 ffmpeg 外部进程范式一致（见 ADR）。

const (
	// magickConvertTimeout 单次 magick 转换超时（RAW 解码可能较慢，给足时间）。
	magickConvertTimeout = 30 * time.Second
	// thumbnailWidth 缩略图缩放宽度（与 ffmpeg 图片缩略图一致）。
	thumbnailWidth = 320
)

// magickConvertExts 需要经 magick 转换为 JPEG 的源扩展名集合（不含点、小写）。
var magickConvertExts = map[string]bool{
	"heic": true, "heif": true,
	"cr2": true, "nef": true, "arw": true, "dng": true,
	"rw2": true, "raf": true, "orf": true, "srw": true, "pef": true,
}

// magickPath 全局 magick 可执行文件路径，默认 "magick"（PATH 查找）。
// 由 main.go 按「环境变量 → 同目录捆绑版 → PATH」注入（见 SetMagickPath）。
var magickPath = "magick"

// SetMagickPath 显式设置 magick 可执行文件路径（绝对或相对均可）。
// 通常由 main.go 从环境变量 JIANVIDEO_MAGICK_PATH 或同目录捆绑版注入。
func SetMagickPath(path string) {
	if path != "" {
		magickPath = path
	}
}

// GetMagickPath 返回当前 magick 可执行文件路径。
func GetMagickPath() string {
	return magickPath
}

// IsMagickAvailable 检查当前配置的 magick 是否可执行。
func IsMagickAvailable() bool {
	if magickPath == "" {
		return false
	}
	if _, err := os.Stat(magickPath); err == nil {
		return true
	}
	// 退化到 PATH 查找
	_, err := exec.LookPath(magickPath)
	return err == nil
}

var (
	convertCacheDirOnce sync.Once
	convertCacheDir     string
)

// InitConvertCacheDir 初始化 HEIC/RAW 转换结果缓存目录（数据目录下 image_cache/）。
// 只需调用一次。
func InitConvertCacheDir(baseDir string) {
	convertCacheDirOnce.Do(func() {
		convertCacheDir = filepath.Join(baseDir, "image_cache")
		if err := os.MkdirAll(convertCacheDir, 0o755); err != nil {
			// 目录创建失败不致命：后续转换会因写盘失败而降级并记日志
			log.Printf("[WARN] 初始化图片转换缓存目录失败: dir=%s, err=%v", convertCacheDir, err)
		}
	})
}

// normalizeExt 归一化扩展名：去前导点、转小写、去首尾空白。
func normalizeExt(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	return strings.TrimPrefix(ext, ".")
}

// isMagickConvertExt 判断给定扩展名是否需要经 magick 转换为 JPEG。
// 入参可带或不带前导点、可大小写混用。
func isMagickConvertExt(ext string) bool {
	return magickConvertExts[normalizeExt(ext)]
}

// needsMagickConvert 判断文件路径对应的源文件是否需要经 magick 转换。
func needsMagickConvert(filePath string) bool {
	return isMagickConvertExt(filepath.Ext(filePath))
}

// NeedsImageConvert 判断文件是否为需经 magick 转 JPEG 的 HEIC/RAW（供 api 层调用）。
func NeedsImageConvert(filePath string) bool {
	return needsMagickConvert(filePath)
}

// buildMagickConvertArgs 构建「源 → JPEG」的 magick 命令行参数。
// 取首帧 [0]，避免多页 HEIC 输出多个文件；保持纵横比，自动定向 EXIF。
// 注意：ImageMagick 7 的 magick 按左到右解析，-auto-orient 等图像算子必须在输入图像之后，
// 否则报「no images found for operation」。故输入在前、算子在后。
func buildMagickConvertArgs(src, dst string) []string {
	return []string{
		src + "[0]",
		"-auto-orient",
		dst,
	}
}

// buildMagickThumbnailArgs 构建「源 → 缩略图 JPEG」的 magick 命令行参数。
// 缩放为 width 宽、等比高度（width x，> 表示仅缩小不放大）。
// 同上：输入在前，-auto-orient / -thumbnail 算子在后（ImageMagick 7 要求）。
func buildMagickThumbnailArgs(src, dst string, width int) []string {
	return []string{
		src + "[0]",
		"-auto-orient",
		"-thumbnail", strconv.Itoa(width) + "x>",
		dst,
	}
}

// convertCacheKey 按「源路径 + 源修改时间(Unix 秒)」计算缓存键。
// 源被替换（mtime 变化）后键自动变化，旧缓存自然失效重转。
func convertCacheKey(srcPath string, modUnix int64) string {
	h := sha256.Sum256([]byte(srcPath + "|" + strconv.FormatInt(modUnix, 10)))
	return hex.EncodeToString(h[:16])
}

// cacheJPEGPath 返回某源文件转换结果在缓存目录中的目标路径。
func cacheJPEGPath(srcPath string, modUnix int64) string {
	return filepath.Join(convertCacheDir, convertCacheKey(srcPath, modUnix)+".jpg")
}

// runMagick 执行一次 magick 命令（带超时）。
func runMagick(args []string) error {
	if !IsMagickAvailable() {
		return fmt.Errorf("magick 不可用（路径: %s）", magickPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), magickConvertTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, magickPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("magick 执行失败: %w, 输出: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ConvertToJPEG 把 HEIC/RAW 源文件转换为 JPEG，返回缓存中的 JPEG 路径。
// 命中缓存（同源同修改时间）直接返回既有产物，不重复调用 magick。
func ConvertToJPEG(srcPath string) (string, error) {
	if convertCacheDir == "" {
		return "", fmt.Errorf("图片转换缓存目录未初始化")
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("读取源文件失败: %w", err)
	}
	dst := cacheJPEGPath(srcPath, info.ModTime().Unix())

	// 命中缓存直接返回
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}

	if err := runMagick(buildMagickConvertArgs(srcPath, dst)); err != nil {
		// 转换失败时清理可能产生的半成品，避免下次误命中
		_ = os.Remove(dst)
		return "", fmt.Errorf("HEIC/RAW 转 JPEG 失败: %w", err)
	}
	return dst, nil
}
