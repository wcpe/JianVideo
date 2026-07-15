package library

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	// WatchEventProgress 表示周期播放进度。
	WatchEventProgress = "progress"
	// WatchEventPause 表示暂停时补报。
	WatchEventPause = "pause"
	// WatchEventSeek 表示 seek 稳定后补报。
	WatchEventSeek = "seek"
	// WatchEventEnded 表示播放器明确播放结束。
	WatchEventEnded = "ended"

	// WatchReasonUser 表示用户直接产生的观看事件。
	WatchReasonUser = "user"
	// WatchReasonABLoop 表示 A-B 循环内部回跳。
	WatchReasonABLoop = "ab_loop"
	// WatchReasonRestore 表示恢复服务端续播位置。
	WatchReasonRestore = "restore"
	// WatchReasonSystem 表示播放器或系统驱动事件。
	WatchReasonSystem = "system"
)

const watchStateMaxLimit = 50

// WatchEventInput 是一次带 revision CAS 的观看状态更新。
type WatchEventInput struct {
	PositionSeconds  float64
	DurationSeconds  *float64
	ExpectedRevision int64
	SessionID        string
	EventSeq         int64
	EventType        string
	Reason           string
}

// WatchEventResult 返回事件是否应用以及当前完整状态。
type WatchEventResult struct {
	Applied bool              `json:"applied"`
	State   models.WatchState `json:"watch_state"`
}

// WatchStateConflictError 表示 expected_revision 已过期。
type WatchStateConflictError struct {
	Current models.WatchState
}

func (e *WatchStateConflictError) Error() string {
	return "观看状态 revision 冲突"
}

// WatchEventValidationError 表示观看事件字段不符合协议。
type WatchEventValidationError struct {
	Message string
}

func (e *WatchEventValidationError) Error() string {
	return e.Message
}

// WatchCursorError 表示观看历史游标无法解析。
type WatchCursorError struct{}

func (e *WatchCursorError) Error() string {
	return "观看历史游标无效"
}

// WatchMediaItem 把媒体信息与同一真源中的观看状态绑定返回。
type WatchMediaItem struct {
	Media models.MediaFile  `json:"media"`
	State models.WatchState `json:"watch_state"`
}

