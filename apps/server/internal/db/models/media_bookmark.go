package models

import "time"

// MediaBookmark 保存用户书签的服务端真源记录。
type MediaBookmark struct {
	ID         string    `gorm:"primaryKey;size:64" json:"id"`
	SpaceID    string    `gorm:"not null;index:idx_media_bookmarks_space_media_position_created,priority:1" json:"-"`
	MediaID    int64     `gorm:"not null;index:idx_media_bookmarks_space_media_position_created,priority:2" json:"-"`
	PositionMS int64     `gorm:"not null;check:position_ms >= 0;index:idx_media_bookmarks_space_media_position_created,priority:3" json:"position_ms"`
	Title      string    `gorm:"not null;size:120" json:"title"`
	Note       *string   `gorm:"type:text" json:"note"`
	Revision   int64     `gorm:"not null;default:1;check:revision > 0" json:"revision"`
	CreatedAt  time.Time `gorm:"not null;index:idx_media_bookmarks_space_media_position_created,priority:4" json:"created_at"`
	UpdatedAt  time.Time `gorm:"not null" json:"updated_at"`
}
