package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipelineBuildArgs_NoSeek_NoHwAccel(t *testing.T) {
	p := Pipeline{encoderName: "libx264", deviceType: "", hwAccel: ""}
	args := p.buildArgs("/tmp/test.mp4", 0)

	assert.Contains(t, args, "-hide_banner")
	assert.Contains(t, args, "-loglevel")
	assert.Contains(t, args, "warning")
	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "/tmp/test.mp4")
	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "libx264")
	// 强制 8-bit yuv420p，避免 10-bit 源编出浏览器/mpegts.js 无法播放的 High 10
	assert.Contains(t, args, "-pix_fmt")
	assert.Contains(t, args, "yuv420p")
	assert.Greater(t, indexOf(args, "-pix_fmt"), indexOf(args, "-i"), "-pix_fmt 应作用于输出（位于 -i 之后）")
	assert.Contains(t, args, "-g")
	assert.Contains(t, args, "48")
	assert.Contains(t, args, "-keyint_min")
	assert.Contains(t, args, "-sc_threshold")
	assert.Contains(t, args, "0")
	assert.Contains(t, args, "-c:a")
	assert.Contains(t, args, "copy")
	assert.Contains(t, args, "-f")
	assert.Contains(t, args, "mpegts")
	assert.Contains(t, args, "-")
	assert.NotContains(t, args, "-ss")
	assert.NotContains(t, args, "-hwaccel")
}

func TestPipelineBuildArgs_WithSeek(t *testing.T) {
	p := Pipeline{encoderName: "libx264", deviceType: "", hwAccel: ""}
	args := p.buildArgs("/tmp/test.mp4", 10.5)

	assert.Contains(t, args, "-ss")
	assert.Contains(t, args, "10.50")

	// 验证 "-ss" 在 "-i" 之前
	ssIdx := indexOf(args, "-ss")
	iIdx := indexOf(args, "-i")
	assert.True(t, ssIdx < iIdx, "-ss 应该出现在 -i 之前")
}

func TestPipelineBuildArgs_WithHwAccel(t *testing.T) {
	p := Pipeline{encoderName: "h264_nvenc", deviceType: "cuda", hwAccel: "cuda"}
	args := p.buildArgs("/tmp/test.mp4", 0)

	assert.Contains(t, args, "-hwaccel")
	assert.Contains(t, args, "cuda")

	// 验证 "-hwaccel" 在 "-i" 之前
	hwIdx := indexOf(args, "-hwaccel")
	iIdx := indexOf(args, "-i")
	assert.True(t, hwIdx < iIdx, "-hwaccel 应该出现在 -i 之前")

	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "h264_nvenc")
}

func TestPipelineBuildArgs_WithSeekAndHwAccel(t *testing.T) {
	p := Pipeline{encoderName: "h264_qsv", deviceType: "qsv", hwAccel: "qsv"}
	args := p.buildArgs("/tmp/test.mp4", 5.0)

	// 验证顺序：-ss → -hwaccel → -i
	ssIdx := indexOf(args, "-ss")
	hwIdx := indexOf(args, "-hwaccel")
	iIdx := indexOf(args, "-i")
	assert.True(t, ssIdx < hwIdx, "-ss 应该出现在 -hwaccel 之前")
	assert.True(t, hwIdx < iIdx, "-hwaccel 应该出现在 -i 之前")
}

func TestPipelineBuildArgs_SeekZero(t *testing.T) {
	p := Pipeline{encoderName: "libx264", deviceType: "", hwAccel: ""}
	args := p.buildArgs("/tmp/test.mp4", 0)

	assert.NotContains(t, args, "-ss", "seekPosition=0 时不应生成 -ss 参数")
}

func TestNewPipeline(t *testing.T) {
	p := NewPipeline()
	assert.NotNil(t, p)
	assert.Equal(t, "libx264", p.encoderName)
}

// indexOf 返回切片中指定元素的索引，不存在时返回 -1。
func indexOf(slice []string, target string) int {
	for i, v := range slice {
		if strings.TrimSpace(v) == target {
			return i
		}
	}
	return -1
}
