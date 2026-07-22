package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

// ErrBookmarkInvalidPosition 及同组错误表示书签输入校验失败。
var (
	ErrBookmarkInvalidPosition = errors.New("书签时间位置无效")
	ErrBookmarkTitleRequired   = errors.New("书签标题不能为空")
	ErrBookmarkTitleTooLong    = errors.New("书签标题过长")
	ErrBookmarkNoteTooLong     = errors.New("书签备注过长")
)

// BookmarkInput 表示创建书签的可编辑字段。
type BookmarkInput struct {
	PositionMS int64
	Title      string
	Note       *string
}

// BookmarkUpdate 表示书签全量更新和并发前置条件。
type BookmarkUpdate struct {
	PositionMS int64
	Title      string
	Note       *string
	Revision   int64
}

// BookmarkConflictError 返回服务端当前记录或已删除状态。
type BookmarkConflictError struct {
	Current *models.MediaBookmark
	Deleted bool
}

func (e *BookmarkConflictError) Error() string { return "书签已被其他客户端修改或删除" }

// ListMediaBookmarks 返回当前 Space 活跃媒体的书签。
func (s *Service) ListMediaBookmarks(ctx context.Context, spaceID string, mediaID int64) ([]models.MediaBookmark, error) {
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return nil, err
	}
	return s.bookmarkRepo.List(ctx, spaceID, mediaID)
}

// CreateMediaBookmark 创建 revision=1 的书签。
func (s *Service) CreateMediaBookmark(ctx context.Context, spaceID string, mediaID int64, input BookmarkInput) (*models.MediaBookmark, error) {
	media, err := s.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeBookmarkInput(input, media.Duration)
	if err != nil {
		return nil, err
	}
	id, err := newBookmarkID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	bookmark := &models.MediaBookmark{ID: id, SpaceID: media.SpaceID, MediaID: media.ID, PositionMS: normalized.PositionMS, Title: normalized.Title, Note: normalized.Note, Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = s.bookmarkRepo.RunInTx(ctx, func(tx *gorm.DB) error {
		if err := s.bookmarkRepo.CreateTx(ctx, tx, bookmark); err != nil {
			return err
		}
		return s.recordBookmarkAudit(ctx, tx, "bookmark.created", nil, bookmark)
	})
	return bookmark, err
}

// UpdateMediaBookmark 使用 revision CAS 全量更新可编辑字段。
func (s *Service) UpdateMediaBookmark(ctx context.Context, spaceID string, mediaID int64, id string, update BookmarkUpdate) (*models.MediaBookmark, error) {
	media, err := s.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeBookmarkInput(BookmarkInput{PositionMS: update.PositionMS, Title: update.Title, Note: update.Note}, media.Duration)
	if err != nil {
		return nil, err
	}
	var updated *models.MediaBookmark
	operation := func() error {
		return s.bookmarkRepo.RunInTx(ctx, func(tx *gorm.DB) error {
			current, err := s.bookmarkRepo.GetTx(ctx, tx, media.SpaceID, media.ID, id)
			if err != nil {
				return bookmarkConflictFromLookup(err, nil)
			}
			if update.Revision <= 0 || current.Revision != update.Revision {
				return &BookmarkConflictError{Current: current}
			}
			updated = bookmarkUpdatedCopy(current, normalized)
			matched, err := s.bookmarkRepo.UpdateCASTx(ctx, tx, updated, update.Revision)
			if err != nil || !matched {
				return s.bookmarkCASResult(ctx, tx, media.SpaceID, media.ID, id, err)
			}
			return s.recordBookmarkAudit(ctx, tx, "bookmark.updated", current, updated)
		})
	}
	err = s.runBookmarkCAS(ctx, media.SpaceID, media.ID, id, update.Revision, operation)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteMediaBookmark 使用 revision CAS 物理删除书签业务行。
func (s *Service) DeleteMediaBookmark(ctx context.Context, spaceID string, mediaID int64, id string, revision int64) error {
	media, err := s.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return err
	}
	operation := func() error {
		return s.bookmarkRepo.RunInTx(ctx, func(tx *gorm.DB) error {
			current, err := s.bookmarkRepo.GetTx(ctx, tx, media.SpaceID, media.ID, id)
			if err != nil {
				return bookmarkConflictFromLookup(err, nil)
			}
			if revision <= 0 || current.Revision != revision {
				return &BookmarkConflictError{Current: current}
			}
			matched, err := s.bookmarkRepo.DeleteCASTx(ctx, tx, media.SpaceID, media.ID, id, revision)
			if err != nil || !matched {
				return s.bookmarkCASResult(ctx, tx, media.SpaceID, media.ID, id, err)
			}
			return s.recordBookmarkAudit(ctx, tx, "bookmark.deleted", current, nil)
		})
	}
	return s.runBookmarkCAS(ctx, media.SpaceID, media.ID, id, revision, operation)
}

func normalizeBookmarkInput(input BookmarkInput, durationSeconds float64) (BookmarkInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return BookmarkInput{}, ErrBookmarkTitleRequired
	}
	if utf8.RuneCountInString(input.Title) > 120 {
		return BookmarkInput{}, ErrBookmarkTitleTooLong
	}
	if input.Note != nil {
		note := strings.TrimSpace(*input.Note)
		if utf8.RuneCountInString(note) > 2000 {
			return BookmarkInput{}, ErrBookmarkNoteTooLong
		}
		if note == "" {
			input.Note = nil
		} else {
			input.Note = &note
		}
	}
	if input.PositionMS < 0 || durationSeconds > 0 && input.PositionMS > int64(math.Round(durationSeconds*1000)) {
		return BookmarkInput{}, ErrBookmarkInvalidPosition
	}
	return input, nil
}

