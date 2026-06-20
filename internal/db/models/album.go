package models

import "time"

// Album 用户相册（FR-40）：跨目录手动归类媒体的集合。
type Album struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"not null" json:"name"`
	Description  string    `json:"description"`
	CoverMediaID int64     `gorm:"default:0" json:"cover_media_id"` // 封面媒体（可选）
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AlbumItem 相册成员（FR-40）：相册与媒体的多对多关联。
type AlbumItem struct {
	ID      int64     `gorm:"primaryKey" json:"id"`
	AlbumID int64     `gorm:"not null;uniqueIndex:idx_album_items_album_media" json:"album_id"`
	MediaID int64     `gorm:"not null;uniqueIndex:idx_album_items_album_media" json:"media_id"`
	AddedAt time.Time `json:"added_at"`
}
