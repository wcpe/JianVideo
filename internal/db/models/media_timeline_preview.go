package models

import "time"

// MediaTimelinePreview 保存媒体与 profile 当前使用的时间线预览 generation 指针。
type MediaTimelinePreview struct {
	ID                       int64     `gorm:"primaryKey" json:"id"`
	SpaceID                  string    `gorm:"not null;uniqueIndex:idx_media_timeline_previews_space_media_profile,priority:1;index:idx_media_timeline_previews_space_media,priority:1" json:"space_id"`
	MediaID                  int64     `gorm:"not null;uniqueIndex:idx_media_timeline_previews_space_media_profile,priority:2;index:idx_media_timeline_previews_space_media,priority:2" json:"media_id"`
	ProfileID                string    `gorm:"not null;uniqueIndex:idx_media_timeline_previews_space_media_profile,priority:3" json:"profile_id"`
	SourceFingerprint        string    `gorm:"not null;default:''" json:"source_fingerprint"`
	GenerationID             string    `gorm:"not null;default:'';index:idx_media_timeline_previews_asset_generation,priority:2" json:"generation_id"`
	AssetID                  int64     `gorm:"not null;default:0;index:idx_media_timeline_previews_asset_generation,priority:1" json:"asset_id"`
	PendingSourceFingerprint string    `gorm:"not null;default:''" json:"pending_source_fingerprint,omitempty"`
	PendingGenerationID      string    `gorm:"not null;default:''" json:"pending_generation_id,omitempty"`
	PendingTaskID            int64     `gorm:"not null;default:0;index:idx_media_timeline_previews_pending_task" json:"pending_task_id,omitempty"`
	UpdatedAt                time.Time `gorm:"not null" json:"updated_at"`
}
