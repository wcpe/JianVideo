package models

import "time"

// MediaHashGroup 记录内容哈希精确重复组的可查询快照。
type MediaHashGroup struct {
	ID              int64     `gorm:"primaryKey" json:"id"`
	SpaceID         string    `gorm:"not null;default:space-default" json:"space_id"`
	FileSize        int64     `gorm:"not null" json:"file_size"`
	ContentHash     string    `gorm:"size:64;not null" json:"content_hash"`
	ContentHashAlgo string    `gorm:"not null;default:sha256" json:"content_hash_algo"`
	ItemCount       int64     `gorm:"not null;default:0" json:"item_count"`
	FirstMediaID    int64     `gorm:"not null;default:0" json:"first_media_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}
