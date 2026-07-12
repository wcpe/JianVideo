package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"

	"golang.org/x/crypto/bcrypt"
)

// ErrCurrentPasswordWrong 修改密码时当前密码校验失败（FR-108），供 handler 区分 401。
var ErrCurrentPasswordWrong = errors.New("当前密码错误")

// Service 认证服务
type Service struct {
	db        *sql.DB
	jwtSecret string
}

// NewService 创建认证服务
func NewService(d *sql.DB, jwtSecret string) *Service {
	return &Service{db: d, jwtSecret: jwtSecret}
}

// NeedsSetup 报告是否需要首次初始化：系统中尚无任何用户时返回 true（FR-109）。
func (s *Service) NeedsSetup() (bool, error) {
	exists, err := models.UserExists(s.db)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// Setup 完成首次初始化：仅当系统无用户时创建首个账户（FR-109）。
// 已存在用户则拒绝（幂等防重复初始化劫持）；用户名 / 密码为空亦拒绝。
func (s *Service) Setup(username, password string) (*models.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, fmt.Errorf("用户名和密码不能为空")
	}
	exists, err := models.UserExists(s.db)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("系统已初始化")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return models.CreateUser(s.db, username, string(hash))
}

// CreateDefaultUser 创建默认用户（admin/admin）。
// 注意：FR-109 起不再于生产启动时调用（首次初始化改由 Setup 引导）；保留仅供测试播种。
func (s *Service) CreateDefaultUser() error {
	exists, err := models.UserExists(s.db)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}

	_, err = models.CreateUser(s.db, "admin", string(hash))
	return err
}

// ChangePassword 校验当前密码后将用户密码改为新密码（FR-108）。
// 当前密码不符返回 ErrCurrentPasswordWrong；新密码为空或用户不存在返回错误。
func (s *Service) ChangePassword(username, currentPassword, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("新密码不能为空")
	}
	user, err := models.FindUserByUsername(s.db, username)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("用户不存在")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return ErrCurrentPasswordWrong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return models.UpdateUserPassword(s.db, username, string(hash))
}

// SpaceAccess 返回目标 Space 是否存在，以及当前用户是否为其 owner。
func (s *Service) SpaceAccess(username, spaceID string) (bool, bool, error) {
	var ownerUserID int64
	err := s.db.QueryRow("SELECT owner_user_id FROM spaces WHERE id = ?", spaceID).Scan(&ownerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	user, err := models.FindUserByUsername(s.db, username)
	if err != nil {
		return true, false, err
	}
	return true, user != nil && int64(user.ID) == ownerUserID, nil
}

// Login 验证用户凭据
func (s *Service) Login(username, password string) (*models.User, error) {
	user, err := models.FindUserByUsername(s.db, username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("用户名或密码错误")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("用户名或密码错误")
	}

	return user, nil
}
