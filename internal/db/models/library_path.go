package models

import "time"

// LibraryPath 媒体库目录记录。
type LibraryPath struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	SpaceID   string    `gorm:"not null;default:space-default;index:idx_library_paths_space_id;uniqueIndex:idx_library_paths_space_path,priority:1" json:"space_id"`
	Path      string    `gorm:"not null;uniqueIndex:idx_library_paths_space_path,priority:2" json:"path"`
	Type      string    `gorm:"not null;default:'local'" json:"type"`
	Label     string    `json:"label"`
	Enabled   int       `gorm:"default:1" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}
