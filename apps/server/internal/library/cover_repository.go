package library

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// coverRepository 封装封面选择与候选的持久化（FR2-070 续4）。
type coverRepository interface {
	// HasCoverTable 判断 media_covers 表是否已迁移（旧库兼容）。
	HasCoverTable() bool
	// GetCover 读取当前封面选择；不存在返回 (nil, nil)。
	GetCover(ctx context.Context, spaceID string, mediaID int64) (*models.MediaCover, error)
	// ListCandidates 按时间点稳定排序列出候选。
	ListCandidates(ctx context.Context, spaceID string, mediaID int64) ([]models.CoverCandidate, error)
	// SaveGenerated 原子写入生成候选，可选清理旧指纹，并按人工/自动规则恢复当前封面。
	SaveGenerated(ctx context.Context, spaceID string, mediaID int64, generated []models.CoverCandidate, refresh bool) (*models.MediaCover, []models.CoverCandidate, error)
	// SelectCandidate 校验候选归属后持久化人工选择。
	SelectCandidate(ctx context.Context, spaceID string, mediaID, candidateID int64) (*models.MediaCover, error)
}

type gormCoverRepository struct {
	db *gorm.DB
}

func newGormCoverRepository(db *gorm.DB) coverRepository {
	return &gormCoverRepository{db: db}
}

func (r *gormCoverRepository) HasCoverTable() bool {
	return r.db.Migrator().HasTable(&models.MediaCover{})
}

func (r *gormCoverRepository) GetCover(ctx context.Context, spaceID string, mediaID int64) (*models.MediaCover, error) {
	var cover models.MediaCover
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		First(&cover).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &cover, err
}

func (r *gormCoverRepository) ListCandidates(ctx context.Context, spaceID string, mediaID int64) ([]models.CoverCandidate, error) {
	var candidates []models.CoverCandidate
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Order("timestamp_seconds ASC, id ASC").Find(&candidates).Error
	return candidates, err
}

func (r *gormCoverRepository) SaveGenerated(ctx context.Context, spaceID string, mediaID int64, generated []models.CoverCandidate, refresh bool) (*models.MediaCover, []models.CoverCandidate, error) {
	spaceID = normalizeSpaceID(spaceID)
	var selected models.MediaCover
	var candidates []models.CoverCandidate
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadMediaCoverTx(tx, spaceID, mediaID)
		if err != nil {
			return err
		}
		fingerprints := make([]string, 0, len(generated))
		for index := range generated {
			generated[index].SpaceID = spaceID
			generated[index].MediaID = mediaID
			fingerprints = append(fingerprints, generated[index].Fingerprint)
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "space_id"}, {Name: "media_id"}, {Name: "fingerprint"}},
				DoUpdates: clause.AssignmentColumns([]string{"asset_id", "source", "timestamp_seconds", "score", "updated_at"}),
			}).Create(&generated[index]).Error; err != nil {
				return err
			}
		}
		if refresh {
			query := tx.Where("space_id = ? AND media_id = ?", spaceID, mediaID)
			if len(fingerprints) > 0 {
				query = query.Where("fingerprint NOT IN ?", fingerprints)
			}
			if err := query.Delete(&models.CoverCandidate{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("space_id = ? AND media_id = ?", spaceID, mediaID).
			Order("timestamp_seconds ASC, id ASC").Find(&candidates).Error; err != nil {
			return err
		}
		selected = resolveCoverSelection(current, candidates, spaceID, mediaID)
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "media_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"space_id", "selected_asset_id", "selected_source", "selected_timestamp_seconds", "selected_fingerprint", "manual", "updated_at"}),
		}).Create(&selected).Error
	})
	return &selected, candidates, err
}

func (r *gormCoverRepository) SelectCandidate(ctx context.Context, spaceID string, mediaID, candidateID int64) (*models.MediaCover, error) {
	spaceID = normalizeSpaceID(spaceID)
	var selected models.MediaCover
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate models.CoverCandidate
		if err := tx.Where("id = ? AND space_id = ? AND media_id = ?", candidateID, spaceID, mediaID).First(&candidate).Error; err != nil {
			return err
		}
		selected = models.MediaCover{
			MediaID: mediaID, SpaceID: spaceID, SelectedAssetID: candidate.AssetID,
			SelectedSource: candidate.Source, SelectedTimestampSeconds: candidate.TimestampSeconds,
			SelectedFingerprint: candidate.Fingerprint, Manual: true, UpdatedAt: candidate.UpdatedAt,
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "media_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"space_id", "selected_asset_id", "selected_source", "selected_timestamp_seconds", "selected_fingerprint", "manual", "updated_at"}),
		}).Create(&selected).Error
	})
	return &selected, err
}

func loadMediaCoverTx(tx *gorm.DB, spaceID string, mediaID int64) (*models.MediaCover, error) {
	var cover models.MediaCover
	err := tx.Where("space_id = ? AND media_id = ?", spaceID, mediaID).First(&cover).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &cover, err
}
