package models

import "time"

// PlaybackSession 播放会话记录。
type PlaybackSession struct {
	ID              int64     `gorm:"primaryKey" json:"id"`
	MediaID         int64     `gorm:"index;not null" json:"media_id"`
	ClientIP        string    `gorm:"index" json:"client_ip"`
	CurrentPosition float64   `gorm:"default:0" json:"current_position"`
	Duration        float64   `gorm:"default:0" json:"duration"`
	FileSize        int64     `gorm:"default:0" json:"file_size"`
	BufferedRanges  string    `gorm:"type:text" json:"buffered_ranges"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
