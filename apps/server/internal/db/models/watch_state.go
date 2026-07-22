package models

import "time"

// WatchState 是 Space 内媒体观看进度、完成状态和历史排序的唯一真源。
type WatchState struct {
	SpaceID         string     `gorm:"primaryKey;not null" json:"space_id"`
	MediaID         int64      `gorm:"primaryKey;not null" json:"media_id"`
	PositionSeconds float64    `gorm:"not null;default:0" json:"position_seconds"`
	Completed       bool       `gorm:"not null;default:false" json:"completed"`
	LastWatchedAt   time.Time  `gorm:"not null" json:"last_watched_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Revision        int64      `gorm:"not null;default:0" json:"revision"`
	LastSessionID   string     `gorm:"not null;default:''" json:"last_session_id"`
	LastEventSeq    int64      `gorm:"not null;default:0" json:"last_event_seq"`
	CreatedAt       time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null" json:"updated_at"`
}

// TableName 固定观看状态真源表名。
func (WatchState) TableName() string {
	return "watch_states"
}
