package transcoder

import "fmt"

// negotiation 实现端到端编码协商的纯逻辑（FR-53 核心）。
// 把「首选优先级 ∩ 客户端能力 ∩ 实测可产出」收敛为单个可穷举单测的纯函数，
// 并把协商出的编码映射为前端可消费的播放描述符。副作用（产 fMP4、记会话）在 api 层。

// NegotiationDescriptor 协商产出的播放描述符（与前端 PlaybackDescriptor 对齐）。
// URL 为相对路径，前端按需绝对化。
type NegotiationDescriptor struct {
	// Codec 协商出的实际目标编码（h264/h265/av1/vp9，已归一化）。
	Codec string `json:"codec"`
	// Path 播放路径：ts（H.264，mpegts.js）/ fmp4（高级编码，原生 MSE）/ mp4（已验证原文件直出）。
	Path string `json:"path"`
	// URL 清单 / 流地址：ts 为 master，fmp4 为 index.m3u8，mp4 为原文件流。
	URL string `json:"url"`
	// MIME 高级编码在 fMP4 容器下的 MSE codec MIME 串；ts 路径为空。
	MIME string `json:"mime,omitempty"`
	// FallbackURL 客户端不支持高级编码时的 H.264/TS 回退源；ts 路径为空。
	FallbackURL string `json:"fallback_url,omitempty"`
	// FramePresentation 仅在后端验证画面 marker 与有界帧索引一一对应后返回。
	FramePresentation *FramePresentationDescriptor `json:"frame_presentation,omitempty"`
}

// FramePresentationDescriptor 描述可由实际呈现像素验证的逐帧身份契约。
type FramePresentationDescriptor struct {
	Marker           FrameMarkerDescriptor `json:"marker"`
	NominalFrameRate float64               `json:"nominal_frame_rate"`
	Timeline         []FrameTimelineEntry  `json:"timeline"`
}

// FrameMarkerDescriptor 描述二进制画面 marker 的像素布局。
type FrameMarkerDescriptor struct {
	Bits      int `json:"bits"`
	CellSize  int `json:"cell_size"`
	Threshold int `json:"threshold,omitempty"`
	X         int `json:"x"`
	Y         int `json:"y"`
}

// FrameTimelineEntry 描述 marker 身份对应的名义媒体时间。
type FrameTimelineEntry struct {
	MediaTime        float64 `json:"media_time"`
	SourceFrameIndex int     `json:"source_frame_index"`
	StableFrameID    string  `json:"stable_frame_id"`
}

// ChosenCodec 编码协商纯函数：返回首选优先级里第一个同时满足
// 「客户端能力支持」且「实测可产出」的编码；都不满足则兜底 DefaultTargetCodec（h264）。
//
// h264 作为恒可用底座：现有 mpegts.js + TS 路径保证可播，故任何协商不出高级编码的情形
// （客户端不支持 / 不可产出 / 优先级为空）都回落 h264。编码标识大小写无关、hevc 视为 h265。
func ChosenCodec(priority []string, clientCaps, producible map[string]bool) string {
	for _, raw := range priority {
		codec := normalizeCodec(raw)
		if codec == DefaultTargetCodec {
			// 优先级显式排到 h264：它恒可用，直接命中兜底底座
			return DefaultTargetCodec
		}
		if clientCaps[codec] && producible[codec] {
			return codec
		}
	}
	return DefaultTargetCodec
}

// BuildNegotiationDescriptor 把协商出的编码映射为播放描述符（纯函数）。
// h264 → TS 路径（master，mpegts.js）；高级编码 → fMP4 路径（index.m3u8，原生 MSE），
// 附 MSE MIME 串与 H.264 回退源（客户端临时不支持时回退，不报错）。
func BuildNegotiationDescriptor(mediaID int64, codec string) NegotiationDescriptor {
	c := normalizeCodec(codec)
	if !IsAdvancedCodec(c) {
		return NegotiationDescriptor{
			Codec: DefaultTargetCodec,
			Path:  string(OutputPathTS),
			URL:   fmt.Sprintf("/api/play/hls/%d/master", mediaID),
		}
	}
	return NegotiationDescriptor{
		Codec:       c,
		Path:        string(OutputPathFMP4),
		URL:         fmt.Sprintf("/api/play/hls/%d/%s", mediaID, fmp4ManifestFilename),
		MIME:        FMP4CodecMIME(c),
		FallbackURL: fmt.Sprintf("/api/play/%d/stream", mediaID),
	}
}
