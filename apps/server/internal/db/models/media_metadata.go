package models

import "time"

// MediaMetadata 保存原文件自带元数据的当前解析结果。
type MediaMetadata struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	MediaID        int64     `gorm:"not null;uniqueIndex:idx_media_metadata_space_media_source,priority:2;index:idx_media_metadata_media,priority:2" json:"media_id"`
	SpaceID        string    `gorm:"not null;default:space-default;uniqueIndex:idx_media_metadata_space_media_source,priority:1;index:idx_media_metadata_media,priority:1;index:idx_media_metadata_space_stale,priority:1" json:"space_id"`
	Source         string    `gorm:"not null;uniqueIndex:idx_media_metadata_space_media_source,priority:3" json:"source"`
	Tool           string    `gorm:"not null" json:"tool"`
	ToolVersion    string    `gorm:"not null" json:"tool_version"`
	RawJSON        string    `gorm:"type:text;not null" json:"raw_json"`
	NormalizedJSON string    `gorm:"type:text;not null" json:"normalized_json"`
	ParsedAt       time.Time `gorm:"not null" json:"parsed_at"`
	Stale          bool      `gorm:"not null;default:false;index:idx_media_metadata_space_stale,priority:2" json:"stale"`
}
