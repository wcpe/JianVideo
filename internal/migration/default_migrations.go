package migration

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// DefaultSpaceID 是 v0.20 历史资源迁入 v2 时使用的默认 Space。
const DefaultSpaceID = "space-default"

// DefaultMigrations 返回 FR2-017 的最小版本化迁移集合。
func DefaultMigrations() []Migration {
	return []Migration{
		{
			ID:          "20260708_0001_baseline_schema",
			Description: "收敛既有基础 schema 与受控 AutoMigrate",
			SafeToRetry: true,
			Estimate:    estimateBaseline,
			Up:          migrateBaselineSchema,
			Validate:    validateBaselineSchema,
		},
		{
			ID:          "20260708_0002_default_space_backfill",
			Description: "创建默认 Space 并回填历史媒体库与媒体文件",
			SafeToRetry: true,
			Estimate:    estimateDefaultSpaceBackfill,
			Up:          migrateDefaultSpaceBackfill,
			Validate:    validateDefaultSpaceBackfill,
		},
		{
			ID:          "20260708_0003_key_indexes",
			Description: "创建 Space 归属与媒体分页 smoke 所需关键索引",
			SafeToRetry: true,
			Estimate:    estimateKeyIndexes,
			Up:          migrateKeyIndexes,
			Validate:    validateKeyIndexes,
		},
	}
}

