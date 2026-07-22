package library

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// inferenceRepository 封装媒体推断读写与回填查询（FR2-070 续6）。
// 事务入口用 *gorm.DB（与 bookmark/watch 一致）；审计仍由 service 经 recordAuditTx 写入。
type inferenceRepository interface {
	// HasTable 判断 media_inferences 表是否已迁移（旧库兼容）。
	HasTable() bool
	// Get 读取指定 Space 媒体的推断记录。
	Get(spaceID string, mediaID int64) (*models.MediaInference, error)
	// GetMedia 读取未软删媒体（含任意 file_state）。
	GetMedia(spaceID string, mediaID int64) (*models.MediaFile, error)
	// RunInTx 在事务中执行 fn。
	RunInTx(fn func(tx *gorm.DB) error) error
	// UpsertTx 无条件 upsert 推断（人工纠正路径）。
	UpsertTx(tx *gorm.DB, inf *models.MediaInference) error
	// CreateAutoTx 自动推断 upsert：仅当现有行非 manual 时覆盖；返回 rowsAffected。
	CreateAutoTx(tx *gorm.DB, inf *models.MediaInference) (int64, error)
	// CountBackfill 统计回填候选媒体数。
	CountBackfill(ctx context.Context, spaceID string, libraryID int64, missingOnly bool) (int, error)
	// ListBackfillBatch 按 id 游标取回填批次。
	ListBackfillBatch(ctx context.Context, spaceID string, libraryID int64, missingOnly bool, cursor int64) ([]inferenceBackfillItem, error)
	// FindNextEpisode 同 Space 同 title 的下一集推断（跳过已软删媒体）。
	// 优先同季更大 episode，否则下一季最小 episode；无则返回 nil,nil。
	FindNextEpisode(spaceID, title string, season, episode int) (*models.MediaInference, error)
}

type gormInferenceRepository struct {
	db *gorm.DB
}

func newGormInferenceRepository(db *gorm.DB) inferenceRepository {
	return &gormInferenceRepository{db: db}
}

func (r *gormInferenceRepository) HasTable() bool {
	return r.db.Migrator().HasTable(&models.MediaInference{})
}

func (r *gormInferenceRepository) Get(spaceID string, mediaID int64) (*models.MediaInference, error) {
	var inf models.MediaInference
	err := r.db.Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).First(&inf).Error
	return &inf, err
}

func (r *gormInferenceRepository) GetMedia(spaceID string, mediaID int64) (*models.MediaFile, error) {
	var mf models.MediaFile
	err := r.db.Where("space_id = ? AND id = ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), mediaID).First(&mf).Error
	return &mf, err
}

func (r *gormInferenceRepository) RunInTx(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r *gormInferenceRepository) UpsertTx(tx *gorm.DB, inf *models.MediaInference) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "media_id"}},
		DoUpdates: clause.AssignmentColumns(inferenceUpsertColumns()),
	}).Create(inf).Error
}

func (r *gormInferenceRepository) CreateAutoTx(tx *gorm.DB, inf *models.MediaInference) (int64, error) {
	result := tx.Clauses(autoInferenceConflict()).Create(inf)
	return result.RowsAffected, result.Error
}

func (r *gormInferenceRepository) backfillQuery(ctx context.Context, spaceID string, libraryID int64, missingOnly bool) *gorm.DB {
	spaceID = normalizeSpaceID(spaceID)
	query := r.db.WithContext(ctx).Table("media_files").
		Joins("JOIN library_paths ON library_paths.id = media_files.library_id AND library_paths.space_id = media_files.space_id").
		Where("media_files.space_id = ? AND media_files.deleted_at IS NULL", spaceID)
	if libraryID > 0 {
		query = query.Where("media_files.library_id = ?", libraryID)
	}
	if missingOnly {
		inferred := r.db.Model(&models.MediaInference{}).Select("media_id").Where("space_id = ?", spaceID)
		query = query.Where("media_files.id NOT IN (?)", inferred)
	}
	return query
}

func (r *gormInferenceRepository) CountBackfill(ctx context.Context, spaceID string, libraryID int64, missingOnly bool) (int, error) {
	var total int64
	err := r.backfillQuery(ctx, spaceID, libraryID, missingOnly).Count(&total).Error
	return int(total), err
}

func (r *gormInferenceRepository) ListBackfillBatch(ctx context.Context, spaceID string, libraryID int64, missingOnly bool, cursor int64) ([]inferenceBackfillItem, error) {
	var items []inferenceBackfillItem
	err := r.backfillQuery(ctx, spaceID, libraryID, missingOnly).
		Select("media_files.*, library_paths.library_kind AS library_kind").
		Where("media_files.id > ?", cursor).
		Order("media_files.id ASC").Limit(inferenceBackfillBatchSize).Scan(&items).Error
	return items, err
}

func (r *gormInferenceRepository) FindNextEpisode(spaceID, title string, season, episode int) (*models.MediaInference, error) {
	if !r.HasTable() {
		return nil, nil
	}
	spaceID = normalizeSpaceID(spaceID)
	base := r.db.Model(&models.MediaInference{}).
		Joins("JOIN media_files ON media_files.id = media_inferences.media_id AND media_files.space_id = media_inferences.space_id").
		Where("media_inferences.space_id = ? AND media_inferences.title = ? AND media_files.deleted_at IS NULL", spaceID, title)

	// 同季下一集
	var sameSeason models.MediaInference
	err := base.Session(&gorm.Session{}).
		Where("media_inferences.season = ? AND media_inferences.episode > ?", season, episode).
		Order("media_inferences.episode ASC").
		First(&sameSeason).Error
	if err == nil {
		return &sameSeason, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// 下一季最小集
	var nextSeason models.MediaInference
	err = base.Session(&gorm.Session{}).
		Where("media_inferences.season > ? AND media_inferences.episode > 0", season).
		Order("media_inferences.season ASC, media_inferences.episode ASC").
		First(&nextSeason).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &nextSeason, nil
}
