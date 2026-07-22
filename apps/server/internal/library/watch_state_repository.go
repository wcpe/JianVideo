package library

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// watchStateRepository 封装观看状态真源与兼容投影的持久化（FR2-070 续5/续24）。
// 事务入口用 *gorm.DB（与 bookmark 一致）；非事务读与列表走仓库自持连接。
type watchStateRepository interface {
	// GetWatchableMedia 非事务取可观看媒体（未软删且 active）；不存在返回 gorm.ErrRecordNotFound。
	GetWatchableMedia(spaceID string, mediaID int64) (models.MediaFile, error)
	// GetWatchableMediaTx 事务内取可观看媒体。
	GetWatchableMediaTx(tx *gorm.DB, spaceID string, mediaID int64) (models.MediaFile, error)
	// LoadState 非事务读取真源；不存在时 exists=false，state 为零值骨架。
	LoadState(spaceID string, mediaID int64) (state models.WatchState, exists bool, err error)
	// LoadStateTx 事务内读取真源。
	LoadStateTx(tx *gorm.DB, spaceID string, mediaID int64) (state models.WatchState, exists bool, err error)
	// PersistState 创建或 CAS 更新真源。
	PersistState(db *gorm.DB, current, next models.WatchState, exists bool) error
	// ProjectState 将真源投影到 media_files 兼容字段；incrementView 时 view_count+1。
	ProjectState(db *gorm.DB, mediaID int64, state models.WatchState, incrementView bool) error
	// RunInTx 在事务中执行 fn。
	RunInTx(fn func(tx *gorm.DB) error) error
	// ListHistory 按 last_watched_at/media_id 倒序列表；cursor 非空时做稳定游标过滤；limit 为实际取条（含 +1 探测由调用方处理）。
	ListHistory(spaceID string, watchedAt *time.Time, cursorMediaID int64, limit int) ([]models.WatchState, error)
	// ListContinue 返回未完成且 position>1 的状态。
	ListContinue(spaceID string, limit int) ([]models.WatchState, error)
	// ListMediaByIDs 批量取活跃未软删媒体（不保证顺序）。
	ListMediaByIDs(spaceID string, mediaIDs []int64) ([]models.MediaFile, error)
	// ListOnThisDay 那年今日媒体列表。
	ListOnThisDay(spaceID, monthDay, year string, limit int) ([]models.MediaFile, error)
}

type gormWatchStateRepository struct {
	db *gorm.DB
}

func newGormWatchStateRepository(db *gorm.DB) watchStateRepository {
	return &gormWatchStateRepository{db: db}
}

func (r *gormWatchStateRepository) GetWatchableMedia(spaceID string, mediaID int64) (models.MediaFile, error) {
	return r.GetWatchableMediaTx(r.db, spaceID, mediaID)
}

func (r *gormWatchStateRepository) GetWatchableMediaTx(tx *gorm.DB, spaceID string, mediaID int64) (models.MediaFile, error) {
	var media models.MediaFile
	err := tx.Where("space_id = ? AND id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), spaceID, mediaID).First(&media).Error
	return media, err
}

func (r *gormWatchStateRepository) LoadState(spaceID string, mediaID int64) (models.WatchState, bool, error) {
	return r.LoadStateTx(r.db, spaceID, mediaID)
}

func (r *gormWatchStateRepository) LoadStateTx(tx *gorm.DB, spaceID string, mediaID int64) (models.WatchState, bool, error) {
	var state models.WatchState
	err := tx.Where("space_id = ? AND media_id = ?", spaceID, mediaID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.WatchState{SpaceID: spaceID, MediaID: mediaID}, false, nil
	}
	if err != nil {
		return models.WatchState{}, false, err
	}
	return state, true, nil
}

func (r *gormWatchStateRepository) PersistState(db *gorm.DB, current, next models.WatchState, exists bool) error {
	if !exists {
		if err := db.Create(&next).Error; err != nil {
			latest, _, readErr := r.LoadStateTx(db, next.SpaceID, next.MediaID)
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
	latest, _, err := r.LoadStateTx(db, next.SpaceID, next.MediaID)
	if err != nil {
		return err
	}
	return &WatchStateConflictError{Current: latest}
}

func (r *gormWatchStateRepository) ProjectState(db *gorm.DB, mediaID int64, state models.WatchState, incrementView bool) error {
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

func (r *gormWatchStateRepository) RunInTx(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r *gormWatchStateRepository) listQuery(spaceID string) *gorm.DB {
	return r.db.Model(&models.WatchState{}).
		Joins("JOIN media_files ON media_files.id = watch_states.media_id AND media_files.space_id = watch_states.space_id").
		Where("watch_states.space_id = ? AND media_files.deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID))
}

func (r *gormWatchStateRepository) ListHistory(spaceID string, watchedAt *time.Time, cursorMediaID int64, limit int) ([]models.WatchState, error) {
	query := r.listQuery(spaceID)
	if watchedAt != nil {
		query = query.Where("watch_states.last_watched_at < ? OR (watch_states.last_watched_at = ? AND watch_states.media_id < ?)", *watchedAt, *watchedAt, cursorMediaID)
	}
	var states []models.WatchState
	err := query.Order("watch_states.last_watched_at DESC, watch_states.media_id DESC").Limit(limit).Find(&states).Error
	return states, err
}

func (r *gormWatchStateRepository) ListContinue(spaceID string, limit int) ([]models.WatchState, error) {
	var states []models.WatchState
	err := r.listQuery(spaceID).
		Where("watch_states.completed = ? AND watch_states.position_seconds > 1", false).
		Order("watch_states.last_watched_at DESC, watch_states.media_id DESC").
		Limit(limit).
		Find(&states).Error
	return states, err
}

func (r *gormWatchStateRepository) ListMediaByIDs(spaceID string, mediaIDs []int64) ([]models.MediaFile, error) {
	if len(mediaIDs) == 0 {
		return []models.MediaFile{}, nil
	}
	var mediaRows []models.MediaFile
	err := r.db.
		Where("space_id = ? AND id IN ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), mediaIDs).
		Find(&mediaRows).Error
	return mediaRows, err
}

func (r *gormWatchStateRepository) ListOnThisDay(spaceID, monthDay, year string, limit int) ([]models.MediaFile, error) {
	var items []models.MediaFile
	// media_time 以 UTC 存储；strftime 加 localtime 与服务器本地「今天」口径一致。
	err := r.db.
		Where("space_id = ? AND media_time IS NOT NULL AND deleted_at IS NULL AND "+activeFileStateCondition()+" AND strftime('%m-%d', media_time, 'localtime') = ? AND strftime('%Y', media_time, 'localtime') != ?", normalizeSpaceID(spaceID), monthDay, year).
		Order("media_time DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}
