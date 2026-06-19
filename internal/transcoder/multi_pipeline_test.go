package transcoder

import (
	"strings"
	"testing"
)

// TestQualitiesForResolution 验证码率阶梯自动裁剪。
func TestQualitiesForResolution(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		want   []string
	}{
		{"1080p 源输出全部三档", 1920, 1080, []string{"1080p", "720p", "480p"}},
		{"720p 源只输出 720p+480p", 1280, 720, []string{"720p", "480p"}},
		{"480p 源只输出 480p", 854, 480, []string{"480p"}},
		{"360p 源只输出 480p", 640, 360, []string{"480p"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QualitiesForResolution(tt.width, tt.height)
			if len(got) != len(tt.want) {
				t.Fatalf("期望 %v, 实际 %v", tt.want, got)
			}
			for i, q := range tt.want {
				if got[i] != q {
					t.Fatalf("期望 %v, 实际 %v", tt.want, got)
				}
			}
		})
	}
}

// TestMultiPipeline_BuildArgs 验证多码率 ffmpeg 参数构建。
func TestMultiPipeline_BuildArgs(t *testing.T) {
	p := NewPipeline()
	mp := NewMultiPipeline(p)

	args := mp.BuildArgs("/tmp/test.mkv", []string{"1080p", "720p", "480p"})

	// 验证 filter_complex 中包含 split=3
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "split=3") {
		t.Fatalf("参数中未找到 split=3, 实际: %v", args)
	}

	// 验证每个码率都有 scale 和输出映射（嵌入在 filter_complex 中）
	if !strings.Contains(joined, "scale=1920:1080") {
		t.Fatalf("参数中未找到 scale=1920:1080, 实际: %v", args)
	}
	if !strings.Contains(joined, "scale=1280:720") {
		t.Fatalf("参数中未找到 scale=1280:720, 实际: %v", args)
	}
	if !strings.Contains(joined, "scale=854:480") {
		t.Fatalf("参数中未找到 scale=854:480, 实际: %v", args)
	}

	// 验证 GOP 参数
	assertContains(t, args, "-g", "48")
	assertContains(t, args, "-keyint_min", "48")
	assertContains(t, args, "-sc_threshold", "0")

	// 验证 HLS 输出
	if !strings.Contains(strings.Join(args, " "), "-f hls") {
		t.Fatal("应包含 -f hls 参数")
	}

	// 验证独立片段标志
	assertContains(t, args, "-hls_flags", "independent_segments")
}

// TestMultiPipeline_BuildArgs_Cropped 验证低分辨率源的码率裁剪。
func TestMultiPipeline_BuildArgs_Cropped(t *testing.T) {
	p := NewPipeline()
	mp := NewMultiPipeline(p)

	args := mp.BuildArgs("/tmp/test.mkv", []string{"720p", "480p"})

	// split=2
	if !strings.Contains(strings.Join(args, " "), "split=2") {
		t.Fatal("720p 源应 split=2")
	}

	// 不应包含 1080p scale
	if strings.Contains(strings.Join(args, " "), "scale=1920:1080") {
		t.Fatal("720p 源不应包含 1080p 输出")
	}
}
