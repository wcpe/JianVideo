package models

import "time"

// Setting 运行期键值设置（FR-24）：如每盘符回收站路径、扫描周期等。
type Setting struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}
