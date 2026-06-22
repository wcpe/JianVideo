// Package share 提供分享链接的读写能力（FR-43）。
// 分享以 SQLite shares 表为唯一真源；服务只管 token 生命周期与过期，
// 不做资源存在性 / 范围判断（那依赖 library），由 api 层判定，保持本服务无跨模块耦合。
package share

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 分享相关业务错误。公开访问层把以下错误统一映射为 404，不区分以免泄露分享是否存在。
var (
	// ErrShareNotFound token 不存在或已撤销
	ErrShareNotFound = errors.New("分享不存在")
	// ErrShareExpired token 已过期
	ErrShareExpired = errors.New("分享已过期")
)

// tokenBytes 分享 token 的随机字节数（hex 编码后长度翻倍），足够不可枚举。
const tokenBytes = 32

// Service 分享链接业务逻辑。
type Service struct {
	db *gorm.DB
}

// NewService 创建分享服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Create 创建分享：生成加密随机 token 落库。expiresAt 为空表示永不过期。
func (s *Service) Create(resourceType string, resourceID int64, expiresAt *time.Time) (*models.Share, error) {
	if resourceType != models.ShareResourceMedia && resourceType != models.ShareResourceAlbum {
		return nil, fmt.Errorf("非法分享资源类型: %s", resourceType)
	}
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	sh := &models.Share{
		Token:        token,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
	}
	if err := s.db.Create(sh).Error; err != nil {
		return nil, err
	}
	return sh, nil
}

// Get 取分享：不存在返回 ErrShareNotFound，已过期返回 ErrShareExpired。
func (s *Service) Get(token string) (*models.Share, error) {
	var sh models.Share
	err := s.db.First(&sh, "token = ?", token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShareNotFound
	}
	if err != nil {
		return nil, err
	}
	if sh.ExpiresAt != nil && time.Now().After(*sh.ExpiresAt) {
		return nil, ErrShareExpired
	}
	return &sh, nil
}

// List 列出全部分享（含已过期，供管理端展示）。
func (s *Service) List() ([]models.Share, error) {
	var shares []models.Share
	if err := s.db.Order("created_at DESC").Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

// Revoke 撤销分享（删除 token）。
func (s *Service) Revoke(token string) error {
	return s.db.Where("token = ?", token).Delete(&models.Share{}).Error
}

// generateToken 生成加密随机 token（不用自增 ID / 时间戳，确保不可枚举）。
func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成分享令牌失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}
