package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRawExtensionsRecognizedAsImage 验证常见 RAW 扩展名被识别为图片类型。
func TestRawExtensionsRecognizedAsImage(t *testing.T) {
	rawExts := []string{"cr2", "nef", "arw", "dng", "rw2", "raf", "orf", "srw", "pef"}
	for _, ext := range rawExts {
		path := "/data/photos/IMG_0001." + ext
		mediaType, ok := mediaTypeByExtension(ext)
		if !ok {
			t.Errorf("RAW 扩展名 %s 未被识别为内置媒体类型", ext)
			continue
		}
		if mediaType != MediaTypeImage {
			t.Errorf("RAW 扩展名 %s 媒体类型应为 %s，实际 %s", ext, MediaTypeImage, mediaType)
		}
		// 经由公开判断方法也应判为图片
		s := &Service{}
		if !s.IsImageFile(path) {
			t.Errorf("IsImageFile 应将 %s 判为图片", path)
		}
	}
}

// TestIsMagickConvertExt 验证哪些扩展名需要经 magick 转换。
func TestIsMagickConvertExt(t *testing.T) {
	// 需要转换：HEIC/HEIF 与 RAW 系列
	needConvert := []string{"heic", "heif", "cr2", "nef", "arw", "dng", "rw2", "raf", "orf", "srw", "pef"}
	for _, ext := range needConvert {
		if !isMagickConvertExt(ext) {
			t.Errorf("扩展名 %s 应需经 magick 转换", ext)
		}
		// 大小写、带点前缀均应归一化处理
		if !isMagickConvertExt("." + strings.ToUpper(ext)) {
			t.Errorf("扩展名 .%s（大写带点）应需经 magick 转换", strings.ToUpper(ext))
		}
	}

	// 浏览器可直显，不需转换
	noConvert := []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "", "mp4"}
	for _, ext := range noConvert {
		if isMagickConvertExt(ext) {
			t.Errorf("扩展名 %q 不应经 magick 转换", ext)
		}
	}
}

// TestBuildMagickConvertArgs 验证转换为 JPEG 的命令行参数构建。
func TestBuildMagickConvertArgs(t *testing.T) {
	args := buildMagickConvertArgs("/data/a.heic", "/cache/x.jpg")
	if len(args) == 0 {
		t.Fatal("转换参数不应为空")
	}
	// 源（取首帧 [0] 以避免多页 HEIC 输出多文件）必须在输出之前
	srcIdx, dstIdx := -1, -1
	for i, a := range args {
		if strings.HasPrefix(a, "/data/a.heic") {
			srcIdx = i
		}
		if a == "/cache/x.jpg" {
			dstIdx = i
		}
	}
	if srcIdx < 0 {
		t.Fatalf("参数中缺少源文件，args=%v", args)
	}
	if dstIdx < 0 {
		t.Fatalf("参数中缺少输出文件，args=%v", args)
	}
	if srcIdx >= dstIdx {
		t.Errorf("源文件应位于输出文件之前，args=%v", args)
	}
}

// TestBuildMagickThumbnailArgs 验证缩略图命令行参数构建含缩放宽度。
func TestBuildMagickThumbnailArgs(t *testing.T) {
	args := buildMagickThumbnailArgs("/data/a.cr2", "/thumb/y.jpg", 320)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "320") {
		t.Errorf("缩略图参数应包含缩放宽度 320，args=%v", args)
	}
	// 必须包含输出路径
	found := false
	for _, a := range args {
		if a == "/thumb/y.jpg" {
			found = true
		}
	}
	if !found {
		t.Errorf("缩略图参数应包含输出路径，args=%v", args)
	}
}

// TestConvertCacheKeyStable 验证缓存键：同源同修改时间稳定、源或时间变化则不同。
func TestConvertCacheKeyStable(t *testing.T) {
	k1 := convertCacheKey("/data/a.heic", 1000)
	k2 := convertCacheKey("/data/a.heic", 1000)
	if k1 != k2 {
		t.Errorf("同源同修改时间缓存键应一致：%s != %s", k1, k2)
	}
	if k1 == "" {
		t.Error("缓存键不应为空")
	}
	// 不同源路径 → 键不同
	if convertCacheKey("/data/b.heic", 1000) == k1 {
		t.Error("不同源路径应产生不同缓存键")
	}
	// 同源不同修改时间 → 键不同（源被替换后失效重转）
	if convertCacheKey("/data/a.heic", 2000) == k1 {
		t.Error("不同修改时间应产生不同缓存键")
	}
}

// TestConvertToJPEGSkipWithoutMagick magick 不可用时返回错误而非 panic。
func TestConvertToJPEGSkipWithoutMagick(t *testing.T) {
	if IsMagickAvailable() {
		t.Skip("本机已安装 magick，跳过不可用分支测试")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.heic")
	if err := os.WriteFile(src, []byte("not-a-real-heic"), 0o644); err != nil {
		t.Fatalf("写入测试源文件失败: %v", err)
	}
	InitConvertCacheDir(tmp)
	if _, err := ConvertToJPEG(src); err == nil {
		t.Error("magick 不可用时 ConvertToJPEG 应返回错误")
	}
}

// TestConvertToJPEGCacheHit 集成测试：有 magick 时验证 HEIC→JPEG 与缓存命中；无 magick 跳过。
func TestConvertToJPEGCacheHit(t *testing.T) {
	if !IsMagickAvailable() {
		t.Skip("未安装 magick，跳过 HEIC→JPEG 真实转换集成测试（真机维度）")
	}
	tmp := t.TempDir()
	InitConvertCacheDir(tmp)

	// 用 magick 现造一张 HEIC 源图（若该 magick 无 HEIC delegate 则跳过）
	src := filepath.Join(tmp, "src.heic")
	if err := runMagick(buildMagickConvertArgs("xc:red[1x1]", src)); err != nil {
		t.Skipf("当前 magick 无法生成 HEIC（缺 delegate），跳过：%v", err)
	}

	out1, err := ConvertToJPEG(src)
	if err != nil {
		t.Fatalf("HEIC→JPEG 转换失败: %v", err)
	}
	data, err := os.ReadFile(out1)
	if err != nil {
		t.Fatalf("读取转换产物失败: %v", err)
	}
	// JPEG magic bytes: FF D8 FF
	if len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
		t.Errorf("转换产物不是合法 JPEG，前 %d 字节=% x", min(3, len(data)), data[:min(3, len(data))])
	}

	// 二次转换应命中缓存，返回同一路径
	out2, err := ConvertToJPEG(src)
	if err != nil {
		t.Fatalf("二次转换失败: %v", err)
	}
	if out1 != out2 {
		t.Errorf("二次转换应命中缓存返回同一路径：%s != %s", out1, out2)
	}
}
