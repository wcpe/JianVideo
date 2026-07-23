// Package space 提供 Space 成员与角色解析（FR2-010）。
package space

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 哨兵错误。
var (
	ErrSpaceNotFound        = errors.New("space 不存在")
	ErrNotMember            = errors.New("不是 space 成员")
	ErrForbidden            = errors.New("权限不足")
	ErrInvalidRole          = errors.New("角色无效")
	ErrUserNotFound         = errors.New("用户不存在")
	ErrCannotRemoveOwner    = errors.New("不能移除 space owner 成员行")
	ErrCannotTransferToSelf = errors.New("不能转让给自己")
)

// Service Space 成员服务。
type Service struct {
	db *gorm.DB
}

// NewService 创建服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// MemberRole 返回用户在 Space 的角色；非成员返回 ErrNotMember。
func (s *Service) MemberRole(spaceID string, userID int64) (string, error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	// 用 Limit+Find，避免 First 在无行时打 GORM「record not found」日志。
	var members []models.SpaceMember
	if err := s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Limit(1).Find(&members).Error; err != nil {
		return "", err
	}
	if len(members) > 0 {
		return members[0].Role, nil
	}
	// 兼容：尚无成员表行时，回退 spaces.owner_user_id（迁移前/半迁移）。
	var owner int64
	qerr := s.db.Model(&models.Space{}).Select("owner_user_id").Where("id = ?", spaceID).Scan(&owner).Error
	if qerr != nil {
		return "", qerr
	}
	if owner == 0 {
		return "", ErrSpaceNotFound
	}
	if owner == userID {
		return models.SpaceRoleOwner, nil
	}
	return "", ErrNotMember
}

// RequireRole 要求用户在 Space 至少具备 minRole。
func (s *Service) RequireRole(spaceID string, userID int64, minRole string) error {
	role, err := s.MemberRole(spaceID, userID)
	if err != nil {
		return err
	}
	if !models.RoleAtLeast(role, minRole) {
		return ErrForbidden
	}
	return nil
}

// ListAccessible 列出用户可访问的 Space。
func (s *Service) ListAccessible(userID int64) ([]models.Space, error) {
	var spaces []models.Space
	err := s.db.Raw(`
		SELECT s.* FROM spaces s
		INNER JOIN space_members m ON m.space_id = s.id
		WHERE m.user_id = ?
		ORDER BY s.created_at ASC
	`, userID).Scan(&spaces).Error
	if err != nil {
		return nil, err
	}
	if len(spaces) > 0 {
		return spaces, nil
	}
	// 无成员行：回退 owner_user_id 匹配（兼容未回填库）。
	err = s.db.Where("owner_user_id = ?", userID).Order("created_at ASC").Find(&spaces).Error
	return spaces, err
}

// CreateSpace 创建 Space，创建者为 owner，并写入成员行。
func (s *Service) CreateSpace(id, name string, ownerUserID int64) (*models.Space, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return nil, fmt.Errorf("id 与 name 不能为空")
	}
	if !validSpaceID(id) {
		return nil, fmt.Errorf("space id 不合法")
	}
	now := time.Now()
	sp := models.Space{ID: id, Name: name, OwnerUserID: ownerUserID, CreatedAt: now, UpdatedAt: now}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&sp).Error; err != nil {
			return err
		}
		return tx.Create(&models.SpaceMember{
			SpaceID: id, UserID: ownerUserID, Role: models.SpaceRoleOwner,
			CreatedAt: now, UpdatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &sp, nil
}

// AddMember 添加或更新成员角色（仅调用方应已校验 owner）。
// created=true 表示新建成员行；false 表示更新已有角色。
func (s *Service) AddMember(spaceID string, userID int64, role string) (created bool, err error) {
	if !validRole(role) {
		return false, ErrInvalidRole
	}
	if role == models.SpaceRoleOwner {
		return false, fmt.Errorf("请通过转让 owner 流程设置 owner")
	}
	var count int64
	if err := s.db.Model(&models.Space{}).Where("id = ?", spaceID).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return false, ErrSpaceNotFound
	}
	now := time.Now()
	var existing []models.SpaceMember
	if err := s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Limit(1).Find(&existing).Error; err != nil {
		return false, err
	}
	if len(existing) == 0 {
		return true, s.db.Create(&models.SpaceMember{
			SpaceID: spaceID, UserID: userID, Role: role, CreatedAt: now, UpdatedAt: now,
		}).Error
	}
	if existing[0].Role == models.SpaceRoleOwner {
		return false, ErrCannotRemoveOwner
	}
	return false, s.db.Model(&models.SpaceMember{}).
		Where("space_id = ? AND user_id = ?", spaceID, userID).
		Updates(map[string]any{"role": role, "updated_at": now}).Error
}

