package models

import "time"

// TranscodePreset 转码预设（FR-77）：可复用的目标编码/分辨率模板。
// width/height 为 0 表示「源宽/源高」（沿用源分辨率）。
// MVP 仅保留编码与分辨率维度，不含 bitrate/多码率档位（YAGNI）。
type TranscodePreset struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	Codec     string    `gorm:"not null" json:"codec"`   // h264 / h265 / av1 / vp9
	Width     int       `gorm:"default:0" json:"width"`  // 0 = 源宽
	Height    int       `gorm:"default:0" json:"height"` // 0 = 源高
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
