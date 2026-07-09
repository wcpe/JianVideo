package models

import "time"

// MediaTypeRule 记录媒体类型与扫描后缀规则。
type MediaTypeRule struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	SpaceID          string    `gorm:"not null;index" json:"space_id"`
	LibraryID        *int64    `gorm:"index" json:"library_id,omitempty"`
	Type             string    `gorm:"not null" json:"type"`
	Extension        string    `gorm:"not null" json:"extension"`
	Label            string    `gorm:"not null;default:''" json:"label"`
	Description      string    `gorm:"not null;default:''" json:"description"`
	Enabled          bool      `gorm:"not null" json:"enabled"`
	Builtin          bool      `gorm:"not null" json:"builtin"`
	CapabilitiesJSON string    `gorm:"not null;default:'[]'" json:"capabilities_json"`
	CreatedAt        time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt        time.Time `gorm:"not null" json:"updated_at"`
}