// TransferOwner 将 space 所有权从 fromUserID 转给 toUserID（须已是成员）。
// 单事务：spaces.owner_user_id=to；旧 owner 行 role→editor；新 owner 行 role→owner。
// 禁止转给自己；to 须 active 成员；from 须为当前 owner。
func (s *Service) TransferOwner(spaceID string, fromUserID, toUserID int64) error {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	if fromUserID <= 0 || toUserID <= 0 {
		return fmt.Errorf("用户 ID 无效")
	}
	if fromUserID == toUserID {
		return ErrCannotTransferToSelf
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		var spaces []models.Space
		if err := tx.Where("id = ?", spaceID).Limit(1).Find(&spaces).Error; err != nil {
			return err
		}
		if len(spaces) == 0 {
			return ErrSpaceNotFound
		}
		if spaces[0].OwnerUserID != fromUserID {
			return ErrForbidden
		}

		// 接收方须已是成员（active 成员行）。
		var toRows []models.SpaceMember
		if err := tx.Where("space_id = ? AND user_id = ?", spaceID, toUserID).Limit(1).Find(&toRows).Error; err != nil {
			return err
		}
		if len(toRows) == 0 {
			return ErrNotMember
		}

		now := time.Now()
		if err := tx.Model(&models.Space{}).Where("id = ?", spaceID).
			Updates(map[string]any{"owner_user_id": toUserID, "updated_at": now}).Error; err != nil {
			return err
		}

		// 旧 owner → editor（无成员行时补写，兼容半迁移）。
		var fromRows []models.SpaceMember
		if err := tx.Where("space_id = ? AND user_id = ?", spaceID, fromUserID).Limit(1).Find(&fromRows).Error; err != nil {
			return err
		}
		if len(fromRows) == 0 {
			if err := tx.Create(&models.SpaceMember{
				SpaceID: spaceID, UserID: fromUserID, Role: models.SpaceRoleEditor,
				CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&models.SpaceMember{}).
				Where("space_id = ? AND user_id = ?", spaceID, fromUserID).
				Updates(map[string]any{"role": models.SpaceRoleEditor, "updated_at": now}).Error; err != nil {
				return err
			}
		}

		// 新 owner → owner
		return tx.Model(&models.SpaceMember{}).
			Where("space_id = ? AND user_id = ?", spaceID, toUserID).
			Updates(map[string]any{"role": models.SpaceRoleOwner, "updated_at": now}).Error
	})
}

// RemoveMember 移除成员（不可移除 owner 行）。
func (s *Service) RemoveMember(spaceID string, userID int64) error {
	var rows []models.SpaceMember
	if err := s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrNotMember
	}
	if rows[0].Role == models.SpaceRoleOwner {
		return ErrCannotRemoveOwner
	}
	return s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Delete(&models.SpaceMember{}).Error
}

// ListMembers 列出 Space 成员。
func (s *Service) ListMembers(spaceID string) ([]models.SpaceMember, error) {
	var members []models.SpaceMember
	err := s.db.Where("space_id = ?", spaceID).Order("role DESC, user_id ASC").Find(&members).Error
	return members, err
}

