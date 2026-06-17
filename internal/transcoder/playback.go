package transcoder

import (
	"fmt"
	"path/filepath"
)

// PlayInfo 播放信息响应结构。
type PlayInfo struct {
	URL             string `json:"url"`
	Format          string `json:"format"` // "direct" 或 "hls"
	TranscodeRequired bool   `json:"transcode_required"`
	HWAccel         string `json:"hw_accel"` // 使用的硬件加速类型，直出时为 ""
}

// DecidePlayback 根据媒体文件的编码信息决定播放策略。
// 直出条件：容器=mp4 AND 视频编码=h264 AND 音频编码=aac。
// 直出返回文件路径，转码返回 HLS 占位 URL。
func DecidePlayback(filePath, container, videoCodec, audioCodec string) PlayInfo {
	if IsBrowserCompatible(container, videoCodec, audioCodec) {
		return PlayInfo{
			URL:             filePath,
			Format:          "direct",
			TranscodeRequired: false,
			HWAccel:         "",
		}
	}

	// 不兼容格式 → 转码模式，返回 HLS 占位 URL
	base := filepath.Base(filePath)
	return PlayInfo{
		URL:             fmt.Sprintf("/api/play/hls/%s/index.m3u8", base),
		Format:          "hls",
		TranscodeRequired: true,
		HWAccel:         "software",
	}
}
