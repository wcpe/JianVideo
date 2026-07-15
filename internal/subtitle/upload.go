package subtitle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

// Upload 流式保存并校验用户上传字幕。
func (s *Service) Upload(ctx context.Context, spaceID string, mediaID int64, fileName string, reader io.Reader) (Track, error) {
	media, err := s.media(ctx, spaceID, mediaID)
	if err != nil {
		return Track{}, err
	}
	format, err := uploadFormat(fileName)
	if err != nil {
		return Track{}, err
	}
	tempPath, err := writeUploadTemp(s.subtitleDir(media), reader)
	if err != nil {
		return Track{}, err
	}
	defer removeFile(tempPath)
	if err := validateUploadTemp(tempPath, format); err != nil {
		return Track{}, err
	}
	return s.commitUpload(ctx, media, fileName, format, tempPath)
}

func uploadFormat(fileName string) (string, error) {
	if filepath.Base(fileName) != fileName || strings.Contains(fileName, "..") {
		return "", fmt.Errorf("%w: 文件名不合法", ErrInvalid)
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")
	for _, allowed := range []string{"srt", "ass", "ssa", "vtt"} {
		if format == allowed {
			return format, nil
		}
	}
	return "", fmt.Errorf("%w: 扩展名不支持", ErrInvalid)
}

func writeUploadTemp(dir string, reader io.Reader) (string, error) {
	file, err := requestTempFile(dir, ".tmp-upload-")
	if err != nil {
		return "", err
	}
	path := file.Name()
	copyErr := copyUpload(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		removeFile(path)
		return "", copyErr
	}
	if closeErr != nil {
		removeFile(path)
		return "", closeErr
	}
	return path, nil
}

func copyUpload(file *os.File, reader io.Reader) error {
	written, err := io.Copy(file, io.LimitReader(reader, MaxUploadBytes+1))
	if err != nil {
		return err
	}
	if written > MaxUploadBytes {
		return ErrTooLarge
	}
	return file.Sync()
}

func validateUploadTemp(path, format string) error {
	// #nosec G304 -- path 仅来自 requestTempFile 创建的受控临时文件。
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := transcoder.ValidateSubtitle(format, data); err != nil {
		return fmt.Errorf("%w: %v", ErrUnprocessable, err)
	}
	return nil
}

func (s *Service) commitUpload(ctx context.Context, media models.MediaFile, fileName, format, tempPath string) (Track, error) {
	trackID, err := newTrackID()
	if err != nil {
		return Track{}, err
	}
	finalPath := filepath.Join(s.subtitleDir(media), trackID+"."+format)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return Track{}, fmt.Errorf("原子保存字幕失败: %w", err)
	}
	row := uploadRow(media, trackID, fileName, format)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if cleanupErr := os.Remove(finalPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			return Track{}, errors.Join(err, fmt.Errorf("清理失败字幕文件失败: %w", cleanupErr))
		}
		return Track{}, err
	}
	return uploadedTrack(row), nil
}

func uploadRow(media models.MediaFile, trackID, fileName, format string) models.MediaSubtitleTrack {
	return models.MediaSubtitleTrack{
		ID: trackID, SpaceID: media.SpaceID, MediaID: media.ID, Source: SourceUploaded,
		SourceRef: filepath.Base(fileName), StorageRelativePath: expectedStoredPath(media.SpaceID, media.ID, trackID, format),
		StreamIndex: -1, Format: format, Title: strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName)),
	}
}

func newTrackID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("生成字幕轨道 ID 失败: %w", err)
	}
	return "upl-" + hex.EncodeToString(value), nil
}

func (s *Service) subtitleDir(media models.MediaFile) string {
	return filepath.Join(s.dataDir, "subtitles", media.SpaceID, strconv.FormatInt(media.ID, 10))
}

func expectedStoredPath(spaceID string, mediaID int64, trackID, format string) string {
	return filepath.ToSlash(filepath.Join("subtitles", spaceID, strconv.FormatInt(mediaID, 10), trackID+"."+format))
}