// EffectiveMaxRating 解析用户在 Space 的有效最高可见分级（FR2-051）。
// 成员 max_rating 非空优先；否则用 Space default_max_rating；皆空表示不限制（返回 ""）。
func (s *Service) EffectiveMaxRating(spaceID string, userID int64) (string, error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	var members []models.SpaceMember
	if err := s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Limit(1).Find(&members).Error; err != nil {
		return "", err
	}
	if len(members) > 0 && members[0].MaxRating != nil {
		if r := strings.TrimSpace(*members[0].MaxRating); r != "" {
			return models.NormalizeContentRating(r), nil
		}
	}
	var spaces []models.Space
	if err := s.db.Where("id = ?", spaceID).Limit(1).Find(&spaces).Error; err != nil {
		return "", err
	}
	if len(spaces) == 0 {
		return "", ErrSpaceNotFound
	}
	return models.NormalizeContentRating(spaces[0].DefaultMaxRating), nil
}

// SetSpaceDefaultMaxRating 设置 Space 默认最高可见分级；rating 空表示不限制。
func (s *Service) SetSpaceDefaultMaxRating(spaceID, rating string) error {
	if !models.ValidContentRating(rating) {
		return fmt.Errorf("非法内容分级")
	}
	norm := models.NormalizeContentRating(rating)
	res := s.db.Model(&models.Space{}).Where("id = ?", spaceID).
		Updates(map[string]any{"default_max_rating": norm, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrSpaceNotFound
	}
	return nil
}

// SetMemberMaxRating 设置成员最高可见分级；rating 空指针表示清除覆盖（继承 Space 默认）。
// rating 非 nil 且空串也表示清除。
func (s *Service) SetMemberMaxRating(spaceID string, userID int64, rating *string) error {
	var norm *string
	if rating != nil {
		r := strings.TrimSpace(*rating)
		if r == "" {
			norm = nil
		} else {
			if !models.ValidContentRating(r) {
				return fmt.Errorf("非法内容分级")
			}
			n := models.NormalizeContentRating(r)
			norm = &n
		}
	}
	var rows []models.SpaceMember
	if err := s.db.Where("space_id = ? AND user_id = ?", spaceID, userID).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrNotMember
	}
	return s.db.Model(&models.SpaceMember{}).
		Where("space_id = ? AND user_id = ?", spaceID, userID).
		Updates(map[string]any{"max_rating": norm, "updated_at": time.Now()}).Error
}

// GetSpace 按 id 取 Space。
func (s *Service) GetSpace(spaceID string) (*models.Space, error) {
	var rows []models.Space
	if err := s.db.Where("id = ?", strings.TrimSpace(spaceID)).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrSpaceNotFound
	}
	return &rows[0], nil
}

// EnsureOwnerMember 确保 Space owner 在成员表中有 owner 行（迁移/修复用）。
func (s *Service) EnsureOwnerMember(spaceID string, ownerUserID int64) error {
	now := time.Now()
	var count int64
	if err := s.db.Model(&models.SpaceMember{}).Where("space_id = ? AND user_id = ?", spaceID, ownerUserID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return s.db.Model(&models.SpaceMember{}).
			Where("space_id = ? AND user_id = ?", spaceID, ownerUserID).
			Updates(map[string]any{"role": models.SpaceRoleOwner, "updated_at": now}).Error
	}
	return s.db.Create(&models.SpaceMember{
		SpaceID: spaceID, UserID: ownerUserID, Role: models.SpaceRoleOwner,
		CreatedAt: now, UpdatedAt: now,
	}).Error
}

func validRole(role string) bool {
	return role == models.SpaceRoleOwner || role == models.SpaceRoleEditor || role == models.SpaceRoleViewer
}

func validSpaceID(spaceID string) bool {
	if spaceID == "" || len(spaceID) > 128 {
		return false
	}
	for _, ch := range spaceID {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}
