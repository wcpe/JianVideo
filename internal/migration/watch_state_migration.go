package migration

import (
	"context"
	"fmt"
	"log"

	"gorm.io/gorm"
)

func estimateWatchStates(_ context.Context, db *gorm.DB) (StepPlan, error) {
	if !hasLegacyWatchColumns(db) {
		return StepPlan{}, nil
	}
	valid := countTableWhere(db, "media_files", "last_watched_at IS NOT NULL AND (last_position > 0 OR watched = 1)")
	skipped := countTableWhere(db, "media_files", "last_watched_at IS NULL AND (last_position > 0 OR watched = 1)")
	if valid < 0 || skipped < 0 {
		return StepPlan{}, fmt.Errorf("统计旧观看状态失败")
	}
	plan := StepPlan{EstimatedRows: valid}
	if skipped > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("有 %d 条旧观看记录缺少 last_watched_at，迁移将跳过且不伪造观看时间", skipped))
	}
	return plan, nil
}

func migrateWatchStates(_ context.Context, tx *gorm.DB) error {
	if err := createWatchStateSchema(tx); err != nil || !hasLegacyWatchColumns(tx) {
		return err
	}
	warnSkippedLegacyWatchStates(tx)
	if err := backfillWatchStates(tx); err != nil {
		return err
	}
	return projectMigratedWatchStates(tx)
}

func createWatchStateSchema(tx *gorm.DB) error {
	for _, statement := range watchStateSchemaStatements() {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func warnSkippedLegacyWatchStates(tx *gorm.DB) {
	skipped := countTableWhere(tx, "media_files", "last_watched_at IS NULL AND (last_position > 0 OR watched = 1)")
	if skipped > 0 {
		log.Printf("[WARN] FR2-045 迁移跳过 %d 条缺少 last_watched_at 的旧观看记录，不伪造观看时间", skipped)
	}
}

func backfillWatchStates(tx *gorm.DB) error {
	return tx.Exec(`
		INSERT INTO watch_states(
			space_id, media_id, position_seconds, completed, last_watched_at, completed_at,
			revision, last_session_id, last_event_seq, created_at, updated_at
		)
		SELECT
			space_id,
			id,
			CASE WHEN watched = 1 THEN 0 WHEN last_position < 0 THEN 0 ELSE last_position END,
			CASE WHEN watched = 1 THEN 1 ELSE 0 END,
			last_watched_at,
			CASE WHEN watched = 1 THEN last_watched_at ELSE NULL END,
			1,
			'',
			0,
			last_watched_at,
			last_watched_at
		FROM media_files
		WHERE last_watched_at IS NOT NULL AND (last_position > 0 OR watched = 1)
		ON CONFLICT(space_id, media_id) DO NOTHING
	`).Error
}

func projectMigratedWatchStates(tx *gorm.DB) error {
	return tx.Exec(`
		UPDATE media_files
		SET last_position = (
				SELECT position_seconds FROM watch_states
				WHERE watch_states.space_id = media_files.space_id AND watch_states.media_id = media_files.id
			),
			watched = (
				SELECT completed FROM watch_states
				WHERE watch_states.space_id = media_files.space_id AND watch_states.media_id = media_files.id
			),
			last_watched_at = (
				SELECT last_watched_at FROM watch_states
				WHERE watch_states.space_id = media_files.space_id AND watch_states.media_id = media_files.id
			)
		WHERE EXISTS (
			SELECT 1 FROM watch_states
			WHERE watch_states.space_id = media_files.space_id AND watch_states.media_id = media_files.id
		)
	`).Error
}

func validateWatchStates(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "watch_states") {
		return Validation{}, fmt.Errorf("watch_states 表不存在")
	}
	for _, column := range []string{
		"space_id", "media_id", "position_seconds", "completed", "last_watched_at",
		"completed_at", "revision", "last_session_id", "last_event_seq", "created_at", "updated_at",
	} {
		if !columnExists(db, "watch_states", column) {
			return Validation{}, fmt.Errorf("watch_states 缺少 %s", column)
		}
	}
	for _, indexName := range []string{
		"idx_watch_states_space_media",
		"idx_watch_states_space_history",
		"idx_watch_states_space_continue",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("观看状态索引不存在: %s", indexName)
		}
	}
	if count := countTableWhere(db, "watch_states", `
		position_seconds < 0 OR revision < 0 OR last_event_seq < 0
		OR completed NOT IN (0, 1)
		OR (completed = 1 AND (position_seconds != 0 OR completed_at IS NULL))
		OR (completed = 0 AND completed_at IS NOT NULL)
	`); count != 0 {
		return Validation{}, fmt.Errorf("watch_states 存在 %d 条非法状态", count)
	}
	if count := countTableWhere(db, "watch_states", `NOT EXISTS (
		SELECT 1 FROM media_files
		WHERE media_files.id = watch_states.media_id AND media_files.space_id = watch_states.space_id
	)`); count != 0 {
		return Validation{}, fmt.Errorf("watch_states 存在 %d 条孤儿或 Space 归属错误记录", count)
	}
	if hasLegacyWatchColumns(db) {
		var missing int64
		err := db.Raw(`
			SELECT COUNT(*) FROM media_files
			WHERE last_watched_at IS NOT NULL AND (last_position > 0 OR watched = 1)
				AND NOT EXISTS (
					SELECT 1 FROM watch_states
					WHERE watch_states.space_id = media_files.space_id AND watch_states.media_id = media_files.id
				)
		`).Scan(&missing).Error
		if err != nil {
			return Validation{}, err
		}
		if missing != 0 {
			return Validation{}, fmt.Errorf("仍有 %d 条可迁移旧观看记录未回填", missing)
		}
		var projectionMismatch int64
		err = db.Raw(`
			SELECT COUNT(*) FROM watch_states
			JOIN media_files
				ON media_files.id = watch_states.media_id
				AND media_files.space_id = watch_states.space_id
			WHERE watch_states.position_seconds != media_files.last_position
				OR watch_states.completed != media_files.watched
				OR media_files.last_watched_at IS NULL
				OR watch_states.last_watched_at != media_files.last_watched_at
		`).Scan(&projectionMismatch).Error
		if err != nil {
			return Validation{}, err
		}
		if projectionMismatch != 0 {
			return Validation{}, fmt.Errorf("watch_states 有 %d 条记录与兼容投影不一致", projectionMismatch)
		}
	}
	return Validation{Summary: "FR2-045 观看状态真源、回填与查询索引已就绪"}, nil
}

