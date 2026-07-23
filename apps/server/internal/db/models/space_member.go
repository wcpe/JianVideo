package models

import "time"

// Space 成员角色（ADR-0056 / FR2-010）。
const (
	SpaceRoleOwner  = "owner"
	SpaceRoleEditor = "editor"
	SpaceRoleViewer = "viewer"
)

// SpaceMember 表示用户在某 Space 的角色。
type SpaceMember struct {
	SpaceID string `gorm:"primaryKey;type:text" json:"space_id"`
	UserID  int64  `gorm:"primaryKey" json:"user_id"`
	Role    string `gorm:"not null" json:"role"`
	// MaxRating 成员在该 Space 的最高可见分级（FR2-051）；空=继承 spaces.default_max_rating。
	MaxRating *string   `json:"max_rating,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (SpaceMember) TableName() string { return "space_members" }

// RoleRank 返回角色权重，便于比较「是否达到最低角色」。
func RoleRank(role string) int {
	switch role {
	case SpaceRoleOwner:
		return 3
	case SpaceRoleEditor:
		return 2
	case SpaceRoleViewer:
		return 1
	default:
		return 0
	}
}

// RoleAtLeast 判断 actual 是否不低于 min。
func RoleAtLeast(actual, min string) bool {
	return RoleRank(actual) >= RoleRank(min)
}
