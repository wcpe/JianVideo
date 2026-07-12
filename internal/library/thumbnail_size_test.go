package library

import (
	"strings"
	"testing"
)

// TestThumbnailPathForSize_DefaultUnchanged 默认尺寸（320）的缓存路径必须与既有 getThumbnailPath 完全一致，
// 以保不破坏 dHash 去重（dhash.go 读固定缩略图路径）与 FR-73 健康巡检读取路径。
func TestThumbnailPathForSize_DefaultUnchanged(t *testing.T) {
	InitThumbnailDir(t.TempDir())
	const src = "/some/dir/photo.png"
	def := getThumbnailPath(src)
	sized := thumbnailPathForSize(src, thumbnailWidth)
	if def != sized {
		t.Fatalf("默认尺寸路径应与 getThumbnailPath 一致：want %s, got %s", def, sized)
	}
	if !strings.HasSuffix(def, ".jpg") {
		t.Fatalf("缩略图产物应恒为 .jpg：%s", def)
	}
}

// TestThumbnailPathForSize_DistinctPerSize 不同尺寸返回不同缓存路径，且非默认尺寸带尺寸后缀、与默认产物并存。
func TestThumbnailPathForSize_DistinctPerSize(t *testing.T) {
	InitThumbnailDir(t.TempDir())
	const src = "/some/dir/photo.png"
	def := thumbnailPathForSize(src, thumbnailWidth)
	small := thumbnailPathForSize(src, 160)
	large := thumbnailPathForSize(src, 640)
	if def == small || def == large || small == large {
		t.Fatalf("不同尺寸应映射不同路径：def=%s small=%s large=%s", def, small, large)
	}
	for _, p := range []string{small, large} {
		if !strings.HasSuffix(p, ".jpg") {
			t.Fatalf("缩略图产物应恒为 .jpg：%s", p)
		}
	}
	// 非默认尺寸路径应含尺寸标识，便于排查
	if !strings.Contains(small, "160") || !strings.Contains(large, "640") {
		t.Fatalf("非默认尺寸路径应含尺寸标识：small=%s large=%s", small, large)
	}
}

// TestNormalizeThumbnailSize 尺寸协商：白名单内原样、白名单外（含 0/负数）回落默认。
func TestNormalizeThumbnailSize(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{160, 160},
		{320, 320},
		{640, 640},
		{0, thumbnailWidth},
		{-1, thumbnailWidth},
		{500, thumbnailWidth},
		{99999, thumbnailWidth},
	}
	for _, c := range cases {
		if got := normalizeThumbnailSize(c.in); got != c.want {
			t.Fatalf("normalizeThumbnailSize(%d)=%d, want %d", c.in, got, c.want)
		}
	}
}

// TestBuildImageThumbnailArgs_MatteAndSize 图片缩略图参数应含中性灰底合成与目标尺寸缩放。
func TestBuildImageThumbnailArgs_MatteAndSize(t *testing.T) {
	args := buildImageThumbnailArgs("/in.png", "/out.jpg", 160)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, thumbnailMatteColor) {
		t.Fatalf("图片缩略图参数应含中性灰底色值 %s：%s", thumbnailMatteColor, joined)
	}
	if !strings.Contains(joined, "scale=160") {
		t.Fatalf("图片缩略图参数应按目标尺寸缩放：%s", joined)
	}
	if !strings.Contains(joined, "overlay") {
		t.Fatalf("图片缩略图应经 overlay 把 alpha 合成到灰底：%s", joined)
	}
}

// TestBuildVideoThumbnailArgs_MatteAndSize 视频缩略图参数应含中性灰底合成与目标尺寸缩放。
func TestBuildVideoThumbnailArgs_MatteAndSize(t *testing.T) {
	args := buildVideoThumbnailArgs("/in.mp4", "/out.jpg", 640)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, thumbnailMatteColor) {
		t.Fatalf("视频缩略图参数应含中性灰底色值 %s：%s", thumbnailMatteColor, joined)
	}
	if !strings.Contains(joined, "scale=640") {
		t.Fatalf("视频缩略图参数应按目标尺寸缩放：%s", joined)
	}
}

// TestBuildMagickThumbnailArgs_Matte magick 缩略图参数应含灰底 flatten，消除 HEIC/RAW 透明区纯黑。
func TestBuildMagickThumbnailArgs_Matte(t *testing.T) {
	args := buildMagickThumbnailArgs("/in.heic", "/out.jpg", 320)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-background") || !strings.Contains(joined, "-flatten") {
		t.Fatalf("magick 缩略图参数应含 -background ... -flatten 合成灰底：%s", joined)
	}
	if !strings.Contains(joined, thumbnailMatteColor) {
		t.Fatalf("magick 缩略图参数应含中性灰底色值 %s：%s", thumbnailMatteColor, joined)
	}
}

func TestSetFFmpegPath_缩略图使用运行期设置路径(t *testing.T) {
	previous := GetFFmpegPath()
	t.Cleanup(func() { SetFFmpegPath(previous) })

	SetFFmpegPath("D:/tools/custom-ffmpeg.exe")
	if got := GetFFmpegPath(); got != "D:/tools/custom-ffmpeg.exe" {
		t.Fatalf("缩略图 ffmpeg 路径未热应用: %s", got)
	}
}
