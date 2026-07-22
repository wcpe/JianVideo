package models

import "time"

// MediaInference 保存单个媒体的本地影视信息推断或人工纠正结果。
type MediaInference struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	MediaID      int64     `gorm:"not null;uniqueIndex:idx_media_inferences_media_id;index:idx_media_inferences_space_media,priority:2" json:"media_id"`
	SpaceID      string    `gorm:"not null;default:space-default;index:idx_media_inferences_space_media,priority:1" json:"space_id"`
	Kind         string    `gorm:"not null;default:'mixed'" json:"kind"`
	Title        string    `json:"title"`
	Year         int       `gorm:"default:0" json:"year"`
	Season       int       `gorm:"default:0" json:"season"`
	Episode      int       `gorm:"default:0" json:"episode"`
	EpisodeTitle string    `json:"episode_title"`
	Confidence   float64   `gorm:"default:0" json:"confidence"`
	Source       string    `gorm:"not null;default:'offline_rule'" json:"source"`
	RuleVersion  string    `gorm:"not null;default:'fr2-031-v1'" json:"rule_version"`
	Manual       bool      `gorm:"default:false" json:"manual"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