func hasLegacyWatchColumns(db *gorm.DB) bool {
	if !tableExists(db, "media_files") {
		return false
	}
	for _, column := range []string{"space_id", "last_position", "watched", "last_watched_at"} {
		if !columnExists(db, "media_files", column) {
			return false
		}
	}
	return true
}

func watchStateSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS watch_states (
			space_id TEXT NOT NULL,
			media_id INTEGER NOT NULL,
			position_seconds REAL NOT NULL DEFAULT 0 CHECK(position_seconds >= 0),
			completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0, 1)),
			last_watched_at DATETIME NOT NULL,
			completed_at DATETIME,
			revision INTEGER NOT NULL DEFAULT 0 CHECK(revision >= 0),
			last_session_id TEXT NOT NULL DEFAULT '',
			last_event_seq INTEGER NOT NULL DEFAULT 0 CHECK(last_event_seq >= 0),
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY(space_id, media_id),
			CHECK(
				(completed = 0 AND completed_at IS NULL)
				OR (completed = 1 AND position_seconds = 0 AND completed_at IS NOT NULL)
			)
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_watch_states_space_media ON watch_states(space_id, media_id);`,
		`CREATE INDEX IF NOT EXISTS idx_watch_states_space_history ON watch_states(space_id, last_watched_at DESC, media_id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_watch_states_space_continue
			ON watch_states(space_id, last_watched_at DESC, media_id DESC)
			WHERE completed = 0 AND position_seconds > 1;`,
	}
}
