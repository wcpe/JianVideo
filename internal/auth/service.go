package auth

import (
	"database/sql"
	"fmt"

	"github.com/wcpe/JianVideo/internal/db/models"

	"golang.org/x/crypto/bcrypt"
)

// Service 认证服务
type Service struct {
	db        *sql.DB
	jwtSecret string
}

// NewService 创建认证服务
func NewService(d *sql.DB, jwtSecret string) *Service {
	return &Service{db: d, jwtSecret: jwtSecret}
}

// CreateDefaultUser 创建默认用户（admin/admin）
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
