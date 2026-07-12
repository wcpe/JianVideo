// Package models 定义数据库实体结构体，仅承载数据字段，不含业务逻辑。
package models

import "time"

// Album 用户相册（FR-40）：跨目录手动归类媒体的集合。
type Album struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	SpaceID      string    `gorm:"not null;default:space-default;index:idx_albums_space_created,priority:1" json:"space_id"`
	Name         string    `gorm:"not null" json:"name"`
	Description  string    `json:"description"`
	CoverMediaID int64     `gorm:"default:0" json:"cover_media_id"` // 封面媒体（可选）
	CreatedAt    time.Time `gorm:"index:idx_albums_space_created,priority:2" json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AlbumItem 相册成员（FR-40）：相册与媒体的多对多关联。
type AlbumItem struct {
	ID      int64     `gorm:"primaryKey" json:"id"`
	SpaceID string    `gorm:"not null;default:space-default;uniqueIndex:idx_album_items_space_album_media,priority:1" json:"space_id"`
	AlbumID int64     `gorm:"not null;uniqueIndex:idx_album_items_space_album_media,priority:2" json:"album_id"`
	MediaID int64     `gorm:"not null;uniqueIndex:idx_album_items_space_album_media,priority:3" json:"media_id"`
	AddedAt time.Time `json:"added_at"`
}
