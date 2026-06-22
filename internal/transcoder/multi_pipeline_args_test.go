package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQualitiesForResolution_1080p(t *testing.T) {
	result := QualitiesForResolution(1920, 1080)
	assert.Equal(t, []string{"1080p", "720p", "480p"}, result)
}

func TestQualitiesForResolution_720p(t *testing.T) {
	result := QualitiesForResolution(1280, 720)
	assert.Equal(t, []string{"720p", "480p"}, result)
}

func TestQualitiesForResolution_480p(t *testing.T) {
	result := QualitiesForResolution(854, 480)
	assert.Equal(t, []string{"480p"}, result)
}

func TestQualitiesForResolution_LowRes(t *testing.T) {
	// 低于最低档时兜底返回最低档
	result := QualitiesForResolution(640, 360)
	assert.Equal(t, []string{"480p"}, result)
}

func TestQualitiesForResolution_4K(t *testing.T) {
	// 4K 源只输出不高于源的档位（1080p 及以下）
	result := QualitiesForResolution(3840, 2160)
	assert.Equal(t, []string{"1080p", "720p", "480p"}, result)
}

func TestBuildArgs_SingleQuality(t *testing.T) {
	p := &Pipeline{encoderName: "libx264"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/test.mp4", []string{"1080p"})

	assert.Contains(t, args, "-filter_complex")

	// 提取 filter_complex 参数值
	filterIdx := -1
	for i, a := range args {
		if a == "-filter_complex" {
			filterIdx = i
			break
		}
	}
	assert.True(t, filterIdx >= 0, "应包含 -filter_complex 参数")

	filterValue := args[filterIdx+1]
	assert.Contains(t, filterValue, "split=1")
	assert.Contains(t, filterValue, "scale=1920:1080")

	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "-f")
	assert.Contains(t, args, "hls")

	// 验证 m3u8 输出文件名
	foundM3u8 := false
	for _, a := range args {
		if a == "1080p.m3u8" {
			foundM3u8 = true
			break
		}
	}
	assert.True(t, foundM3u8, "应包含 1080p.m3u8 输出文件")
}

func TestBuildArgs_MultiQuality(t *testing.T) {
	p := &Pipeline{encoderName: "libx264"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/test.mp4", []string{"1080p", "720p", "480p"})

	// 提取 filter_complex 参数值
	filterIdx := -1
	for i, a := range args {
		if a == "-filter_complex" {
			filterIdx = i
			break
		}
	}
	assert.True(t, filterIdx >= 0, "应包含 -filter_complex 参数")

	filterValue := args[filterIdx+1]

	// filter_complex 应包含 split=3
	assert.Contains(t, filterValue, "split=3")

	// 验证各档位 scale 参数（含 format=yuv420p 强制 8-bit，回归 10-bit High 10 不可播放）
	assert.Contains(t, filterValue, "[v1]scale=1920:1080,format=yuv420p[v1out]")
	assert.Contains(t, filterValue, "[v2]scale=1280:720,format=yuv420p[v2out]")
	assert.Contains(t, filterValue, "[v3]scale=854:480,format=yuv420p[v3out]")

	// 验证各档位的 m3u8 输出文件
	found1080p := false
	found720p := false
	found480p := false
	for _, a := range args {
		switch a {
		case "1080p.m3u8":
			found1080p = true
		case "720p.m3u8":
			found720p = true
		case "480p.m3u8":
			found480p = true
		}
	}
	assert.True(t, found1080p, "应包含 1080p.m3u8")
	assert.True(t, found720p, "应包含 720p.m3u8")
	assert.True(t, found480p, "应包含 480p.m3u8")
}

func TestBuildArgs_WithHwAccel(t *testing.T) {
	p := &Pipeline{encoderName: "h264_nvenc", hwAccel: "cuda"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/test.mp4", []string{"1080p"})

	// 验证包含硬件加速参数
	hwAccelIdx := -1
	inputIdx := -1
	for i, a := range args {
		if a == "-hwaccel" {
			hwAccelIdx = i
		}
		if a == "-i" {
			inputIdx = i
		}
	}

	assert.True(t, hwAccelIdx >= 0, "应包含 -hwaccel 参数")
	assert.Equal(t, "cuda", args[hwAccelIdx+1], "硬件加速类型应为 cuda")
	assert.True(t, inputIdx >= 0, "应包含 -i 参数")
	assert.True(t, hwAccelIdx < inputIdx, "-hwaccel 应在 -i 之前")
}

func TestBuildArgs_FilterComplexFormat(t *testing.T) {
	p := &Pipeline{encoderName: "libx264"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/test.mp4", []string{"1080p", "720p", "480p"})

	// 提取 filter_complex 参数值
	filterIdx := -1
	for i, a := range args {
		if a == "-filter_complex" {
			filterIdx = i
			break
		}
	}
	assert.True(t, filterIdx >= 0, "应包含 -filter_complex 参数")

	filterValue := args[filterIdx+1]

	// 验证分号分隔的 filter chain 格式
	chains := strings.Split(filterValue, "; ")
	assert.Len(t, chains, 4, "应有 4 个 filter chain: 1 个 split + 3 个 scale")

	// 第一个 chain 是 split
	assert.True(t, strings.HasPrefix(chains[0], "[0:v]split="), "第一个 chain 应以 [0:v]split= 开头")

	// 后续 chain 是 scale
	for i := 1; i < len(chains); i++ {
		assert.Contains(t, chains[i], "scale=", "第 %d 个 chain 应包含 scale=", i+1)
		assert.Contains(t, chains[i], "[v", "第 %d 个 chain 应包含 [v 标签", i+1)
	}
}
