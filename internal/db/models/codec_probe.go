package models

import "time"

// CodecProbeCache 缓存一次编解码器实测结果（FR-49），以 FFmpeg 可执行文件身份摘要为键。
// 显式指定列名，避免 GORM 把 FFmpegVersion 误转为 f_fmpeg_version。
type CodecProbeCache struct {
	FFmpegVersion string    `gorm:"column:ffmpeg_version;primaryKey" json:"ffmpeg_version"`
	Results       string    `gorm:"column:results" json:"results"` // []EncoderProbeResult 的 JSON
	TestedAt      time.Time `gorm:"column:tested_at" json:"tested_at"`
}
