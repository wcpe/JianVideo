package library

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// NextEpisodeResult 下一集定位结果（FR2-047）。
// Media 为 nil 表示当前无下一集（标题缺失、无推断或已是最后一集）。
type NextEpisodeResult struct {
	Media   *models.MediaFile      `json:"media"`
	Current *models.MediaInference `json:"current"`
	Next    *models.MediaInference `json:"next"`
}

// FindNextEpisodeInSpace 在同 Space 内按 title + season/episode 定位下一集。
// 规则：优先同季更大 episode；否则下一季的最小 episode。
// 仅匹配未软删媒体；跨 Space 不返回。
func (s *Service) FindNextEpisodeInSpace(spaceID string, mediaID int64) (*NextEpisodeResult, error) {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return nil, err
	}
	current, err := s.GetMediaInferenceInSpace(spaceID, mediaID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &NextEpisodeResult{Media: nil, Current: nil, Next: nil}, nil
	}
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(current.Title)
	if title == "" || current.Episode <= 0 {
		return &NextEpisodeResult{Media: nil, Current: current, Next: nil}, nil
	}

	// 同 title + 同季 + 更大 episode
	nextInf, err := s.findNextInference(spaceID, title, current.Season, current.Episode)
	if err != nil {
		return nil, err
	}
	if nextInf == nil {
		return &NextEpisodeResult{Media: nil, Current: current, Next: nil}, nil
	}
	nextMedia, err := s.GetMediaFileByIDInSpace(spaceID, nextInf.MediaID)
	if err != nil {
		// 推断残留但媒体已删：视为无下一集
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &NextEpisodeResult{Media: nil, Current: current, Next: nil}, nil
		}
		return nil, err
	}
	return &NextEpisodeResult{Media: nextMedia, Current: current, Next: nextInf}, nil
}

// findNextInference 查询同 Space 同 title 的下一集推断记录（跳过已软删媒体）。
func (s *Service) findNextInference(spaceID, title string, season, episode int) (*models.MediaInference, error) {
	if !s.db.Migrator().HasTable(&models.MediaInference{}) {
		return nil, nil
	}
	base := s.db.Model(&models.MediaInference{}).
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

// FindAlbumNeighborInSpace 在合集顺序中定位相对当前媒体的上/下一首。
// direction: +1 下一首，-1 上一首；越界返回 nil media。
func (s *Service) FindAlbumNeighborInSpace(spaceID string, albumID, mediaID int64, direction int) (*models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	items, err := s.ListAlbumItemsInSpace(spaceID, albumID)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, item := range items {
		if item.ID == mediaID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	next := idx + direction
	if next < 0 || next >= len(items) {
		return nil, nil
	}
	return &items[next], nil
}