// WatchHistoryPage 是稳定游标观看历史页。
type WatchHistoryPage struct {
	Items      []WatchMediaItem `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// GetWatchStateInSpace 返回指定 Space 媒体的真源状态；未观看时返回 revision=0。
func (s *Service) GetWatchStateInSpace(spaceID string, mediaID int64) (models.WatchState, error) {
	spaceID = normalizeSpaceID(spaceID)
	if _, err := s.getWatchableMedia(s.db, spaceID, mediaID); err != nil {
		return models.WatchState{}, err
	}
	var state models.WatchState
	err := s.db.Where("space_id = ? AND media_id = ?", spaceID, mediaID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.WatchState{SpaceID: spaceID, MediaID: mediaID}, nil
	}
	return state, err
}

// ApplyWatchEventInSpace 在一个事务中完成 CAS、真源写入、兼容投影与观看次数更新。
func (s *Service) ApplyWatchEventInSpace(spaceID string, mediaID int64, input WatchEventInput) (WatchEventResult, error) {
	if err := validateWatchEventInput(input); err != nil {
		return WatchEventResult{}, err
	}
	spaceID = normalizeSpaceID(spaceID)
	var result WatchEventResult
	for attempt := 0; attempt < 3; attempt++ {
		result = WatchEventResult{}
		err := s.applyWatchEventOnce(spaceID, mediaID, input, &result)
		if err == nil || !isSQLiteBusyError(err) {
			return result, err
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	return result, fmt.Errorf("SQLite 观看状态事务持续争用")
}

func (s *Service) applyWatchEventOnce(spaceID string, mediaID int64, input WatchEventInput, output *WatchEventResult) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		media, err := s.getWatchableMedia(tx, spaceID, mediaID)
		if err != nil {
			return err
		}
		current, exists, err := loadWatchState(tx, spaceID, mediaID)
		if err != nil {
			return err
		}
		if exists && input.SessionID == current.LastSessionID && input.EventSeq <= current.LastEventSeq {
			*output = WatchEventResult{Applied: false, State: current}
			return nil
		}
		if input.ExpectedRevision != current.Revision {
			return &WatchStateConflictError{Current: current}
		}

		now := s.now().UTC()
		next, completedTransition := deriveNextWatchState(current, exists, media, input, now)
		if err := persistWatchState(tx, current, next, exists); err != nil {
			return err
		}
		if err := projectWatchState(tx, mediaID, next, completedTransition); err != nil {
			return err
		}
		*output = WatchEventResult{Applied: true, State: next}
		return nil
	})
}

func (s *Service) getWatchableMedia(db *gorm.DB, spaceID string, mediaID int64) (models.MediaFile, error) {
	var media models.MediaFile
	err := db.Where("space_id = ? AND id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), spaceID, mediaID).First(&media).Error
	return media, err
}

func loadWatchState(db *gorm.DB, spaceID string, mediaID int64) (models.WatchState, bool, error) {
	var state models.WatchState
	err := db.Where("space_id = ? AND media_id = ?", spaceID, mediaID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.WatchState{SpaceID: spaceID, MediaID: mediaID}, false, nil
	}
	return state, err == nil, err
}

func deriveNextWatchState(
	current models.WatchState,
	exists bool,
	media models.MediaFile,
	input WatchEventInput,
	now time.Time,
) (models.WatchState, bool) {
	duration := effectiveWatchDuration(media.Duration, input.DurationSeconds)
	position := input.PositionSeconds
	if duration > 0 && position > duration {
		position = duration
	}
	completed := shouldCompleteWatch(position, duration, input.EventType, input.Reason)
	completedTransition := completed && (!exists || !current.Completed)
	completedAt := current.CompletedAt
	if completedTransition {
		completedAt = &now
	} else if !completed {
		completedAt = nil
	}
	if completed {
		position = 0
	}
	createdAt := current.CreatedAt
	if !exists {
		createdAt = now
	}
	return models.WatchState{
		SpaceID:         media.SpaceID,
		MediaID:         media.ID,
		PositionSeconds: position,
		Completed:       completed,
		LastWatchedAt:   now,
		CompletedAt:     completedAt,
		Revision:        current.Revision + 1,
		LastSessionID:   input.SessionID,
		LastEventSeq:    input.EventSeq,
		CreatedAt:       createdAt,
		UpdatedAt:       now,
	}, completedTransition
}

func effectiveWatchDuration(mediaDuration float64, clientDuration *float64) float64 {
	if mediaDuration > 0 && !math.IsNaN(mediaDuration) && !math.IsInf(mediaDuration, 0) {
		return mediaDuration
	}
	if clientDuration != nil && *clientDuration > 0 && !math.IsNaN(*clientDuration) && !math.IsInf(*clientDuration, 0) {
		return *clientDuration
	}
	return 0
}

func shouldCompleteWatch(position, duration float64, eventType, reason string) bool {
	if reason == WatchReasonABLoop {
		return false
	}
	if eventType == WatchEventEnded {
		return true
	}
	if duration <= 0 {
		return false
	}
	ratio := position / duration
	remaining := duration - position
	return ratio >= 0.95 || (remaining <= 15 && ratio >= 0.8)
}

func persistWatchState(db *gorm.DB, current, next models.WatchState, exists bool) error {
	if !exists {
		if err := db.Create(&next).Error; err != nil {
			latest, _, readErr := loadWatchState(db, next.SpaceID, next.MediaID)
			if readErr == nil && latest.Revision != 0 {
				return &WatchStateConflictError{Current: latest}
			}
			return err
		}
		return nil
	}
	result := db.Model(&models.WatchState{}).
		Where("space_id = ? AND media_id = ? AND revision = ?", next.SpaceID, next.MediaID, current.Revision).
		Updates(map[string]any{
			"position_seconds": next.PositionSeconds,
			"completed":        next.Completed,
			"last_watched_at":  next.LastWatchedAt,
			"completed_at":     next.CompletedAt,
			"revision":         next.Revision,
			"last_session_id":  next.LastSessionID,
			"last_event_seq":   next.LastEventSeq,
			"updated_at":       next.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	latest, _, err := loadWatchState(db, next.SpaceID, next.MediaID)
	if err != nil {
		return err
	}
	return &WatchStateConflictError{Current: latest}
}

func projectWatchState(db *gorm.DB, mediaID int64, state models.WatchState, incrementView bool) error {
	updates := map[string]any{
		"last_position":   state.PositionSeconds,
		"watched":         state.Completed,
		"last_watched_at": state.LastWatchedAt,
	}
	if incrementView {
		updates["view_count"] = gorm.Expr("view_count + 1")
	}
	result := db.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", state.SpaceID, mediaID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("观看状态兼容投影未更新媒体")
	}
	return nil
}

// ListWatchHistoryInSpace 按 last_watched_at、media_id 倒序返回稳定游标页。
func (s *Service) ListWatchHistoryInSpace(spaceID, cursor string, limit int) (WatchHistoryPage, error) {
	spaceID = normalizeSpaceID(spaceID)
	limit = normalizeWatchLimit(limit, 20)
	query := s.watchStateListQuery(spaceID)
	if cursor != "" {
		watchedAt, mediaID, err := decodeWatchCursor(cursor)
		if err != nil {
			return WatchHistoryPage{}, err
		}
		query = query.Where("watch_states.last_watched_at < ? OR (watch_states.last_watched_at = ? AND watch_states.media_id < ?)", watchedAt, watchedAt, mediaID)
	}
	var states []models.WatchState
	if err := query.Order("watch_states.last_watched_at DESC, watch_states.media_id DESC").Limit(limit + 1).Find(&states).Error; err != nil {
		return WatchHistoryPage{}, err
	}
	page := WatchHistoryPage{}
	if len(states) > limit {
		last := states[limit-1]
		page.NextCursor = encodeWatchCursor(last.LastWatchedAt, last.MediaID)
		states = states[:limit]
	}
	items, err := s.watchMediaItems(spaceID, states)
	if err != nil {
		return WatchHistoryPage{}, err
	}
	page.Items = items
	return page, nil
}

// ListContinueWatchingStatesInSpace 从真源返回未完成且位置大于一秒的媒体。
func (s *Service) ListContinueWatchingStatesInSpace(spaceID string, limit int) ([]WatchMediaItem, error) {
	spaceID = normalizeSpaceID(spaceID)
	limit = normalizeWatchLimit(limit, 12)
	var states []models.WatchState
	err := s.watchStateListQuery(spaceID).
		Where("watch_states.completed = ? AND watch_states.position_seconds > 1", false).
		Order("watch_states.last_watched_at DESC, watch_states.media_id DESC").
		Limit(limit).
		Find(&states).Error
	if err != nil {
		return nil, err
	}
	return s.watchMediaItems(spaceID, states)
}

func (s *Service) watchStateListQuery(spaceID string) *gorm.DB {
	return s.db.Model(&models.WatchState{}).
		Joins("JOIN media_files ON media_files.id = watch_states.media_id AND media_files.space_id = watch_states.space_id").
		Where("watch_states.space_id = ? AND media_files.deleted_at IS NULL AND "+activeFileStateCondition(), spaceID)
}

func (s *Service) watchMediaItems(spaceID string, states []models.WatchState) ([]WatchMediaItem, error) {
	if len(states) == 0 {
		return []WatchMediaItem{}, nil
	}
	ids := make([]int64, 0, len(states))
	for _, state := range states {
		ids = append(ids, state.MediaID)
	}
	var mediaRows []models.MediaFile
	if err := s.db.
		Where("space_id = ? AND id IN ? AND deleted_at IS NULL AND "+activeFileStateCondition(), spaceID, ids).
		Find(&mediaRows).Error; err != nil {
		return nil, err
	}
	mediaByID := make(map[int64]models.MediaFile, len(mediaRows))
	for _, media := range mediaRows {
		mediaByID[media.ID] = media
	}
	items := make([]WatchMediaItem, 0, len(states))
	for _, state := range states {
		media, ok := mediaByID[state.MediaID]
		if !ok {
			continue
		}
		items = append(items, WatchMediaItem{Media: media, State: state})
	}
	return items, nil
}

func normalizeWatchLimit(limit, fallback int) int {
	if limit < 1 {
		return fallback
	}
	if limit > watchStateMaxLimit {
		return watchStateMaxLimit
	}
	return limit
}

func encodeWatchCursor(watchedAt time.Time, mediaID int64) string {
	raw := watchedAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(mediaID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeWatchCursor(cursor string) (time.Time, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, &WatchCursorError{}
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return time.Time{}, 0, &WatchCursorError{}
	}
	watchedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, &WatchCursorError{}
	}
	mediaID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || mediaID <= 0 {
		return time.Time{}, 0, &WatchCursorError{}
	}
	return watchedAt, mediaID, nil
}

func validateWatchEventInput(input WatchEventInput) error {
	if input.PositionSeconds < 0 || math.IsNaN(input.PositionSeconds) || math.IsInf(input.PositionSeconds, 0) {
		return invalidWatchEvent("观看位置必须为有限非负数")
	}
	if input.ExpectedRevision < 0 {
		return invalidWatchEvent("expected_revision 不能为负数")
	}
	if input.EventSeq < 0 {
		return invalidWatchEvent("event_seq 不能为负数")
	}
	if !validWatchSessionID(input.SessionID) {
		return invalidWatchEvent("session_id 格式无效")
	}
	switch input.EventType {
	case WatchEventProgress, WatchEventPause, WatchEventSeek, WatchEventEnded:
	default:
		return invalidWatchEvent("event_type 无效")
	}
	switch input.Reason {
	case WatchReasonUser, WatchReasonABLoop, WatchReasonRestore, WatchReasonSystem:
	default:
		return invalidWatchEvent("reason 无效")
	}
	if input.DurationSeconds != nil && (*input.DurationSeconds <= 0 || math.IsNaN(*input.DurationSeconds) || math.IsInf(*input.DurationSeconds, 0)) {
		return invalidWatchEvent("duration_seconds 必须为有限正数")
	}
	return nil
}

func invalidWatchEvent(message string) error {
	return &WatchEventValidationError{Message: message}
}

func validWatchSessionID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func isSQLiteBusyError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

func (s *Service) applyLegacyWatchEvent(spaceID string, mediaID int64, position float64, sessionID, eventType string) error {
	for attempt := 0; attempt < 3; attempt++ {
		current, err := s.GetWatchStateInSpace(spaceID, mediaID)
		if err != nil {
			return err
		}
		eventSeq := int64(1)
		if current.LastSessionID == sessionID {
			eventSeq = current.LastEventSeq + 1
		}
		_, err = s.ApplyWatchEventInSpace(spaceID, mediaID, WatchEventInput{
			PositionSeconds:  position,
			ExpectedRevision: current.Revision,
			SessionID:        sessionID,
			EventSeq:         eventSeq,
			EventType:        eventType,
			Reason:           WatchReasonUser,
		})
		var conflict *WatchStateConflictError
		if !errors.As(err, &conflict) {
			return err
		}
	}
	return fmt.Errorf("旧观看端点连续发生 revision 冲突")
}
