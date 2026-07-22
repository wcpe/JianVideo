package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 分发判定：h264（或空）→ TS 路径；h265/av1/vp9 → fMP4 路径。
// 该纯函数是「播放/转码输出分发点」的核心决策，单测穷举保证 h264 永远回到原路径。

func TestSelectOutputPath_H264StaysTS(t *testing.T) {
	assert.Equal(t, OutputPathTS, SelectOutputPath("h264"))
	assert.Equal(t, OutputPathTS, SelectOutputPath(""), "空目标编码默认 H.264/TS")
	assert.Equal(t, OutputPathTS, SelectOutputPath("H264"))
	assert.Equal(t, OutputPathTS, SelectOutputPath("unknown"), "未知编码保守回 TS")
}

func TestSelectOutputPath_AdvancedGoesFMP4(t *testing.T) {
	assert.Equal(t, OutputPathFMP4, SelectOutputPath("h265"))
	assert.Equal(t, OutputPathFMP4, SelectOutputPath("hevc"))
	assert.Equal(t, OutputPathFMP4, SelectOutputPath("av1"))
	assert.Equal(t, OutputPathFMP4, SelectOutputPath("vp9"))
}

// TestTSPathArgsUnchanged 锁定 H.264 TS 路径参数：fMP4 改动不得影响现有多码率 TS 参数。
// 这是红线回归断言——若有人改了 TS 分支参数，此测试应当失败。
func TestTSPathArgsUnchanged(t *testing.T) {
	p := &Pipeline{encoderName: "libx264"}
	mp := NewMultiPipeline(p)
	args := mp.BuildArgs("/tmp/test.mp4", []string{"720p"})

	// 仍输出 mpegts/HLS TS（非 fMP4）
	f, ok := argValue(args, "-f")
	assert.True(t, ok)
	assert.Equal(t, "hls", f)
	// TS 路径不得混入 fMP4 段类型
	_, hasFmp4 := argValue(args, "-hls_segment_type")
	assert.False(t, hasFmp4, "TS 路径不应出现 -hls_segment_type fmp4")
	// 单管道 mpegts 输出（buildArgs）仍为 mpegts
	sp := p.buildArgs("/tmp/test.mp4", 0)
	spF, _ := argValue(sp, "-f")
	assert.Equal(t, "mpegts", spF, "单码率管道仍输出 mpegts 裸流")
}