func estimateBaseline(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateBaselineSchema(_ context.Context, tx *gorm.DB) error {
	if err := migrateCoreSchema(tx); err != nil {
		return err
	}
	return tx.AutoMigrate(
		&models.MediaExtension{},
		&models.Album{},
		&models.AlbumItem{},
		&models.Tag{},
		&models.TagMapping{},
		&models.Setting{},
		&models.ScanTask{},
		&models.Share{},
		&models.CodecProbeCache{},
		&models.MediaHealthIssue{},
		&models.TranscodePreset{},
		&models.TranscodeTask{},
		&models.MetricSample{},
	)
}

func migrateCoreSchema(tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS library_paths (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'local',
			label TEXT,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE TABLE IF NOT EXISTS media_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL,
			file_path TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_size INTEGER DEFAULT 0,
			format TEXT,
			video_codec TEXT,
			audio_codec TEXT,
			duration REAL DEFAULT 0,
			width INTEGER DEFAULT 0,
			height INTEGER DEFAULT 0,
			bitrate INTEGER DEFAULT 0,
			subtitle_tracks TEXT,
			added_at DATETIME,
			modified_at DATETIME
		);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_file_path ON media_files(file_path);`,
		`CREATE TABLE IF NOT EXISTS media_extensions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_id INTEGER NOT NULL,
			extension TEXT NOT NULL,
			type TEXT NOT NULL,
			is_built_in INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_extensions_library_extension ON media_extensions(library_id, extension);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("初始化核心 schema 失败: %w", err)
		}
	}
	columns := map[string]string{
		"display_name":      "TEXT DEFAULT ''",
		"deleted_at":        "DATETIME",
		"media_time":        "DATETIME",
		"media_time_source": "TEXT DEFAULT ''",
		"camera":            "TEXT DEFAULT ''",
		"lens":              "TEXT DEFAULT ''",
		"aperture":          "TEXT DEFAULT ''",
		"shutter":           "TEXT DEFAULT ''",
		"iso":               "INTEGER DEFAULT 0",
		"gps_lat":           "REAL DEFAULT 0",
		"gps_lon":           "REAL DEFAULT 0",
		"location":          "TEXT DEFAULT ''",
		"favorite":          "INTEGER DEFAULT 0",
		"notes":             "TEXT DEFAULT ''",
		"dhash":             "INTEGER DEFAULT 0",
		"last_position":     "REAL DEFAULT 0",
		"watched":           "INTEGER DEFAULT 0",
		"last_watched_at":   "DATETIME",
		"last_viewed_at":    "DATETIME",
		"view_count":        "INTEGER DEFAULT 0",
	}
	for column, definition := range columns {
		if err := addColumnIfMissing(tx, "media_files", column, definition); err != nil {
			return fmt.Errorf("迁移 media_files.%s 失败: %w", column, err)
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_files_library_id ON media_files(library_id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_file_name ON media_files(file_name);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_deleted_at ON media_files(deleted_at);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_media_time ON media_files(media_time);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_last_viewed_at ON media_files(last_viewed_at);`,
	}
	for _, stmt := range indexes {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("创建 media_files 索引失败: %w", err)
		}
	}
	return nil
}

func validateBaselineSchema(_ context.Context, db *gorm.DB) (Validation, error) {
	requiredTables := []string{
		"library_paths", "media_files", "media_extensions", "settings",
		"scan_tasks", "transcode_tasks", "shares", "metric_samples",
	}
	for _, table := range requiredTables {
		if !tableExists(db, table) {
			return Validation{}, fmt.Errorf("基础表不存在: %s", table)
		}
	}
	return Validation{Summary: "基础 schema 已就绪"}, nil
}

func estimateDefaultSpaceBackfill(_ context.Context, db *gorm.DB) (StepPlan, error) {
	var libraryCount, mediaCount int64
	if tableExists(db, "library_paths") {
		_ = db.Table("library_paths").Count(&libraryCount).Error
	}
	if tableExists(db, "media_files") {
		_ = db.Table("media_files").Count(&mediaCount).Error
	}
	return StepPlan{EstimatedRows: libraryCount + mediaCount + 1}, nil
}

func migrateDefaultSpaceBackfill(_ context.Context, tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS spaces (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`INSERT OR IGNORE INTO spaces(id, name, created_at)
		 VALUES ('` + DefaultSpaceID + `', '默认 Space', datetime('now'));`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	if err := addColumnIfMissing(tx, "library_paths", "space_id", "TEXT NOT NULL DEFAULT '"+DefaultSpaceID+"'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "media_files", "space_id", "TEXT NOT NULL DEFAULT '"+DefaultSpaceID+"'"); err != nil {
		return err
	}
	if err := tx.Exec("UPDATE library_paths SET space_id = ? WHERE space_id IS NULL OR space_id = ''", DefaultSpaceID).Error; err != nil {
		return err
	}
	return tx.Exec("UPDATE media_files SET space_id = ? WHERE space_id IS NULL OR space_id = ''", DefaultSpaceID).Error
}

func validateDefaultSpaceBackfill(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "spaces") {
		return Validation{}, fmt.Errorf("spaces 表不存在")
	}
	if count := countTableWhere(db, "spaces", "id = ?", DefaultSpaceID); count != 1 {
		return Validation{}, fmt.Errorf("默认 Space 不存在")
	}
	for _, table := range []string{"library_paths", "media_files"} {
		if !columnExists(db, table, "space_id") {
			return Validation{}, fmt.Errorf("%s 缺少 space_id", table)
		}
		if count := countTableWhere(db, table, "space_id IS NULL OR space_id = ''"); count != 0 {
			return Validation{}, fmt.Errorf("%s 存在未回填 space_id 的记录", table)
		}
	}
	return Validation{Summary: "默认 Space 回填完整"}, nil
}

func estimateKeyIndexes(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateKeyIndexes(_ context.Context, tx *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_library_paths_space_id ON library_paths(space_id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_id ON media_files(space_id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_library_added ON media_files(space_id, library_id, added_at, id);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateKeyIndexes(_ context.Context, db *gorm.DB) (Validation, error) {
	for _, indexName := range []string{
		"idx_media_files_library_id",
		"idx_library_paths_space_id",
		"idx_media_files_space_id",
		"idx_media_files_space_library_added",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("关键索引不存在: %s", indexName)
		}
	}
	rows, err := db.Raw(
		"SELECT id FROM media_files WHERE space_id = ? ORDER BY added_at DESC, id DESC LIMIT 1",
		DefaultSpaceID,
	).Rows()
	if err != nil {
		return Validation{}, fmt.Errorf("FR2-007 查询 smoke 失败: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()
	return Validation{Summary: "关键索引已就绪"}, nil
}

func addColumnIfMissing(db *gorm.DB, table, column, definition string) error {
	if columnExists(db, table, column) {
		return nil
	}
	return db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)).Error
}

func columnExists(db *gorm.DB, table, column string) bool {
	rows, err := db.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		return false
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func indexExists(db *gorm.DB, indexName string) bool {
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).
		Scan(&count).Error; err != nil {
		return false
	}
	return count == 1
}

func countTableWhere(db *gorm.DB, table, where string, args ...any) int64 {
	var count int64
	if err := db.Table(table).Where(where, args...).Count(&count).Error; err != nil {
		return -1
	}
	return count
}
