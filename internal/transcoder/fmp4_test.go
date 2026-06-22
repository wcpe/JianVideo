package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// argValue 返回 args 中 flag 后紧跟的值；找不到返回空串与 false。
func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// argValues 返回 args 中所有 flag 后紧跟的值（同一 flag 可重复）。
func argValues(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func TestIsAdvancedCodec(t *testing.T) {
	// h264（及空、未知）走原 TS 路径；h265/av1/vp9 走 fMP4 路径
	assert.False(t, IsAdvancedCodec("h264"))
	assert.False(t, IsAdvancedCodec(""))
	assert.False(t, IsAdvancedCodec("H264"))
	assert.True(t, IsAdvancedCodec("h265"))
	assert.True(t, IsAdvancedCodec("hevc"))
	assert.True(t, IsAdvancedCodec("av1"))
	assert.True(t, IsAdvancedCodec("vp9"))
}

func TestFMP4CodecMIME_H265(t *testing.T) {
	mime := FMP4CodecMIME("h265")
	assert.True(t, strings.HasPrefix(mime, "video/mp4"), "应为 mp4 容器")
	assert.Contains(t, mime, "hvc1", "H.265 fMP4 应声明 hvc1")
}

func TestFMP4CodecMIME_AV1(t *testing.T) {
	mime := FMP4CodecMIME("av1")
	assert.True(t, strings.HasPrefix(mime, "video/mp4"))
	assert.Contains(t, mime, "av01", "AV1 fMP4 应声明 av01")
}

func TestFMP4CodecMIME_VP9(t *testing.T) {
	mime := FMP4CodecMIME("vp9")
	assert.True(t, strings.HasPrefix(mime, "video/mp4"))
	assert.Contains(t, mime, "vp09", "VP9 fMP4 应声明 vp09")
}

func TestFMP4CodecMIME_H264Empty(t *testing.T) {
	// h264 不走 fMP4，无 fMP4 MIME
	assert.Equal(t, "", FMP4CodecMIME("h264"))
	assert.Equal(t, "", FMP4CodecMIME("unknown"))
}

func TestSelectFMP4Encoder_SoftwareFallback(t *testing.T) {
	// 空实测快照 → 软件兜底编码器
	enc, _, ok := SelectFMP4Encoder(nil, "h265")
	assert.True(t, ok)
	assert.Equal(t, "libx265", enc)

	enc, _, ok = SelectFMP4Encoder(nil, "av1")
	assert.True(t, ok)
	assert.Equal(t, "libsvtav1", enc)

	enc, _, ok = SelectFMP4Encoder(nil, "vp9")
	assert.True(t, ok)
	assert.Equal(t, "libvpx-vp9", enc)
}

func TestSelectFMP4Encoder_HardwarePreferred(t *testing.T) {
	// 实测 av1_amf 可用时优先选硬件编码器（复用 FR-49 优先级）
	results := []EncoderProbeResult{
		{Encoder: "libsvtav1", Family: "software", Codec: "av1", Compiled: true, TestedOK: true},
		{Encoder: "av1_amf", Family: "amf", Codec: "av1", Compiled: true, TestedOK: true},
	}
	enc, dev, ok := SelectFMP4Encoder(results, "av1")
	assert.True(t, ok)
	assert.Equal(t, "av1_amf", enc)
	assert.Equal(t, "d3d11va", dev)
}

func TestSelectFMP4Encoder_UnsupportedCodec(t *testing.T) {
	// h264 不归 fMP4 路径管，返回 false
	_, _, ok := SelectFMP4Encoder(nil, "h264")
	assert.False(t, ok)
}

func TestBuildFMP4Args_H265(t *testing.T) {
	args := BuildFMP4Args("/tmp/in.mkv", "libx265", "h265", "")

	// 输入
	in, ok := argValue(args, "-i")
	assert.True(t, ok)
	assert.Equal(t, "/tmp/in.mkv", in)

	// 视频编码器
	cv, ok := argValue(args, "-c:v")
	assert.True(t, ok)
	assert.Equal(t, "libx265", cv)

	// 强制 8-bit yuv420p（与现有 TS 路径同策略）
	pix, ok := argValue(args, "-pix_fmt")
	assert.True(t, ok)
	assert.Equal(t, "yuv420p", pix)

	// HEVC 须打 hvc1 tag（Safari/MSE 兼容）
	tag, ok := argValue(args, "-tag:v")
	assert.True(t, ok)
	assert.Equal(t, "hvc1", tag)

	// fMP4/CMAF 封装：hls muxer + fmp4 分片
	f, ok := argValue(args, "-f")
	assert.True(t, ok)
	assert.Equal(t, "hls", f)
	segType, ok := argValue(args, "-hls_segment_type")
	assert.True(t, ok)
	assert.Equal(t, "fmp4", segType)

	// VOD 播放列表 + init 文件名
	plType, ok := argValue(args, "-hls_playlist_type")
	assert.True(t, ok)
	assert.Equal(t, "vod", plType)
	initName, ok := argValue(args, "-hls_fmp4_init_filename")
	assert.True(t, ok)
	assert.Equal(t, "init.mp4", initName)

	// media segment 模板为 .m4s
	segName, ok := argValue(args, "-hls_segment_filename")
	assert.True(t, ok)
	assert.True(t, strings.HasSuffix(segName, ".m4s"), "media segment 应为 .m4s，实得 %s", segName)

	// 固定 GOP（与现有 TS 路径一致，便于 Seek）
	assert.Contains(t, args, "-g")

	// 音频统一 AAC（fMP4 不能 copy 任意编码）
	ca, ok := argValue(args, "-c:a")
	assert.True(t, ok)
	assert.Equal(t, "aac", ca)

	// 输出清单为 index.m3u8（最后一个参数）
	assert.Equal(t, "index.m3u8", args[len(args)-1])
}

func TestBuildFMP4Args_AV1_NoHvc1Tag(t *testing.T) {
	args := BuildFMP4Args("/tmp/in.mkv", "libsvtav1", "av1", "")
	cv, _ := argValue(args, "-c:v")
	assert.Equal(t, "libsvtav1", cv)
	// 仅 HEVC 打 hvc1 tag，AV1 不应有
	_, hasTag := argValue(args, "-tag:v")
	assert.False(t, hasTag, "AV1 不应有 -tag:v hvc1")
}

func TestBuildFMP4Args_VP9(t *testing.T) {
	args := BuildFMP4Args("/tmp/in.webm", "libvpx-vp9", "vp9", "")
	cv, _ := argValue(args, "-c:v")
	assert.Equal(t, "libvpx-vp9", cv)
	segType, _ := argValue(args, "-hls_segment_type")
	assert.Equal(t, "fmp4", segType)
}

func TestBuildFMP4Args_NoTSArtifacts(t *testing.T) {
	// fMP4 路径不应出现 mpegts 相关参数（与 TS 路径隔离）
	args := BuildFMP4Args("/tmp/in.mkv", "libx265", "h265", "")
	joined := strings.Join(args, " ")
	assert.NotContains(t, joined, "mpegts")
	assert.NotContains(t, joined, ".ts")
}
