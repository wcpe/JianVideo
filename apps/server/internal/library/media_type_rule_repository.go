package library

import (
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// mediaTypeRuleRepository 封装媒体类型规则持久化（FR2-070 续22）。
// 事务入口用 *gorm.DB；审计仍由 service 经 recordAuditTx 写入。
type mediaTypeRuleRepository interface {
	// HasTable 判断 media_type_rules 表是否已迁移（旧库兼容）。
	HasTable() bool
	// ListBySpace 取 Space 级（library_id IS NULL）及可选库级规则；库级后于 Space 级。
	ListBySpace(spaceID string, libraryID int64) ([]models.MediaTypeRule, error)
	// RunInTx 在事务中执行 fn。
	RunInTx(fn func(tx *gorm.DB) error) error
	// GetByIDTx 事务内按 space+id 取规则。
	GetByIDTx(tx *gorm.DB, spaceID string, id int64) (*models.MediaTypeRule, error)
	// FindByKeyTx 事务内按 space+library+type+extension 取规则。
	FindByKeyTx(tx *gorm.DB, spaceID string, libraryID *int64, mediaType, ext string) (*models.MediaTypeRule, error)
	// CreateTx 事务内插入规则（Select * 以写入零值 enabled 等）。
	CreateTx(tx *gorm.DB, rule *models.MediaTypeRule) error
	// UpdateFieldsTx 事务内按主键更新字段。
	UpdateFieldsTx(tx *gorm.DB, id int64, updates map[string]any) error
	// ReloadTx 事务内按主键重载规则。
	ReloadTx(tx *gorm.DB, id int64) (*models.MediaTypeRule, error)
	// DeleteTx 事务内删除规则行。
	DeleteTx(tx *gorm.DB, rule *models.MediaTypeRule) error
}

type gormMediaTypeRuleRepository struct {
	db *gorm.DB
}

func newGormMediaTypeRuleRepository(db *gorm.DB) mediaTypeRuleRepository {
	return &gormMediaTypeRuleRepository{db: db}
}

func (r *gormMediaTypeRuleRepository) HasTable() bool {
	return r.db != nil && r.db.Migrator().HasTable(&models.MediaTypeRule{})
}

func (r *gormMediaTypeRuleRepository) ListBySpace(spaceID string, libraryID int64) ([]models.MediaTypeRule, error) {
	var rows []models.MediaTypeRule
	spaceID = normalizeSpaceID(spaceID)
	query := r.db.Where("space_id = ? AND library_id IS NULL", spaceID)
	if libraryID > 0 {
		query = r.db.Where("space_id = ? AND (library_id IS NULL OR library_id = ?)", spaceID, libraryID)
	}
	err := query.Order("CASE WHEN library_id IS NULL THEN 0 ELSE 1 END").Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *gormMediaTypeRuleRepository) RunInTx(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r *gormMediaTypeRuleRepository) GetByIDTx(tx *gorm.DB, spaceID string, id int64) (*models.MediaTypeRule, error) {
	var rule models.MediaTypeRule
	err := tx.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).First(&rule).Error
	return &rule, err
}

func (r *gormMediaTypeRuleRepository) FindByKeyTx(tx *gorm.DB, spaceID string, libraryID *int64, mediaType, ext string) (*models.MediaTypeRule, error) {
	var rule models.MediaTypeRule
	query := tx.Where("space_id = ? AND type = ? AND extension = ?", normalizeSpaceID(spaceID), mediaType, ext)
	if libraryID == nil || *libraryID <= 0 {
		query = query.Where("library_id IS NULL")
	} else {
		query = query.Where("library_id = ?", *libraryID)
	}
	return &rule, query.First(&rule).Error
}

func (r *gormMediaTypeRuleRepository) CreateTx(tx *gorm.DB, rule *models.MediaTypeRule) error {
	return tx.Select("*").Create(rule).Error
}

func (r *gormMediaTypeRuleRepository) UpdateFieldsTx(tx *gorm.DB, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return tx.Model(&models.MediaTypeRule{}).Where("id = ?", id).Updates(updates).Error
}

func (r *gormMediaTypeRuleRepository) ReloadTx(tx *gorm.DB, id int64) (*models.MediaTypeRule, error) {
	var rule models.MediaTypeRule
	err := tx.First(&rule, id).Error
	return &rule, err
}

func (r *gormMediaTypeRuleRepository) DeleteTx(tx *gorm.DB, rule *models.MediaTypeRule) error {
	return tx.Delete(rule).Error
}
