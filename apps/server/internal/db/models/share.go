package models

import "time"

// 分享资源类型（FR-43）：集中定义避免魔法字符串。
const (
	// ShareResourceMedia 单个媒体分享
	ShareResourceMedia = "media"
	// ShareResourceAlbum 相册分享（成员均可只读访问）
	ShareResourceAlbum = "album"
)

// Share 分享链接（FR-43，密码/限次扩展见 FR-78）：token 化只读公开访问指定媒体 / 相册，
// 带过期、范围、可选访问密码与可选访问限次。
// 作为鉴权的受控例外——公开访客凭 token 免登只读访问被分享内容。
type Share struct {
	// Token 加密随机、不可枚举的分享令牌（主键）
	Token string `gorm:"primaryKey" json:"token"`
	// SpaceID 分享资源归属的 Space；历史记录迁入默认 Space。
	SpaceID string `gorm:"not null;default:space-default;index:idx_shares_space_created,priority:1" json:"space_id"`
	// ResourceType 分享资源类型：media / album
	ResourceType string `gorm:"not null" json:"resource_type"`
	// ResourceID 被分享的媒体 ID 或相册 ID
	ResourceID int64 `gorm:"not null;index" json:"resource_id"`
	// ExpiresAt 过期时间；空表示永不过期
	ExpiresAt *time.Time `json:"expires_at"`
	// PasswordHash 访问密码的 bcrypt 哈希（FR-78）；空串表示无密码。
	// 绝不存明文、绝不回显前端（json:"-"）。
	PasswordHash string `gorm:"default:''" json:"-"`
	// MaxUses 最大访问次数（FR-78）；0 表示无限。
	MaxUses int `gorm:"default:0" json:"max_uses"`
	// UsedCount 已访问次数（FR-78），实际访问资源时原子自增。
	UsedCount int `gorm:"default:0" json:"used_count"`
	// AllowDownload 是否允许公开端点下载原文件（FR2-055）；默认 true；false 时 download 统一 404。
	AllowDownload bool      `gorm:"not null;default:1" json:"allow_download"`
	CreatedAt     time.Time `gorm:"index:idx_shares_space_created,priority:2" json:"created_at"`
}
