package settings

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// Repository 设置表数据访问（ADR-0058 / FR2-070）。业务层不直接拼 GORM 查询。
type Repository interface {
	// Get 按 key 读取；不存在返回 ("", nil)。
	Get(ctx context.Context, key string) (string, error)
	// FindAll 读取全部设置行。
	FindAll(ctx context.Context) ([]models.Setting, error)
	// GetMany 按 key 列表批量读取，返回 key→value（缺失键不出现）。
	GetMany(ctx context.Context, keys []string) (map[string]string, error)
	// Upsert 写入或覆盖单条设置。
	Upsert(ctx context.Context, key, value string) error
	// Transaction 在单事务中执行 fn；fn 内使用同一 TxRepository。
	Transaction(ctx context.Context, fn func(tx TxRepository) error) error
}

// TxRepository 事务内设置访问。
type TxRepository interface {
	GetMany(ctx context.Context, keys []string) (map[string]string, error)
	Upsert(ctx context.Context, key, value string) error
	// DB 暴露底层事务连接，仅供同事务跨域钩子（如推断回填入队）使用；业务包应优先走具名方法。
	DB() *gorm.DB
}

type gormRepository struct {
	db *gorm.DB
}

// NewRepository 创建 GORM 实现的设置仓库。
func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Get(ctx context.Context, key string) (string, error) {
	var setting models.Setting
	err := r.db.WithContext(ctx).First(&setting, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *gormRepository) FindAll(ctx context.Context) ([]models.Setting, error) {
	var items []models.Setting
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

func (r *gormRepository) GetMany(ctx context.Context, keys []string) (map[string]string, error) {
	return getMany(ctx, r.db, keys)
}

func (r *gormRepository) Upsert(ctx context.Context, key, value string) error {
	return upsertSetting(ctx, r.db, key, value)
}

func (r *gormRepository) Transaction(ctx context.Context, fn func(tx TxRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&gormTxRepository{tx: tx})
	})
}

type gormTxRepository struct {
	tx *gorm.DB
}

func (t *gormTxRepository) GetMany(ctx context.Context, keys []string) (map[string]string, error) {
	return getMany(ctx, t.tx, keys)
}

func (t *gormTxRepository) Upsert(ctx context.Context, key, value string) error {
	return upsertSetting(ctx, t.tx, key, value)
}

func (t *gormTxRepository) DB() *gorm.DB {
	return t.tx
}

func getMany(ctx context.Context, db *gorm.DB, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	var items []models.Setting
	if err := db.WithContext(ctx).Where("key IN ?", keys).Find(&items).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		result[item.Key] = item.Value
	}
	return result, nil
}

func upsertSetting(ctx context.Context, db *gorm.DB, key, value string) error {
	setting := models.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error
}
