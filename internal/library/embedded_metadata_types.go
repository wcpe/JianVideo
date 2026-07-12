package library

import "time"

const (
	// MetadataSourceFFprobe 表示 ffprobe 读取的视频容器与流元数据。
	MetadataSourceFFprobe = "ffprobe"
	// MetadataSourceImage 表示图片 EXIF、IPTC 与 XMP 元数据。
	MetadataSourceImage = "image"
)

// ParsedEmbeddedMetadata 是解析器返回的持久化输入。
type ParsedEmbeddedMetadata struct {
	Source         string
	Tool           string
	ToolVersion    string
	RawJSON        string
	NormalizedJSON string
	Normalized     NormalizedEmbeddedMetadata
}

// NormalizedEmbeddedMetadata 是前后端共享的规范化元数据结构。
type NormalizedEmbeddedMetadata struct {
	MediaType       string                   `json:"media_type"`
	FileSize        int64                    `json:"file_size,omitempty"`
	FileMTime       time.Time                `json:"file_mtime,omitempty"`
	FileHash        string                   `json:"file_hash,omitempty"`
	FileHashAlgo    string                   `json:"file_hash_algo,omitempty"`
	Container       ContainerMetadata        `json:"container,omitempty"`
	VideoStreams    []VideoStreamMetadata    `json:"video_streams,omitempty"`
	AudioStreams    []AudioStreamMetadata    `json:"audio_streams,omitempty"`
	SubtitleStreams []SubtitleStreamMetadata `json:"subtitle_streams,omitempty"`
	Image           *ImageEmbeddedMetadata   `json:"image,omitempty"`
	Tags            map[string]string        `json:"tags,omitempty"`
}

// ContainerMetadata 描述视频容器级技术信息。
type ContainerMetadata struct {
	FormatName     string            `json:"format_name,omitempty"`
	FormatLongName string            `json:"format_long_name,omitempty"`
	Duration       float64           `json:"duration_seconds,omitempty"`
	Bitrate        int64             `json:"bitrate,omitempty"`
	Size           int64             `json:"size,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// ColorMetadata 描述视频流色彩字段。
type ColorMetadata struct {
	Range     string `json:"range,omitempty"`
	Space     string `json:"space,omitempty"`
	Transfer  string `json:"transfer,omitempty"`
	Primaries string `json:"primaries,omitempty"`
}

// VideoStreamMetadata 描述单个视频流。
type VideoStreamMetadata struct {
	Index            int           `json:"index"`
	CodecName        string        `json:"codec_name,omitempty"`
	CodecLongName    string        `json:"codec_long_name,omitempty"`
	Profile          string        `json:"profile,omitempty"`
	Width            int           `json:"width,omitempty"`
	Height           int           `json:"height,omitempty"`
	PixelFormat      string        `json:"pixel_format,omitempty"`
	FrameRate        string        `json:"frame_rate,omitempty"`
	AverageFrameRate string        `json:"average_frame_rate,omitempty"`
	FrameRateFPS     float64       `json:"frame_rate_fps,omitempty"`
	Bitrate          int64         `json:"bitrate,omitempty"`
	Language         string        `json:"language,omitempty"`
	Title            string        `json:"title,omitempty"`
	Default          bool          `json:"default,omitempty"`
	Forced           bool          `json:"forced,omitempty"`
	Color            ColorMetadata `json:"color,omitempty"`
}

// AudioStreamMetadata 描述单个音频流。
type AudioStreamMetadata struct {
	Index         int    `json:"index"`
	CodecName     string `json:"codec_name,omitempty"`
	CodecLongName string `json:"codec_long_name,omitempty"`
	Profile       string `json:"profile,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	Bitrate       int64  `json:"bitrate,omitempty"`
	Language      string `json:"language,omitempty"`
	Title         string `json:"title,omitempty"`
	Default       bool   `json:"default,omitempty"`
	Forced        bool   `json:"forced,omitempty"`
}

// SubtitleStreamMetadata 描述单个内嵌字幕流。
type SubtitleStreamMetadata struct {
	Index         int    `json:"index"`
	CodecName     string `json:"codec_name,omitempty"`
	CodecLongName string `json:"codec_long_name,omitempty"`
	Language      string `json:"language,omitempty"`
	Title         string `json:"title,omitempty"`
	Default       bool   `json:"default,omitempty"`
	Forced        bool   `json:"forced,omitempty"`
}

// ImageEmbeddedMetadata 描述图片可得的 EXIF、IPTC 与 XMP 信息。
type ImageEmbeddedMetadata struct {
	EXIF map[string]any    `json:"exif,omitempty"`
	IPTC map[string]string `json:"iptc,omitempty"`
	XMP  map[string]string `json:"xmp,omitempty"`
}

// ImageSupplementalMetadata 是标准库提取的 IPTC 与 XMP 子集。
type ImageSupplementalMetadata struct {
	IPTC map[string]string `json:"iptc,omitempty"`
	XMP  map[string]string `json:"xmp,omitempty"`
}
