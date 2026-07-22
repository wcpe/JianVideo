package library

import (
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// libraryPathRepository 封装媒体库目录 CRUD 持久化（FR2-070 续15）。
type libraryPathRepository interface {
	// RunInTx 在事务中执行 fn。
	RunInTx(fn func(tx *gorm.DB) error) error
	// CreateTx 事务内插入目录记录。
	CreateTx(tx *gorm.DB, lp *models.LibraryPath) error
	// GetByID 按 space+id 取目录。
	GetByID(spaceID string, id int64) (*models.LibraryPath, error)
	// GetByIDTx 事务内按 space+id 取目录。
	GetByIDTx(tx *gorm.DB, spaceID string, id int64) (*models.LibraryPath, error)
	// ListBySpace 按 space 列出目录（id 升序）。
	ListBySpace(spaceID string) ([]models.LibraryPath, error)
	// ListAll 列出全部 Space 目录（供 watcher/定时扫描）。
	ListAll() ([]models.LibraryPath, error)
	// UpdateTx 事务内更新字段。
	UpdateTx(tx *gorm.DB, spaceID string, id int64, updates map[string]any) error
	// DeleteTx 事务内删除目录行；返回 rowsAffected。
	DeleteTx(tx *gorm.DB, spaceID string, id int64) (int64, error)
	// DeleteExtensionsByLibraryIDTx 事务内删除该库自定义后缀。
	DeleteExtensionsByLibraryIDTx(tx *gorm.DB, libraryID int64) error
	// ListCustomExtensions 列出自定义后缀（libraryID>0 时按库过滤；按 extension 升序）。
	ListCustomExtensions(libraryID int64) ([]models.MediaExtension, error)
	// FirstOrCreateExtension 幂等写入自定义后缀。
	FirstOrCreateExtension(item *models.MediaExtension) error
	// DeleteExtension 按 library+extension 删除自定义后缀；返回 rowsAffected。
	DeleteExtension(libraryID int64, extension string) (int64, error)
	// CountBySpace 统计某 Space 下目录数。
	CountBySpace(spaceID string) (int64, error)
	// CountEnabledBySpace 统计某 Space 下启用目录数（enabled=1）。
	CountEnabledBySpace(spaceID string) (int64, error)
	// GetSpaceIDByLibraryID 按 library id 取 space_id；不存在返回 gorm.ErrRecordNotFound。
	GetSpaceIDByLibraryID(libraryID int64) (string, error)
	// GetLibraryKind 取内容分型；不存在返回 gorm.ErrRecordNotFound。
	GetLibraryKind(spaceID string, libraryID int64) (string, error)
	// HasTable 是否存在 library_paths 表。
	HasTable() bool
	// HasMediaExtensionTable 是否存在 media_extensions 表（旧库兼容）。
	HasMediaExtensionTable() bool
}

type gormLibraryPathRepository struct {
	db *gorm.DB
}

func newGormLibraryPathRepository(db *gorm.DB) libraryPathRepository {
	return &gormLibraryPathRepository{db: db}
}

func (r *gormLibraryPathRepository) RunInTx(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r *gormLibraryPathRepository) CreateTx(tx *gorm.DB, lp *models.LibraryPath) error {
	return tx.Create(lp).Error
}

func (r *gormLibraryPathRepository) GetByID(spaceID string, id int64) (*models.LibraryPath, error) {
	var lp models.LibraryPath
	if err := r.db.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).First(&lp).Error; err != nil {
		return nil, err
	}
	return &lp, nil
}

func (r *gormLibraryPathRepository) GetByIDTx(tx *gorm.DB, spaceID string, id int64) (*models.LibraryPath, error) {
	var lp models.LibraryPath
	if err := tx.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).First(&lp).Error; err != nil {
		return nil, err
	}
	return &lp, nil
}

func (r *gormLibraryPathRepository) ListBySpace(spaceID string) ([]models.LibraryPath, error) {
	var paths []models.LibraryPath
	err := r.db.Where("space_id = ?", normalizeSpaceID(spaceID)).Order("id").Find(&paths).Error
	return paths, err
}

func (r *gormLibraryPathRepository) ListAll() ([]models.LibraryPath, error) {
	var paths []models.LibraryPath
	err := r.db.Order("space_id ASC, id ASC").Find(&paths).Error
	return paths, err
}

func (r *gormLibraryPathRepository) UpdateTx(tx *gorm.DB, spaceID string, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return tx.Model(&models.LibraryPath{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).
		Updates(updates).Error
}

func (r *gormLibraryPathRepository) DeleteTx(tx *gorm.DB, spaceID string, id int64) (int64, error) {
	result := tx.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).Delete(&models.LibraryPath{})
	return result.RowsAffected, result.Error
}

func (r *gormLibraryPathRepository) DeleteExtensionsByLibraryIDTx(tx *gorm.DB, libraryID int64) error {
	return tx.Where("library_id = ?", libraryID).Delete(&models.MediaExtension{}).Error
}

func (r *gormLibraryPathRepository) ListCustomExtensions(libraryID int64) ([]models.MediaExtension, error) {
	var custom []models.MediaExtension
	query := r.db.Order("extension ASC")
	if libraryID > 0 {
		query = query.Where("library_id = ?", libraryID)
	}
	if err := query.Find(&custom).Error; err != nil {
		return nil, err
	}
	return custom, nil
}

func (r *gormLibraryPathRepository) FirstOrCreateExtension(item *models.MediaExtension) error {
	return r.db.Where(models.MediaExtension{LibraryID: item.LibraryID, Extension: item.Extension}).
		Attrs(*item).FirstOrCreate(item).Error
}

func (r *gormLibraryPathRepository) DeleteExtension(libraryID int64, extension string) (int64, error) {
	result := r.db.Where("library_id = ? AND extension = ?", libraryID, extension).Delete(&models.MediaExtension{})
	return result.RowsAffected, result.Error
}

func (r *gormLibraryPathRepository) CountBySpace(spaceID string) (int64, error) {
	var count int64
	err := r.db.Model(&models.LibraryPath{}).Where("space_id = ?", normalizeSpaceID(spaceID)).Count(&count).Error
	return count, err
}

func (r *gormLibraryPathRepository) CountEnabledBySpace(spaceID string) (int64, error) {
	var count int64
	err := r.db.Model(&models.LibraryPath{}).
		Where("space_id = ? AND enabled = ?", normalizeSpaceID(spaceID), 1).
		Count(&count).Error
	return count, err
}

func (r *gormLibraryPathRepository) GetSpaceIDByLibraryID(libraryID int64) (string, error) {
	var lp models.LibraryPath
	if err := r.db.Select("space_id").First(&lp, libraryID).Error; err != nil {
		return "", err
	}
	return normalizeSpaceID(lp.SpaceID), nil
}

func (r *gormLibraryPathRepository) GetLibraryKind(spaceID string, libraryID int64) (string, error) {
	var lp models.LibraryPath
	err := r.db.Select("library_kind").Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), libraryID).First(&lp).Error
	if err != nil {
		return "", err
	}
	return lp.LibraryKind, nil
}

func (r *gormLibraryPathRepository) HasTable() bool {
	return r.db.Migrator().HasTable(&models.LibraryPath{})
}

func (r *gormLibraryPathRepository) HasMediaExtensionTable() bool {
	return r.db != nil && r.db.Migrator().HasTable(&models.MediaExtension{})
}
