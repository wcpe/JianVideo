// Package settings 提供运行期键值设置的读写能力（FR-24）。
// 设置以 SQLite settings 表为唯一真源，供回收站、定时扫描等后续能力消费。
package settings

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 已知设置键集中定义，避免魔法字符串散落。
const (
	// KeyRecycleBinPaths 每盘符回收站路径，值为 JSON 字符串（盘符 → 路径）。
	KeyRecycleBinPaths = "recycle_bin_paths"
	// KeyScanInterval 定时扫描周期（秒），值为字符串形式的整数。
	KeyScanInterval = "scan_interval"
)

// Service 运行期设置业务逻辑。
type Service struct {
	db *gorm.DB
}

// NewService 创建设置服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Get 按 key 读取单项设置；键不存在时返回空串且不报错。
func (s *Service) Get(key string) (string, error) {
	var setting models.Setting
	err := s.db.First(&setting, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

// ScanInterval 读取定时扫描周期（FR-28）：值为秒的整数字符串。
// 空 / 非法 / <=0 一律视为关闭定时扫描，返回 0。
func (s *Service) ScanInterval() time.Duration {
	raw, err := s.Get(KeyScanInterval)
	if err != nil {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// GetAll 读取全部设置，返回 key → value 映射。
func (s *Service) GetAll() (map[string]string, error) {
	var items []models.Setting
	if err := s.db.Find(&items).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		result[item.Key] = item.Value
	}
	return result, nil
}

// Set 写入单项设置（upsert：存在则覆盖 value 与 updated_at）。
func (s *Service) Set(key, value string) error {
	return s.upsert(s.db, key, value)
}

// SetMany 批量写入设置，在单事务内原子完成。
func (s *Service) SetMany(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for key, value := range values {
			if err := s.upsert(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// upsert 以主键冲突更新的方式写入单条设置。
func (s *Service) upsert(tx *gorm.DB, key, value string) error {
	setting := models.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error
}
