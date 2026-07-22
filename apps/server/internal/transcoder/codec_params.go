package transcoder

// codec_params 定义「目标编码 → 输出参数」的纯映射（FR-50）。
// 把原先散落在 buildArgs/buildMultiArgs 中硬编的 H.264 输出参数抽象为按编码取值，
// 编码器名由 SelectEncoderForCodec 选取，此处只负责像素格式与关键编码参数。

// CodecParams 描述某目标编码的输出侧参数。
type CodecParams struct {
	// PixFmt 强制输出像素格式。统一 yuv420p：10-bit 源否则编出 10-bit High/Main 10，
	// 浏览器与 mpegts.js 无法解码播放。
	PixFmt string
	// ExtraArgs 该编码必要的额外 ffmpeg 输出参数（成对的 flag/value），无则为 nil。
	ExtraArgs []string
}

// DefaultTargetCodec 未配置 / 配置非法时的默认目标编码，保证运行时默认行为不变。
const DefaultTargetCodec = "h264"

// codecParamsMap 编码 → 输出参数映射。
// 关键参数取保守、软硬件通用值；编码器特有调优不在本期引入。
var codecParamsMap = map[string]CodecParams{
	// h264：维持现状，不附加额外参数（默认行为字节级不变）
	"h264": {PixFmt: "yuv420p"},
	// h265：附 hvc1 标记，便于后续容器封装识别 HEVC
	"h265": {PixFmt: "yuv420p", ExtraArgs: []string{"-tag:v", "hvc1"}},
	"av1":  {PixFmt: "yuv420p"},
	"vp9":  {PixFmt: "yuv420p"},
}

// CodecOutputParams 返回目标编码的输出参数（纯函数）。未知编码返回 ok=false。
func CodecOutputParams(codec string) (CodecParams, bool) {
	params, ok := codecParamsMap[codec]
	return params, ok
}
