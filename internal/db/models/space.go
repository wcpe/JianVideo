package models

import "time"

// DefaultSpaceID 是单用户兼容模式下的默认 Space。
const DefaultSpaceID = "space-default"

// Space 表示最小 Space 归属单元。
type Space struct {
	ID          string    `gorm:"primaryKey;type:text" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	OwnerUserID int64     `gorm:"not null;index" json:"owner_user_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
