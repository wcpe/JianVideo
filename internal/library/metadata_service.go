package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const metadataBackfillBatchSize = 100

type embeddedMetadataParser func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error)

type metadataFileFingerprint struct {
	FileSize           int64
	ModifiedAtUnixNano int64
}

// ParseAndStoreMetadata 解析单个媒体并覆盖同来源的当前记录。
func (s *Service) ParseAndStoreMetadata(ctx context.Context, spaceID string, mediaID int64) (*models.MediaMetadata, error) {
	return s.parseAndStoreMetadataForFingerprint(ctx, spaceID, mediaID, nil)
}

func (s *Service) parseAndStoreMetadataForFingerprint(ctx context.Context, spaceID string, mediaID int64, expected *metadataFileFingerprint) (*models.MediaMetadata, error) {
	media, err := s.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return nil, err
	}
	if !metadataFingerprintMatches(*media, expected) {
		return nil, nil
	}
	parsed, err := s.metadataParser(ctx, *media)
	if err != nil {
		return nil, err
	}
	if err := validateParsedEmbeddedMetadata(parsed); err != nil {
		return nil, err
	}
	current, err := s.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return nil, err
	}
	if !metadataFingerprintMatches(*current, expected) {
		return nil, nil
	}
	row := metadataRow(*current, parsed)
	if err := s.metadataRepo.Upsert(ctx, row, metadataMediaUpdates(parsed.Normalized)); err != nil {
		return nil, err
	}
	return row, nil
}

func metadataFingerprintMatches(media models.MediaFile, expected *metadataFileFingerprint) bool {
	if expected == nil {
		return true
	}
	return media.FileSize == expected.FileSize && media.ModifiedAt.UnixNano() == expected.ModifiedAtUnixNano
}

// ListMediaMetadata 返回单个媒体的全部当前来源记录。
func (s *Service) ListMediaMetadata(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaMetadata, error) {
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return nil, err
	}
	return s.metadataRepo.List(ctx, spaceID, mediaID)
}

// MarkMediaMetadataStale 标记媒体已有解析结果过期。
func (s *Service) MarkMediaMetadataStale(ctx context.Context, spaceID string, mediaID int64) error {
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return err
	}
	return s.metadataRepo.MarkStale(ctx, spaceID, mediaID)
}

// BackfillMediaMetadata 从检查点后分批解析媒体。
func (s *Service) BackfillMediaMetadata(ctx context.Context, spaceID string, libraryID, lastID int64, progress func(int, int, int64) error) error {
	total, err := s.metadataRepo.CountMedia(ctx, spaceID, libraryID)
	if err != nil {
		return err
	}
	completed, err := s.metadataRepo.CountMediaThroughID(ctx, spaceID, libraryID, lastID)
	if err != nil {
		return err
	}
	done := int(completed)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.metadataRepo.NextMediaBatch(ctx, spaceID, libraryID, lastID, metadataBackfillBatchSize)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for i := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.parseBackfillMedia(ctx, rows[i]); err != nil {
				return err
			}
			lastID, done = rows[i].ID, done+1
			if progress != nil {
				if err := progress(done, int(total), lastID); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Service) parseBackfillMedia(ctx context.Context, media models.MediaFile) error {
	_, err := s.ParseAndStoreMetadata(ctx, media.SpaceID, media.ID)
	if err == nil {
		return nil
	}
	return fmt.Errorf("解析媒体元数据失败: mediaID=%d: %w", media.ID, err)
}

func metadataRow(media models.MediaFile, parsed ParsedEmbeddedMetadata) *models.MediaMetadata {
	return &models.MediaMetadata{
		MediaID: media.ID, SpaceID: media.SpaceID, Source: parsed.Source,
		Tool: parsed.Tool, ToolVersion: parsed.ToolVersion,
		RawJSON: parsed.RawJSON, NormalizedJSON: parsed.NormalizedJSON,
		ParsedAt: time.Now().UTC(), Stale: false,
	}
}

func validateParsedEmbeddedMetadata(parsed ParsedEmbeddedMetadata) error {
	if strings.TrimSpace(parsed.Source) == "" || strings.TrimSpace(parsed.Tool) == "" {
		return errors.New("元数据来源与解析工具不能为空")
	}
	if !json.Valid([]byte(parsed.RawJSON)) || !json.Valid([]byte(parsed.NormalizedJSON)) {
		return errors.New("元数据 JSON 无效")
	}
	return nil
}

func metadataMediaUpdates(normalized NormalizedEmbeddedMetadata) map[string]any {
	if normalized.MediaType != MediaTypeVideo {
		return nil
	}
	updates := map[string]any{
		"duration": normalized.Container.Duration,
		"bitrate":  normalized.Container.Bitrate,
	}
	if len(normalized.VideoStreams) > 0 {
		updates["video_codec"] = normalized.VideoStreams[0].CodecName
		updates["width"] = normalized.VideoStreams[0].Width
		updates["height"] = normalized.VideoStreams[0].Height
	}
	if len(normalized.AudioStreams) > 0 {
		updates["audio_codec"] = normalized.AudioStreams[0].CodecName
	}
	if data, err := json.Marshal(normalized.SubtitleStreams); err == nil {
		updates["subtitle_tracks"] = string(data)
	}
	return updates
}

func (s *Service) mediaByScanChange(change ScanChange) (*models.MediaFile, error) {
	path := change.Path
	if change.Op == ScanChangeRemoved && change.OldPath != "" {
		path = change.OldPath
	}
	return s.mediaRepo.GetMediaFileByLibraryAndPathAnyState(change.SpaceID, change.LibraryID, path)
}

// MetadataScanChangeHook 创建文件变化后的 stale 标记与解析任务入队 hook。
func (s *Service) MetadataScanChangeHook(tasks MetadataTaskEnqueuer, wake func()) func(ScanChange) {
	return func(change ScanChange) {
		change = NormalizeScanChange(change)
		if change.Op != ScanChangeAdded && change.Op != ScanChangeRemoved && !change.FingerprintChanged {
			return
		}
		media, err := s.mediaByScanChange(change)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Printf("[WARN] 扫描变化反查媒体失败: path=%s, err=%v", change.Path, err)
			}
			return
		}
		if err := s.metadataRepo.MarkStale(context.Background(), media.SpaceID, media.ID); err != nil {
			log.Printf("[WARN] 标记媒体元数据过期失败: mediaID=%d, err=%v", media.ID, err)
			return
		}
		if change.Op == ScanChangeRemoved || tasks == nil {
			return
		}
		if _, err := EnqueueMetadataParse(context.Background(), tasks, *media); err != nil {
			log.Printf("[WARN] 媒体元数据刷新入队失败: mediaID=%d, err=%v", media.ID, err)
			return
		}
		if wake != nil {
			wake()
		}
	}
}
