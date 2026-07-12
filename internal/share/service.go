// Package share 提供分享链接的读写能力（FR-43）。
// 分享以 SQLite shares 表为唯一真源；服务只管 token 生命周期与过期，
// 不做资源存在性 / 范围判断（那依赖 library），由 api 层判定，保持本服务无跨模块耦合。
package share

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 分享相关业务错误。公开访问层把以下错误统一映射为 404，不区分以免泄露分享是否存在。
var (
	// ErrShareNotFound token 不存在或已撤销
	ErrShareNotFound = errors.New("分享不存在")
	// ErrShareExpired token 已过期
	ErrShareExpired = errors.New("分享已过期")
	// ErrShareForbidden 访问密码错误（FR-78）
	ErrShareForbidden = errors.New("分享密码错误")
	// ErrShareExhausted 访问次数已达上限（FR-78）
	ErrShareExhausted = errors.New("分享访问次数已用尽")
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

// Create 创建默认 Space 分享。
func (s *Service) Create(resourceType string, resourceID int64, expiresAt *time.Time, password string, maxUses int) (*models.Share, error) {
	return s.CreateInSpace(models.DefaultSpaceID, resourceType, resourceID, expiresAt, password, maxUses)
}

// CreateInSpace 创建指定 Space 分享：生成加密随机 token 落库。
func (s *Service) CreateInSpace(spaceID, resourceType string, resourceID int64, expiresAt *time.Time, password string, maxUses int) (*models.Share, error) {
	if resourceType != models.ShareResourceMedia && resourceType != models.ShareResourceAlbum {
		return nil, fmt.Errorf("非法分享资源类型: %s", resourceType)
	}
	if maxUses < 0 {
		maxUses = 0
	}
	var passwordHash string
	if password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("哈希分享密码失败: %w", err)
		}
		passwordHash = string(hashed)
	}
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = models.DefaultSpaceID
	}
	sh := &models.Share{
		Token:        token,
		SpaceID:      spaceID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ExpiresAt:    expiresAt,
		PasswordHash: passwordHash,
		MaxUses:      maxUses,
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

// VerifyPassword 校验访问密码（FR-78）：分享无密码（PasswordHash 为空）直接放行；
// 否则用 bcrypt 比对，不匹配返回 ErrShareForbidden。不区分以免泄露。
func (s *Service) VerifyPassword(sh *models.Share, password string) error {
	if sh.PasswordHash == "" {
		return nil
	}
	if bcrypt.CompareHashAndPassword([]byte(sh.PasswordHash), []byte(password)) != nil {
		return ErrShareForbidden
	}
	return nil
}

// ConsumeUse 消费一次访问额度（FR-78）：在事务内「先检查后自增」防并发超限。
// MaxUses 为 0 表示无限、不计数；MaxUses>0 且已达上限返回 ErrShareExhausted。
// SQLite 单写者 + 事务保证检查与自增原子，避免并发下超发。
func (s *Service) ConsumeUse(token string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var sh models.Share
		if err := tx.First(&sh, "token = ?", token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareNotFound
			}
			return err
		}
		if sh.MaxUses <= 0 {
			return nil // 无限次：不计数
		}
		if sh.UsedCount >= sh.MaxUses {
			return ErrShareExhausted
		}
		return tx.Model(&models.Share{}).Where("token = ?", token).
			Update("used_count", gorm.Expr("used_count + 1")).Error
	})
}

// List 列出默认 Space 分享。
func (s *Service) List() ([]models.Share, error) {
	return s.ListInSpace(models.DefaultSpaceID)
}

// ListInSpace 列出指定 Space 分享。
func (s *Service) ListInSpace(spaceID string) ([]models.Share, error) {
	var shares []models.Share
	if err := s.db.Where("space_id = ?", normalizeSpaceID(spaceID)).Order("created_at DESC").Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

// Revoke 撤销默认 Space 分享。
func (s *Service) Revoke(token string) error {
	return s.RevokeInSpace(models.DefaultSpaceID, token)
}

// RevokeInSpace 撤销指定 Space 分享。
func (s *Service) RevokeInSpace(spaceID, token string) error {
	result := s.db.Where("space_id = ? AND token = ?", normalizeSpaceID(spaceID), token).Delete(&models.Share{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrShareNotFound
	}
	return nil
}

func normalizeSpaceID(spaceID string) string {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return models.DefaultSpaceID
	}
	return spaceID
}

// generateToken 生成加密随机 token（不用自增 ID / 时间戳，确保不可枚举）。
func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成分享令牌失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}
