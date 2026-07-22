package models

import "time"

// MediaCover 保存当前封面的可信选择语义；缓存资产可清理，人工选择字段必须保留。
type MediaCover struct {
	MediaID                  int64     `gorm:"primaryKey" json:"media_id"`
	SpaceID                  string    `gorm:"not null;default:space-default;uniqueIndex:idx_media_covers_space_media,priority:1" json:"space_id"`
	SelectedAssetID          int64     `gorm:"default:0" json:"selected_asset_id"`
	SelectedSource           string    `gorm:"not null;default:''" json:"selected_source"`
	SelectedTimestampSeconds float64   `gorm:"not null;default:0" json:"selected_timestamp_seconds"`
	SelectedFingerprint      string    `gorm:"not null;default:''" json:"selected_fingerprint"`
	Manual                   bool      `gorm:"not null;default:false" json:"manual"`
	UpdatedAt                time.Time `gorm:"not null" json:"updated_at"`
}

// CoverCandidate 保存规则化抽帧候选及其当前缓存资产关联。
type CoverCandidate struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	MediaID          int64     `gorm:"not null;uniqueIndex:idx_cover_candidates_space_media_fingerprint,priority:2;index:idx_cover_candidates_space_media_created,priority:2" json:"media_id"`
	SpaceID          string    `gorm:"not null;default:space-default;uniqueIndex:idx_cover_candidates_space_media_fingerprint,priority:1;index:idx_cover_candidates_space_media_created,priority:1" json:"space_id"`
	AssetID          int64     `gorm:"not null;default:0;index:idx_cover_candidates_asset_id" json:"asset_id"`
	Source           string    `gorm:"not null" json:"source"`
	TimestampSeconds float64   `gorm:"not null;default:0" json:"timestamp_seconds"`
	Fingerprint      string    `gorm:"not null;uniqueIndex:idx_cover_candidates_space_media_fingerprint,priority:3" json:"fingerprint"`
	Score            float64   `gorm:"not null;default:0" json:"score"`
	CreatedAt        time.Time `gorm:"not null;index:idx_cover_candidates_space_media_created,priority:3" json:"created_at"`
	UpdatedAt        time.Time `gorm:"not null" json:"updated_at"`
}