// Delete 隔离文件并删除记录，最终移除失败时恢复记录、审计和原路径。
func (s *Service) Delete(ctx context.Context, spaceID string, mediaID int64, trackID string) error {
	row, path, err := s.uploadedRowPath(ctx, spaceID, mediaID, trackID)
	if err != nil {
		return err
	}
	isolated := path + ".deleting-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Rename(path, isolated); err != nil {
		return fmt.Errorf("隔离字幕文件失败: %w", err)
	}
	eventID, err := s.deleteRowTx(ctx, row)
	if err != nil {
		_ = os.Rename(isolated, path)
		return err
	}
	if err := s.remove(isolated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return s.compensateDelete(ctx, row, path, isolated, eventID, err)
	}
	return nil
}

func (s *Service) deleteRowTx(ctx context.Context, row models.MediaSubtitleTrack) (int64, error) {
	var eventID int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND space_id = ? AND media_id = ? AND source = ?", row.ID, row.SpaceID, row.MediaID, SourceUploaded).
			Delete(&models.MediaSubtitleTrack{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		var err error
		eventID, err = s.recordDeleteAudit(ctx, tx, row)
		return err
	})
	return eventID, err
}

func (s *Service) recordDeleteAudit(ctx context.Context, tx *gorm.DB, row models.MediaSubtitleTrack) (int64, error) {
	if s.audit == nil {
		return 0, nil
	}
	requestID, err := newTrackID()
	if err != nil {
		return 0, err
	}
	input := deleteAuditInput(row, requestID)
	if err := s.audit.RecordTx(ctx, tx, input); err != nil {
		return 0, err
	}
	var event models.AuditEvent
	err = tx.WithContext(ctx).Where("request_id = ?", requestID).First(&event).Error
	return event.ID, err
}

func deleteAuditInput(row models.MediaSubtitleTrack, requestID string) audit.EventInput {
	return audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: row.SpaceID, ActorType: audit.ActorSystem,
		Action: "subtitle.deleted", ResourceType: "subtitle", ResourceID: row.ID, RequestID: requestID,
		Metadata: map[string]any{"media_id": row.MediaID, "format": row.Format},
	}
}

func (s *Service) compensateDelete(ctx context.Context, row models.MediaSubtitleTrack, path, isolated string, eventID int64, removeErr error) error {
	if err := os.Rename(isolated, path); err != nil {
		return errors.Join(fmt.Errorf("删除已隔离字幕文件失败: %w", removeErr), fmt.Errorf("恢复字幕原路径失败: %w", err))
	}
	if err := s.restoreDeleteTx(ctx, row, eventID, removeErr); err != nil {
		_ = os.Rename(path, isolated)
		return errors.Join(fmt.Errorf("删除已隔离字幕文件失败: %w", removeErr), err)
	}
	return fmt.Errorf("删除已隔离字幕文件失败，已恢复: %w", removeErr)
}

func (s *Service) restoreDeleteTx(ctx context.Context, row models.MediaSubtitleTrack, eventID int64, removeErr error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if eventID > 0 {
			result := tx.Where("id = ?", eventID).Delete(&models.AuditEvent{})
			if result.Error != nil || result.RowsAffected != 1 {
				return errors.Join(result.Error, errors.New("删除字幕审计事件失败"))
			}
		}
		return s.recordCompensationAudit(ctx, tx, row, removeErr)
	})
}

func (s *Service) recordCompensationAudit(ctx context.Context, tx *gorm.DB, row models.MediaSubtitleTrack, removeErr error) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordTx(ctx, tx, audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: row.SpaceID, ActorType: audit.ActorSystem,
		Action: "subtitle.delete_compensated", ResourceType: "subtitle", ResourceID: row.ID,
		Metadata: map[string]any{"media_id": row.MediaID, "error": removeErr.Error()},
	})
}

func removeFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
