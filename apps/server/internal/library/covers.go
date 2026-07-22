package library

import (
	"context"
	"slices"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// GetMediaCoverInSpace 返回媒体当前封面选择语义。
func (s *Service) GetMediaCoverInSpace(ctx context.Context, spaceID string, mediaID int64) (*models.MediaCover, error) {
	if !s.coverRepo.HasCoverTable() {
		return nil, nil
	}
	return s.coverRepo.GetCover(ctx, spaceID, mediaID)
}

// ListCoverCandidatesInSpace 返回媒体封面候选，按时间点稳定排序。
func (s *Service) ListCoverCandidatesInSpace(ctx context.Context, spaceID string, mediaID int64) ([]models.CoverCandidate, error) {
	return s.coverRepo.ListCandidates(ctx, spaceID, mediaID)
}

// SaveGeneratedCoverCandidates 原子更新候选，并按人工指纹或自动评分恢复当前封面。
func (s *Service) SaveGeneratedCoverCandidates(ctx context.Context, spaceID string, mediaID int64, generated []models.CoverCandidate, refresh bool) (*models.MediaCover, []models.CoverCandidate, error) {
	return s.coverRepo.SaveGenerated(ctx, normalizeSpaceID(spaceID), mediaID, generated, refresh)
}

// SelectCoverCandidateInSpace 校验 Space、媒体与候选归属后持久化人工选择语义。
func (s *Service) SelectCoverCandidateInSpace(ctx context.Context, spaceID string, mediaID, candidateID int64) (*models.MediaCover, error) {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return nil, err
	}
	return s.coverRepo.SelectCandidate(ctx, spaceID, mediaID, candidateID)
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
