package models

import "time"

// LibraryPath 媒体库目录记录。
type LibraryPath struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Path      string    `gorm:"uniqueIndex;not null" json:"path"`
	Type      string    `gorm:"not null;default:'local'" json:"type"`
	Label     string    `json:"label"`
	Enabled   int       `gorm:"default:1" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}
