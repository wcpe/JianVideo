package models

import "time"

// Tag 标签（FR-41）。
type Tag struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	SpaceID   string    `gorm:"not null;default:space-default;index:idx_tags_space_id;uniqueIndex:idx_tags_space_name,priority:1" json:"space_id"`
	Name      string    `gorm:"not null;uniqueIndex:idx_tags_space_name,priority:2" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TagMapping 标签与媒体的多对多关联（FR-41）。
type TagMapping struct {
	ID      int64 `gorm:"primaryKey" json:"id"`
	TagID   int64 `gorm:"not null;uniqueIndex:idx_tag_mappings_tag_media" json:"tag_id"`
	MediaID int64 `gorm:"not null;uniqueIndex:idx_tag_mappings_tag_media" json:"media_id"`
}
