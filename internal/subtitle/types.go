// Package subtitle 提供统一字幕与音轨聚合、上传、删除和按请求转换能力。
package subtitle

import (
	"errors"
	"fmt"
)

// 字幕轨道、来源、能力与不支持原因常量。
const (
	// MaxUploadBytes 是单个上传字幕的硬上限。
	MaxUploadBytes int64 = 10 << 20

	KindAudio    = "audio"
	KindSubtitle = "subtitle"

	SourceEmbedded = "embedded"
	SourceSidecar  = "sidecar"
	SourceUploaded = "uploaded"

	CapabilitySeamless    = "seamless"
	CapabilityReload      = "reload"
	CapabilityUnsupported = "unsupported"

	ReasonSMBSidecarUnsupported        = "SMB_SIDECAR_UNSUPPORTED"
	ReasonSMBAudioReloadUnsupported    = "SMB_AUDIO_RELOAD_UNSUPPORTED"
	ReasonAudioSwitchUnsupported       = "AUDIO_SWITCH_UNSUPPORTED"
	ReasonAudioReloadFFmpegUnavailable = "AUDIO_RELOAD_FFMPEG_UNAVAILABLE"
	ReasonAudioStreamIndexUnavailable  = "AUDIO_STREAM_INDEX_UNAVAILABLE"
	ReasonAudioHardwareUnavailable     = "AUDIO_RELOAD_HARDWARE_UNAVAILABLE"
	ReasonSubtitleSwitchUnsupported    = "SUBTITLE_SWITCH_UNSUPPORTED"
	ReasonImageSubtitleUnsupported     = "IMAGE_SUBTITLE_UNSUPPORTED"
	ReasonSubtitleCodecUnsupported     = "SUBTITLE_CODEC_UNSUPPORTED"
	ReasonSubtitleServiceUnavailable   = "SUBTITLE_SERVICE_UNAVAILABLE"
)

// 字幕服务返回的公开错误。
var (
	ErrInvalid       = errors.New("字幕输入无效")
	ErrUnprocessable = errors.New("字幕内容无法处理")
	ErrTooLarge      = errors.New("字幕文件超过 10 MiB")
	ErrNotFound      = errors.New("字幕轨道不存在")
	ErrUnsupported   = errors.New("字幕轨道不受支持")
)

// Track 是前后端共享的统一播放轨道 DTO。
type Track struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Label             string `json:"label"`
	Source            string `json:"source"`
	Format            string `json:"format,omitempty"`
	Codec             string `json:"codec,omitempty"`
	Language          string `json:"language,omitempty"`
	Title             string `json:"title,omitempty"`
	Channels          int    `json:"channels,omitempty"`
	ChannelLayout     string `json:"channel_layout,omitempty"`
	Default           bool   `json:"is_default"`
	Forced            bool   `json:"is_forced"`
	Available         bool   `json:"available"`
	Capability        string `json:"capability"`
	UnsupportedReason string `json:"unsupported_reason,omitempty"`
	StreamIndex       *int   `json:"stream_index,omitempty"`

	path string
}

// SourceCapability 描述来源级发现与读取能力。
type SourceCapability struct {
	Available         bool   `json:"available"`
	Capability        string `json:"capability"`
	UnsupportedReason string `json:"unsupported_reason,omitempty"`
}

// SelectionState 暴露轨道选择意图和后端确认状态。
type SelectionState struct {
	SelectedTrackID  *string `json:"selected_track_id"`
	EffectiveTrackID *string `json:"effective_track_id"`
}

// ListResponse 是统一轨道列表响应。
type ListResponse struct {
	Tracks    []Track                     `json:"tracks"`
	Selection map[string]SelectionState   `json:"selection"`
	Sources   map[string]SourceCapability `json:"sources"`
	Backend   map[string]SourceCapability `json:"backend"`
}

type unsupportedError struct {
	reason string
}

func (e unsupportedError) Error() string {
	return fmt.Sprintf("%s: %s", ErrUnsupported, e.reason)
}

func (e unsupportedError) Unwrap() error {
	return ErrUnsupported
}

// UnsupportedReason 返回结构化不支持原因。
func UnsupportedReason(err error) string {
	var target unsupportedError
	if errors.As(err, &target) {
		return target.reason
	}
	return ""
}