func bookmarkUpdatedCopy(current *models.MediaBookmark, input BookmarkInput) *models.MediaBookmark {
	updated := *current
	updated.PositionMS = input.PositionMS
	updated.Title = input.Title
	updated.Note = input.Note
	updated.Revision++
	updated.UpdatedAt = time.Now().UTC()
	return &updated
}

func bookmarkConflictFromLookup(err error, current *models.MediaBookmark) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &BookmarkConflictError{Deleted: true}
	}
	if err != nil {
		return err
	}
	return &BookmarkConflictError{Current: current}
}

const bookmarkCASMaxAttempts = 2

type bookmarkCASBusyError struct {
	cause error
}

func (e *bookmarkCASBusyError) Error() string { return e.cause.Error() }
func (e *bookmarkCASBusyError) Unwrap() error { return e.cause }

func (s *Service) bookmarkCASResult(ctx context.Context, tx *gorm.DB, spaceID string, mediaID int64, id string, operationErr error) error {
	if operationErr != nil {
		if isSQLiteBusyError(operationErr) {
			return &bookmarkCASBusyError{cause: operationErr}
		}
		return operationErr
	}
	current, err := s.bookmarkRepo.GetTx(ctx, tx, spaceID, mediaID, id)
	return bookmarkConflictFromLookup(err, current)
}

func (s *Service) runBookmarkCAS(ctx context.Context, spaceID string, mediaID int64, id string, expectedRevision int64, operation func() error) error {
	for attempt := 0; attempt < bookmarkCASMaxAttempts; attempt++ {
		err := operation()
		var busy *bookmarkCASBusyError
		if !errors.As(err, &busy) {
			return err
		}
		conflict, lookupErr := s.bookmarkConflictAfterBusy(ctx, spaceID, mediaID, id, expectedRevision)
		if lookupErr != nil {
			return lookupErr
		}
		if conflict != nil {
			return conflict
		}
		if attempt == bookmarkCASMaxAttempts-1 {
			return busy.cause
		}
	}
	return nil
}

func (s *Service) bookmarkConflictAfterBusy(ctx context.Context, spaceID string, mediaID int64, id string, expectedRevision int64) (*BookmarkConflictError, error) {
	current, err := s.bookmarkRepo.Get(ctx, spaceID, mediaID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &BookmarkConflictError{Deleted: true}, nil
	}
	if err != nil {
		return nil, err
	}
	if current.Revision != expectedRevision {
		return &BookmarkConflictError{Current: current}, nil
	}
	return nil, nil
}

func (s *Service) recordBookmarkAudit(ctx context.Context, tx *gorm.DB, action string, before, after *models.MediaBookmark) error {
	if s.audit == nil {
		return nil
	}
	reference := after
	if reference == nil {
		reference = before
	}
	return s.audit.RecordTx(ctx, tx, audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: reference.SpaceID, Action: action,
		ResourceType: "bookmark", ResourceID: reference.ID,
		Before: bookmarkAuditSummary(before), After: bookmarkAuditSummary(after),
		Metadata: map[string]any{"media_id": reference.MediaID},
	})
}

func bookmarkAuditSummary(bookmark *models.MediaBookmark) any {
	if bookmark == nil {
		return nil
	}
	return map[string]any{
		"bookmark_id": bookmark.ID, "media_id": bookmark.MediaID,
		"position_ms": bookmark.PositionMS, "title": bookmark.Title, "revision": bookmark.Revision,
	}
}

func newBookmarkID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
