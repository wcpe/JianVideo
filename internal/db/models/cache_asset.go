package models

import "time"

// CacheAsset 记录可重建缓存资产，清理只允许作用于这些白名单路径。
type CacheAsset struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	SpaceID      string     `gorm:"not null;default:space-default;index:idx_cache_assets_space_kind,priority:1" json:"space_id"`
	LibraryID    int64      `gorm:"index:idx_cache_assets_library_kind,priority:1" json:"library_id,omitempty"`
	MediaID      int64      `gorm:"index:idx_cache_assets_media_kind,priority:1" json:"media_id,omitempty"`
	Kind         string     `gorm:"not null;index:idx_cache_assets_space_kind,priority:2;index:idx_cache_assets_library_kind,priority:2;index:idx_cache_assets_media_kind,priority:2" json:"kind"`
	AssetLevel   string     `gorm:"not null" json:"asset_level"`
	ProfileID    string     `json:"profile_id,omitempty"`
	Variant      string     `json:"variant,omitempty"`
	CacheKey     string     `json:"cache_key,omitempty"`
	RelativePath string     `gorm:"not null;uniqueIndex:idx_cache_assets_relative_path_unique;index" json:"relative_path"`
	SizeBytes    int64      `gorm:"not null;default:0" json:"size_bytes"`
	FileCount    int64      `gorm:"not null;default:0" json:"file_count"`
	Rebuildable  bool       `gorm:"not null;default:true" json:"rebuildable"`
	CreatedAt    time.Time  `gorm:"not null" json:"created_at"`
	AccessedAt   *time.Time `json:"accessed_at,omitempty"`
	MissingAt    *time.Time `json:"missing_at,omitempty"`
	UpdatedAt    time.Time  `gorm:"not null" json:"updated_at"`
}
