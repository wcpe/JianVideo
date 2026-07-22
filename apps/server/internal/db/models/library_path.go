package models

import "time"

// LibraryKindMovie 等常量定义内置媒体库内容分型。
const (
	LibraryKindMovie     = "movie"
	LibraryKindSeries    = "series"
	LibraryKindHomeVideo = "home_video"
	LibraryKindMixed     = "mixed"
)

// LibraryPath 媒体库目录记录。
type LibraryPath struct {
	ID                 int64     `gorm:"primaryKey" json:"id"`
	SpaceID            string    `gorm:"not null;default:space-default;index:idx_library_paths_space_id;index:idx_library_paths_space_kind_id,priority:1;uniqueIndex:idx_library_paths_space_path,priority:1" json:"space_id"`
	Path               string    `gorm:"not null;uniqueIndex:idx_library_paths_space_path,priority:2" json:"path"`
	Type               string    `gorm:"not null;default:'local'" json:"type"`
	LibraryKind        string    `gorm:"not null;default:'mixed';index:idx_library_paths_space_kind_id,priority:2" json:"library_kind"`
	LibraryProfileJSON string    `gorm:"column:library_profile_json;not null;default:'{}'" json:"library_profile_json,omitempty"`
	Label              string    `json:"label"`
	Enabled            int       `gorm:"default:1" json:"enabled"`
	CreatedAt          time.Time `json:"created_at"`
}
