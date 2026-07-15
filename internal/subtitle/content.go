package subtitle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// Content 按请求解析轨道并返回安全规范化 WebVTT。
func (s *Service) Content(ctx context.Context, spaceID string, mediaID int64, trackID string) (string, error) {
	response, err := s.List(ctx, spaceID, mediaID)
	if err != nil {
		return "", err
	}
	track := findTrack(response.Tracks, trackID)
	if track == nil {
		return "", ErrNotFound
	}
	if !track.Available {
		return "", unsupportedError{reason: track.UnsupportedReason}
	}
	switch track.Source {
	case SourceSidecar:
		return s.sidecarContent(ctx, spaceID, mediaID, *track)
	case SourceUploaded:
		return s.uploadedContent(ctx, spaceID, mediaID, trackID)
	case SourceEmbedded:
		return s.embeddedContent(ctx, spaceID, mediaID, *track)
	default:
		return "", ErrNotFound
	}
}

func (s *Service) sidecarContent(ctx context.Context, spaceID string, mediaID int64, track Track) (string, error) {
	media, err := s.media(ctx, spaceID, mediaID)
	if err != nil {
		return "", err
	}
	content, err := transcoder.ConvertSubtitleFileInRoot(filepath.Dir(media.FilePath), filepath.Base(track.path))
	if errors.Is(err, transcoder.ErrSubtitleFileUnavailable) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", errors.Join(ErrUnprocessable, err)
	}
	return content, nil
}

func findTrack(tracks []Track, trackID string) *Track {
	for index := range tracks {
		if tracks[index].ID == trackID {
			return &tracks[index]
		}
	}
	return nil
}

func (s *Service) uploadedContent(ctx context.Context, spaceID string, mediaID int64, trackID string) (string, error) {
	row, path, err := s.uploadedRowPath(ctx, spaceID, mediaID, trackID)
	if err != nil {
		return "", err
	}
	// #nosec G304 -- path 已由 uploadedRowPath 经 containedPath 限定在字幕数据目录内。
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取上传字幕失败: %w", err)
	}
	return transcoder.ConvertSubtitle(row.Format, data)
}

func (s *Service) uploadedRowPath(ctx context.Context, spaceID string, mediaID int64, trackID string) (models.MediaSubtitleTrack, string, error) {
	if !safeSegment(spaceID) || !safeSegment(trackID) || mediaID <= 0 {
		return models.MediaSubtitleTrack{}, "", ErrNotFound
	}
	var row models.MediaSubtitleTrack
	err := s.db.WithContext(ctx).Where("id = ? AND space_id = ? AND media_id = ? AND source = ?", trackID, spaceID, mediaID, SourceUploaded).First(&row).Error
	if errorsIsNotFound(err) {
		return row, "", ErrNotFound
	}
	if err != nil {
		return row, "", err
	}
	expected := expectedStoredPath(spaceID, mediaID, trackID, row.Format)
	if row.StorageRelativePath != expected {
		return row, "", ErrInvalid
	}
	path, err := containedPath(s.dataDir, expected)
	return row, path, err
}

// StoredPath 返回上传字幕受控绝对路径，仅供内部维护和测试。
func (s *Service) StoredPath(ctx context.Context, spaceID string, mediaID int64, trackID string) (string, error) {
	_, path, err := s.uploadedRowPath(ctx, spaceID, mediaID, trackID)
	return path, err
}

func (s *Service) embeddedContent(ctx context.Context, spaceID string, mediaID int64, track Track) (string, error) {
	if track.StreamIndex == nil {
		return "", ErrNotFound
	}
	media, err := s.media(ctx, spaceID, mediaID)
	if err != nil {
		return "", err
	}
	format := track.Format
	if format == "" {
		return "", unsupportedError{reason: ReasonSubtitleCodecUnsupported}
	}
	path, cleanup, err := s.extractToTemp(ctx, media, *track.StreamIndex, format)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return transcoder.ConvertSubtitleFile(path)
}

func (s *Service) extractToTemp(ctx context.Context, media models.MediaFile, streamIndex int, format string) (string, func(), error) {
	dir := filepath.Join(s.subtitleDir(media), ".requests")
	file, err := requestTempFile(dir, ".tmp-extract-*."+format)
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		removeFile(path)
		return "", func() {}, err
	}
	cleanup := func() { removeFile(path); _ = os.Remove(dir) }
	if err := s.extractor(ctx, media.FilePath, streamIndex, path); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}
