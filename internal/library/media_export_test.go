package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateImageParams(t *testing.T) {
	cases := []struct {
		name    string
		params  ImageExportParams
		wantErr bool
	}{
		{"ok jpeg", ImageExportParams{Exposure: 10, Contrast: 0, Saturation: 0, Temp: 0, Format: "jpeg"}, false},
		{"ok png", ImageExportParams{Exposure: 0, Contrast: -5, Saturation: 0, Temp: 0, Format: "png"}, false},
		{"bad format", ImageExportParams{Format: "gif"}, true},
		{"out of range exposure", ImageExportParams{Exposure: 200, Format: "jpeg"}, true},
		{"out of range temp", ImageExportParams{Temp: 200, Format: "webp"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateImageParams(c.params)
			if (err != nil) != c.wantErr {
				t.Fatalf("期望错误=%v 实际=%v", c.wantErr, err)
			}
		})
	}
}

func TestValidateVideoClipParams(t *testing.T) {
	if err := ValidateVideoClipParams(VideoClipParams{StartSec: -1, EndSec: 2, Format: "mp4"}, 0, 0); err == nil {
		t.Fatal("负起始时间应报错")
	}
	if err := ValidateVideoClipParams(VideoClipParams{StartSec: 5, EndSec: 5, Format: "mp4"}, 0, 0); err == nil {
		t.Fatal("起止相等应报错")
	}
	if err := ValidateVideoClipParams(VideoClipParams{StartSec: 0, EndSec: 10, Format: "avi"}, 0, 0); err == nil {
		t.Fatal("不支持的格式应报错")
	}
	if err := ValidateVideoClipParams(VideoClipParams{StartSec: 0, EndSec: 10, Format: "mp4"}, 5, 0); err == nil {
		t.Fatal("结束超过媒体时长应报错")
	}
	if err := ValidateVideoClipParams(VideoClipParams{StartSec: 0, EndSec: 100, Format: "mp4"}, 0, 50); err == nil {
		t.Fatal("超过最大时长应报错")
	}
	if err := ValidateVideoClipParams(VideoClipParams{StartSec: 0, EndSec: 10, Format: "mp4"}, 0, 0); err != nil {
		t.Fatalf("合法参数应通过, 实际: %v", err)
	}
}

func TestSanitizeImageFormat(t *testing.T) {
	cases := map[string]struct {
		in       string
		wantOK   bool
		wantNorm string
	}{
		"jpeg": {"jpeg", true, "jpeg"},
		"jpg":  {"jpg", true, "jpg"},
		"png":  {"png", true, "png"},
		"webp": {"webp", true, "webp"},
		"image/jpeg": {"image/jpeg", true, "jpeg"},
		"gif":  {"gif", false, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := SanitizeImageFormat(c.in)
			if ok != c.wantOK {
				t.Fatalf("期望 ok=%v 实际 %v", c.wantOK, ok)
			}
			if got != c.wantNorm {
				t.Fatalf("期望归一化=%q 实际 %q", c.wantNorm, got)
			}
		})
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a := ImageExportFingerprint("space-default", 1, ImageExportParams{Exposure: 1.2, Format: "jpeg"})
	b := ImageExportFingerprint("space-default", 1, ImageExportParams{Exposure: 2.5, Format: "jpeg"})
	if a == b {
		t.Fatalf("不同参数应产生不同指纹")
	}
	if ImageExportFingerprint("space-default", 1, ImageExportParams{Exposure: 1.2, Format: "jpeg"}) != a {
		t.Fatalf("相同参数应产生稳定指纹")
	}
	c := VideoClipFingerprint("space-default", 2, VideoClipParams{StartSec: 0, EndSec: 5.001, Format: "mp4"})
	d := VideoClipFingerprint("space-default", 2, VideoClipParams{StartSec: 0, EndSec: 5.0, Format: "mp4"})
	if c != d {
		t.Fatalf("视频粗剪指纹应忽略亚毫秒差异")
	}
	if VideoClipFingerprint("space-default", 2, VideoClipParams{StartSec: 0, EndSec: 6, Format: "mp4"}) == c {
		t.Fatalf("不同起止应产生不同指纹")
	}
}

func TestOutputPath(t *testing.T) {
	p := ImageExportOutputPath("/tmp/data", "space-default", 1, 2, "png")
	want := filepath.Join("/tmp/data", "exports", "space-default", "1", "2.png")
	if p != want {
		t.Fatalf("输出路径错误：%s vs %s", p, want)
	}
}

func TestExportImageMagickUnavailable(t *testing.T) {
	prev := magickPath
	magickPath = "jianvideo-nonexistent-magick-xyz"
	defer func() { magickPath = prev }()
	_, err := ExportImage(context.Background(), t.TempDir(), "space-default", 1, 1, "/dev/null", ImageExportParams{Format: "png"})
	if err == nil {
		t.Fatal("magick 不可用时应报错")
	}
}

func TestExportVideoClipFFmpegUnavailable(t *testing.T) {
	prev := thumbnailFFmpegPath
	thumbnailFFmpegPath = "jianvideo-nonexistent-ffmpeg-xyz"
	defer func() { thumbnailFFmpegPath = prev }()
	_, err := ExportVideoClip(context.Background(), t.TempDir(), "space-default", 1, 1, "/dev/null",
		VideoClipParams{StartSec: 0, EndSec: 1, Format: "mp4"}, 10)
	if err == nil {
		t.Fatal("ffmpeg 不可用时应报错")
	}
}

func TestExportImageSourceMissing(t *testing.T) {
	// 让 magick 可用但源文件不存在，应在工具链校验前就返回文件不可访问。
	prev := magickPath
	magickPath = "/bin/true"
	defer func() { magickPath = prev }()
	if _, err := os.Stat("/bin/true"); err != nil {
		t.Skip("当前环境没有 /bin/true")
	}
	_, err := ExportImage(context.Background(), t.TempDir(), "space-default", 1, 1, "/jianvideo-no-such-file",
		ImageExportParams{Format: "png"})
	if err == nil {
		t.Fatal("源文件不存在应报错")
	}
}

func TestInitExportDir(t *testing.T) {
	base := t.TempDir()
	InitExportDir(base)
	if _, err := os.Stat(ExportDir(base)); err != nil {
		t.Fatalf("导出目录应创建, err=%v", err)
	}
}