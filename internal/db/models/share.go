package models

import "time"

// 分享资源类型（FR-43）：集中定义避免魔法字符串。
const (
	// ShareResourceMedia 单个媒体分享
	ShareResourceMedia = "media"
	// ShareResourceAlbum 相册分享（成员均可只读访问）
	ShareResourceAlbum = "album"
)

// Share 分享链接（FR-43）：token 化只读公开访问指定媒体 / 相册，带过期与范围。
// 作为鉴权的受控例外——公开访客凭 token 免登只读访问被分享内容。
type Share struct {
	// Token 加密随机、不可枚举的分享令牌（主键）
	Token string `gorm:"primaryKey" json:"token"`
	// ResourceType 分享资源类型：media / album
	ResourceType string `gorm:"not null" json:"resource_type"`
	// ResourceID 被分享的媒体 ID 或相册 ID
	ResourceID int64 `gorm:"not null;index" json:"resource_id"`
	// ExpiresAt 过期时间；空表示永不过期
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}
