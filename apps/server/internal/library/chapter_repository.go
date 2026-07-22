package library

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

type chapterRepository interface {
	List(context.Context, string, int64) ([]models.MediaChapter, error)
}

type gormChapterRepository struct {
	db *gorm.DB
}

func newGormChapterRepository(db *gorm.DB) chapterRepository {
	return &gormChapterRepository{db: db}
}

func (r *gormChapterRepository) List(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaChapter, error) {
	var chapters []models.MediaChapter
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND media_id = ?", normalizeSpaceID(spaceID), mediaID).
		Order("start_ms ASC, source_index ASC").Find(&chapters).Error
	return chapters, err
}

// MediaChapterList 是章节只读 API 的当前解析状态。
type MediaChapterList struct {
	Items    []models.MediaChapter
	Stale    bool
	ParsedAt *time.Time
}

// ListMediaChapters 返回当前 Space 活跃媒体的只读内嵌章节。
func (s *Service) ListMediaChapters(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaChapter, error) {
	result, err := s.GetMediaChapters(ctx, spaceID, mediaID)
	return result.Items, err
}

// GetMediaChapters 返回章节及其 ffprobe 当前解析状态。
func (s *Service) GetMediaChapters(ctx context.Context, spaceID string, mediaID int64) (MediaChapterList, error) {
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return MediaChapterList{}, err
	}
	chapters, err := s.chapterRepo.List(ctx, spaceID, mediaID)
	if err != nil {
		return MediaChapterList{}, err
	}
	result := MediaChapterList{Items: chapters}
	metadata, err := s.metadataRepo.List(ctx, spaceID, mediaID)
	if err != nil {
		return MediaChapterList{}, err
	}
	for index := range metadata {
		if metadata[index].Source == MetadataSourceFFprobe {
			result.Stale = metadata[index].Stale
			result.ParsedAt = &metadata[index].ParsedAt
			break
		}
	}
	return result, nil
}
