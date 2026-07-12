package library

import (
	"context"
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// GetMediaCoverInSpace 返回媒体当前封面选择语义。
func (s *Service) GetMediaCoverInSpace(ctx context.Context, spaceID string, mediaID int64) (*models.MediaCover, error) {
	if !s.db.Migrator().HasTable(&models.MediaCover{}) {
		return nil, nil
	}
	var cover models.MediaCover
	err := s.db.WithContext(ctx).Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).First(&cover).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &cover, err
}

// ListCoverCandidatesInSpace 返回媒体封面候选，按时间点稳定排序。
func (s *Service) ListCoverCandidatesInSpace(ctx context.Context, spaceID string, mediaID int64) ([]models.CoverCandidate, error) {
	var candidates []models.CoverCandidate
	err := s.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Order("timestamp_seconds ASC, id ASC").Find(&candidates).Error
	return candidates, err
}

// SaveGeneratedCoverCandidates 原子更新候选，并按人工指纹或自动评分恢复当前封面。
func (s *Service) SaveGeneratedCoverCandidates(ctx context.Context, spaceID string, mediaID int64, generated []models.CoverCandidate, refresh bool) (*models.MediaCover, []models.CoverCandidate, error) {
	spaceID = normalizeSpaceID(spaceID)
	var selected models.MediaCover
	var candidates []models.CoverCandidate
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadMediaCover(tx, spaceID, mediaID)
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

// SelectCoverCandidateInSpace 校验 Space、媒体与候选归属后持久化人工选择语义。
func (s *Service) SelectCoverCandidateInSpace(ctx context.Context, spaceID string, mediaID, candidateID int64) (*models.MediaCover, error) {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return nil, err
	}
	var selected models.MediaCover
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

func loadMediaCover(tx *gorm.DB, spaceID string, mediaID int64) (*models.MediaCover, error) {
	var cover models.MediaCover
	err := tx.Where("space_id = ? AND media_id = ?", spaceID, mediaID).First(&cover).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &cover, err
}

func resolveCoverSelection(current *models.MediaCover, candidates []models.CoverCandidate, spaceID string, mediaID int64) models.MediaCover {
	if current != nil && current.Manual {
		restored := *current
		restored.SelectedAssetID = 0
		for _, candidate := range candidates {
			if candidate.Fingerprint == current.SelectedFingerprint {
				restored.SelectedAssetID = candidate.AssetID
				break
			}
		}
		if len(candidates) > 0 {
			restored.UpdatedAt = candidates[0].UpdatedAt
		}
		return restored
	}
	selected := models.MediaCover{MediaID: mediaID, SpaceID: spaceID}
	if len(candidates) == 0 {
		return selected
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Score > best.Score {
			best = candidate
		}
	}
	selected.SelectedAssetID = best.AssetID
	selected.SelectedSource = best.Source
	selected.SelectedTimestampSeconds = best.TimestampSeconds
	selected.SelectedFingerprint = best.Fingerprint
	selected.UpdatedAt = best.UpdatedAt
	return selected
}

// CoverCandidateFingerprints 返回候选指纹去重副本，供缓存清理保留集使用。
func CoverCandidateFingerprints(candidates []models.CoverCandidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		fingerprint := strings.TrimSpace(candidate.Fingerprint)
		if fingerprint != "" && !slices.Contains(result, fingerprint) {
			result = append(result, fingerprint)
		}
	}
	return result
}
