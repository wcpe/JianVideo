package models

import "time"

// MediaExtension 媒体文件后缀配置。
type MediaExtension struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	LibraryID int64     `gorm:"not null;uniqueIndex:idx_media_extensions_library_extension" json:"library_id"`
	Extension string    `gorm:"not null;uniqueIndex:idx_media_extensions_library_extension" json:"extension"`
	Type      string    `gorm:"not null" json:"type"`
	IsBuiltIn int       `gorm:"default:0" json:"is_builtin"`
	CreatedAt time.Time `json:"created_at"`
}
