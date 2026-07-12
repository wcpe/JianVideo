package migration

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// DefaultSpaceID 是 v0.20 历史资源迁入 v2 时使用的默认 Space。
const DefaultSpaceID = models.DefaultSpaceID

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
		{
			ID:          "20260708_0004_fr2_007_space_owner_indexes",
			Description: "补齐 Space owner 元数据与媒体查询组合索引",
			SafeToRetry: true,
			Estimate:    estimateFR2007Indexes,
			Up:          migrateFR2007SpaceOwnerIndexes,
			Validate:    validateFR2007SpaceOwnerIndexes,
		},
		{
			ID:          "20260708_0005_fr2_040_audit_events",
			Description: "补齐审计事件正式字段与查询索引",
			SafeToRetry: true,
			Estimate:    estimateAuditEvents,
			Up:          migrateAuditEvents,
			Validate:    validateAuditEvents,
		},
		{
			ID:          "20260708_0006_fr2_037_tasks_center",
			Description: "建立通用任务队列中心并回填旧扫描转码任务",
			SafeToRetry: true,
			Estimate:    estimateTasksCenter,
			Up:          migrateTasksCenter,
			Validate:    validateTasksCenter,
		},
		{
			ID:          "20260708_0007_fr2_052_library_kinds",
			Description: "补齐媒体库内容分型与扫描上下文索引",
			SafeToRetry: true,
			Estimate:    estimateLibraryKinds,
			Up:          migrateLibraryKinds,
			Validate:    validateLibraryKinds,
		},
		{
			ID:          "20260708_0008_fr2_025_media_type_rules",
			Description: "建立媒体类型规则表并回填旧后缀配置",
			SafeToRetry: true,
			Estimate:    estimateMediaTypeRules,
			Up:          migrateMediaTypeRules,
			Validate:    validateMediaTypeRules,
		},
		{
			ID:          "20260708_0009_fr2_027_media_file_state",
			Description: "补齐媒体文件缺失状态字段与扫描任务载荷",
			SafeToRetry: true,
			Estimate:    estimateMediaFileState,
			Up:          migrateMediaFileState,
			Validate:    validateMediaFileState,
		},
		{
			ID:          "20260708_0010_fr2_061_file_hash_dedup",
			Description: "补齐媒体文件内容哈希字段与精确去重索引",
			SafeToRetry: true,
			Estimate:    estimateFileHashDedup,
			Up:          migrateFileHashDedup,
			Validate:    validateFileHashDedup,
		},
		{
			ID:          "20260708_0011_fr2_031_media_inferences",
			Description: "建立本地离线影视信息推断表",
			SafeToRetry: true,
			Estimate:    estimateMediaInferences,
			Up:          migrateMediaInferences,
			Validate:    validateMediaInferences,
		},
		{
			ID:          "20260709_0009_fr2_048_cache_assets",
			Description: "建立缓存资产登记表与白名单清理索引",
			SafeToRetry: true,
			Estimate:    estimateCacheAssets,
			Up:          migrateCacheAssets,
			Validate:    validateCacheAssets,
		},
		{
			ID:          "20260712_0012_space_owned_collections_health",
			Description: "回填相册分享与健康问题的 Space 归属",
			SafeToRetry: true,
			Estimate:    estimateSpaceOwnedCollections,
			Up:          migrateSpaceOwnedCollections,
			Validate:    validateSpaceOwnedCollections,
		},
		{
			ID:          "20260712_0013_fr2_007_active_query_indexes",
			Description: "补齐活跃媒体分页查询 partial 组合索引",
			SafeToRetry: true,
			Estimate:    estimateFR2007ActiveQueryIndexes,
			Up:          migrateFR2007ActiveQueryIndexes,
			Validate:    validateFR2007ActiveQueryIndexes,
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
		&models.Album{},
		&models.AlbumItem{},
		&models.Tag{},
		&models.TagMapping{},
		&models.Setting{},
		&models.ScanTask{},
		&models.Task{},
		&models.Share{},
		&models.CodecProbeCache{},
		&models.MediaHealthIssue{},
		&models.TranscodePreset{},
		&models.TranscodeTask{},
		&models.MetricSample{},
		&models.MediaHashGroup{},
		&models.MediaInference{},
	)
}

func migrateCoreSchema(tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS library_paths (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'local',
			library_kind TEXT NOT NULL DEFAULT 'mixed',
			library_profile_json TEXT NOT NULL DEFAULT '{}',
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
		`CREATE TABLE IF NOT EXISTS media_type_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			space_id TEXT NOT NULL DEFAULT '` + DefaultSpaceID + `',
			library_id INTEGER,
			type TEXT NOT NULL,
			extension TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			builtin INTEGER NOT NULL DEFAULT 0,
			capabilities_json TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_type_rules_space_global_type_ext
			ON media_type_rules(space_id, type, extension)
			WHERE library_id IS NULL;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_type_rules_space_library_type_ext
			ON media_type_rules(space_id, library_id, type, extension)
			WHERE library_id IS NOT NULL;`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("初始化核心 schema 失败: %w", err)
		}
	}
	columns := map[string]string{
		"display_name":             "TEXT DEFAULT ''",
		"deleted_at":               "DATETIME",
		"file_state":               "TEXT NOT NULL DEFAULT 'available'",
		"media_time":               "DATETIME",
		"media_time_source":        "TEXT DEFAULT ''",
		"camera":                   "TEXT DEFAULT ''",
		"lens":                     "TEXT DEFAULT ''",
		"aperture":                 "TEXT DEFAULT ''",
		"shutter":                  "TEXT DEFAULT ''",
		"iso":                      "INTEGER DEFAULT 0",
		"gps_lat":                  "REAL DEFAULT 0",
		"gps_lon":                  "REAL DEFAULT 0",
		"location":                 "TEXT DEFAULT ''",
		"favorite":                 "INTEGER DEFAULT 0",
		"notes":                    "TEXT DEFAULT ''",
		"dhash":                    "INTEGER DEFAULT 0",
		"content_hash":             "TEXT DEFAULT ''",
		"content_hash_algo":        "TEXT DEFAULT ''",
		"content_hash_computed_at": "DATETIME",
		"content_hash_stale":       "INTEGER NOT NULL DEFAULT 1",
		"last_position":            "REAL DEFAULT 0",
		"watched":                  "INTEGER DEFAULT 0",
		"last_watched_at":          "DATETIME",
		"last_viewed_at":           "DATETIME",
		"view_count":               "INTEGER DEFAULT 0",
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
		`CREATE INDEX IF NOT EXISTS idx_media_files_file_state ON media_files(file_state);`,
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
		"scan_tasks", "transcode_tasks", "tasks", "shares", "metric_samples",
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
			owner_user_id INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	if err := addColumnIfMissing(tx, "spaces", "owner_user_id", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "spaces", "updated_at", "DATETIME"); err != nil {
		return err
	}
	if err := tx.Exec(`
		INSERT OR IGNORE INTO spaces(id, name, owner_user_id, created_at, updated_at)
		VALUES (?, '默认 Space', COALESCE((SELECT id FROM users ORDER BY id LIMIT 1), 1), datetime('now'), datetime('now'))
	`, DefaultSpaceID).Error; err != nil {
		return err
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

func estimateFR2007Indexes(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateFR2007SpaceOwnerIndexes(_ context.Context, tx *gorm.DB) error {
	if err := addColumnIfMissing(tx, "spaces", "owner_user_id", "INTEGER"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "spaces", "updated_at", "DATETIME"); err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE spaces
		SET owner_user_id = COALESCE((SELECT id FROM users ORDER BY id LIMIT 1), 1)
		WHERE owner_user_id IS NULL OR owner_user_id = 0
	`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE spaces
		SET updated_at = COALESCE(created_at, datetime('now'))
		WHERE updated_at IS NULL OR updated_at = ''
	`).Error; err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "tags", "space_id", "TEXT NOT NULL DEFAULT '"+DefaultSpaceID+"'"); err != nil {
		return err
	}
	if err := tx.Exec("UPDATE tags SET space_id = ? WHERE space_id IS NULL OR space_id = ''", DefaultSpaceID).Error; err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "scan_tasks", "space_id", "TEXT NOT NULL DEFAULT '"+DefaultSpaceID+"'"); err != nil {
		return err
	}
	if err := tx.Exec("UPDATE scan_tasks SET space_id = ? WHERE space_id IS NULL OR space_id = ''", DefaultSpaceID).Error; err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "transcode_tasks", "space_id", "TEXT NOT NULL DEFAULT '"+DefaultSpaceID+"'"); err != nil {
		return err
	}
	if err := tx.Exec("UPDATE transcode_tasks SET space_id = ? WHERE space_id IS NULL OR space_id = ''", DefaultSpaceID).Error; err != nil {
		return err
	}
	statements := []string{
		`DROP INDEX IF EXISTS idx_library_paths_path;`,
		`DROP INDEX IF EXISTS idx_tags_name;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_library_paths_space_path ON library_paths(space_id, path);`,
		`CREATE INDEX IF NOT EXISTS idx_library_paths_space_path_id ON library_paths(space_id, path, id);`,
		`CREATE INDEX IF NOT EXISTS idx_library_paths_space_enabled_id ON library_paths(space_id, enabled, id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_added_id ON media_files(space_id, added_at DESC, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_media_time_id ON media_files(space_id, media_time DESC, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_library_path_id ON media_files(space_id, library_id, file_path, id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_deleted_id ON media_files(space_id, deleted_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_format_added_id ON media_files(space_id, format, added_at DESC, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_tags_space_id ON tags(space_id);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_space_name ON tags(space_id, name);`,
		`CREATE INDEX IF NOT EXISTS idx_scan_tasks_space_status_created ON scan_tasks(space_id, status, created_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_transcode_tasks_space_status_created ON transcode_tasks(space_id, status, created_at, id);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateFR2007SpaceOwnerIndexes(_ context.Context, db *gorm.DB) (Validation, error) {
	for _, column := range []string{"owner_user_id", "updated_at"} {
		if !columnExists(db, "spaces", column) {
			return Validation{}, fmt.Errorf("spaces 缺少 %s", column)
		}
	}
	if !columnExists(db, "tags", "space_id") {
		return Validation{}, fmt.Errorf("tags 缺少 space_id")
	}
	if !columnExists(db, "scan_tasks", "space_id") {
		return Validation{}, fmt.Errorf("scan_tasks 缺少 space_id")
	}
	if !columnExists(db, "transcode_tasks", "space_id") {
		return Validation{}, fmt.Errorf("transcode_tasks 缺少 space_id")
	}
	if count := countTableWhere(db, "spaces", "id = ? AND owner_user_id IS NOT NULL AND owner_user_id > 0", DefaultSpaceID); count != 1 {
		return Validation{}, fmt.Errorf("默认 Space owner 缺失")
	}
	for _, indexName := range fr2007IndexNames() {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("FR2-007 组合索引不存在: %s", indexName)
		}
	}
	return Validation{Summary: "FR2-007 Space owner 与组合索引已就绪"}, nil
}

func fr2007IndexNames() []string {
	return []string{
		"idx_library_paths_space_path_id",
		"idx_library_paths_space_path",
		"idx_library_paths_space_enabled_id",
		"idx_media_files_space_added_id",
		"idx_media_files_space_media_time_id",
		"idx_media_files_space_library_path_id",
		"idx_media_files_space_deleted_id",
		"idx_media_files_space_format_added_id",
		"idx_tags_space_id",
		"idx_tags_space_name",
		"idx_scan_tasks_space_status_created",
		"idx_transcode_tasks_space_status_created",
	}
}

const activeMediaPartialPredicate = "deleted_at IS NULL AND (file_state IS NULL OR file_state = '' OR file_state = 'available')"

func estimateFR2007ActiveQueryIndexes(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateFR2007ActiveQueryIndexes(_ context.Context, tx *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_files_active_space_added_id ON media_files(space_id, added_at DESC, id DESC) WHERE ` + activeMediaPartialPredicate + `;`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_active_space_library_added_id ON media_files(space_id, library_id, added_at DESC, id DESC) WHERE ` + activeMediaPartialPredicate + `;`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_active_space_format_added_id ON media_files(space_id, LOWER(format), added_at DESC, id DESC) WHERE ` + activeMediaPartialPredicate + `;`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateFR2007ActiveQueryIndexes(_ context.Context, db *gorm.DB) (Validation, error) {
	definitions := map[string]string{
		"idx_media_files_active_space_added_id":         "ON media_files(space_id, added_at DESC, id DESC)",
		"idx_media_files_active_space_library_added_id": "ON media_files(space_id, library_id, added_at DESC, id DESC)",
		"idx_media_files_active_space_format_added_id":  "ON media_files(space_id, LOWER(format), added_at DESC, id DESC)",
	}
	for name, columns := range definitions {
		if !indexDefinitionContains(db, name, columns+" WHERE "+activeMediaPartialPredicate) {
			return Validation{}, fmt.Errorf("FR2-007 活跃媒体索引定义不正确: %s", name)
		}
	}
	return Validation{Summary: "FR2-007 活跃媒体 partial 组合索引已就绪"}, nil
}

func indexDefinitionContains(db *gorm.DB, indexName, expected string) bool {
	var definition string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).
		Scan(&definition).Error; err != nil {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(definition), " "), expected)
}

func estimateAuditEvents(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateAuditEvents(_ context.Context, tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope TEXT NOT NULL,
			space_id TEXT,
			actor_type TEXT NOT NULL DEFAULT 'system',
			actor_id TEXT,
			action TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL DEFAULT '',
			resource_type TEXT NOT NULL DEFAULT '',
			resource_id TEXT,
			migration_id TEXT,
			message TEXT,
			before_json TEXT,
			after_json TEXT,
			metadata_json TEXT,
			request_id TEXT,
			created_at DATETIME NOT NULL
		);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	columns := map[string]string{
		"actor_type":    "TEXT NOT NULL DEFAULT 'system'",
		"actor_id":      "TEXT",
		"action":        "TEXT NOT NULL DEFAULT ''",
		"resource_type": "TEXT NOT NULL DEFAULT ''",
		"resource_id":   "TEXT",
		"before_json":   "TEXT",
		"after_json":    "TEXT",
		"request_id":    "TEXT",
	}
	for column, definition := range columns {
		if err := addColumnIfMissing(tx, "audit_events", column, definition); err != nil {
			return fmt.Errorf("迁移 audit_events.%s 失败: %w", column, err)
		}
	}
	statements = []string{
		`UPDATE audit_events SET action = event_type WHERE action = '' AND event_type != '';`,
		`UPDATE audit_events SET resource_type = 'migration' WHERE resource_type = '' AND event_type LIKE 'migration.%';`,
		`UPDATE audit_events SET resource_id = migration_id WHERE (resource_id IS NULL OR resource_id = '') AND migration_id IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_scope_created ON audit_events(scope, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_scope_space_created ON audit_events(scope, space_id, created_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_action_created ON audit_events(action, created_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_resource_created ON audit_events(resource_type, resource_id, created_at, id);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateAuditEvents(_ context.Context, db *gorm.DB) (Validation, error) {
	for _, column := range []string{"actor_type", "action", "resource_type", "before_json", "after_json", "request_id"} {
		if !columnExists(db, "audit_events", column) {
			return Validation{}, fmt.Errorf("audit_events 缺少 %s", column)
		}
	}
	for _, indexName := range []string{
		"idx_audit_events_scope_space_created",
		"idx_audit_events_action_created",
		"idx_audit_events_resource_created",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("审计索引不存在: %s", indexName)
		}
	}
	return Validation{Summary: "审计事件正式字段与索引已就绪"}, nil
}

func estimateTasksCenter(_ context.Context, db *gorm.DB) (StepPlan, error) {
	var scanCount, transcodeCount int64
	if tableExists(db, "scan_tasks") {
		_ = db.Table("scan_tasks").Count(&scanCount).Error
	}
	if tableExists(db, "transcode_tasks") {
		_ = db.Table("transcode_tasks").Count(&transcodeCount).Error
	}
	return StepPlan{EstimatedRows: scanCount + transcodeCount}, nil
}

func migrateTasksCenter(_ context.Context, tx *gorm.DB) error {
	if err := tx.AutoMigrate(&models.Task{}); err != nil {
		return err
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_tasks_space_status_priority_created ON tasks(space_id, status, priority, created_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_type_status_priority_created ON tasks(type, status, priority, created_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_space_type_status_updated ON tasks(space_id, type, status, updated_at);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_scope_space_type_status_id ON tasks(scope, space_id, type, status, id);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status_next_run_priority_created ON tasks(status, next_run_at, priority, created_at, id);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_idempotency_active ON tasks(idempotency_key) WHERE idempotency_key <> '' AND status IN ('pending', 'running');`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	if tableExists(tx, "scan_tasks") {
		if err := backfillScanTasks(tx); err != nil {
			return err
		}
	}
	if tableExists(tx, "transcode_tasks") {
		return backfillTranscodeTasks(tx)
	}
	return nil
}

func backfillScanTasks(tx *gorm.DB) error {
	return tx.Exec(`
		INSERT INTO tasks(
			scope, space_id, type, status, priority, attempts, max_attempts, progress,
			checkpoint, idempotency_key, payload_json, resource_type, resource_id, error,
			created_at, updated_at, started_at, finished_at
		)
		SELECT
			'space',
			COALESCE(NULLIF(space_id, ''), ?),
			'library.scan',
			CASE status
				WHEN 'completed' THEN 'succeeded'
				WHEN 'error' THEN 'failed'
				ELSE status
			END,
			0,
			CASE WHEN status = 'error' THEN 1 ELSE 0 END,
			1,
			CASE
				WHEN status = 'completed' THEN 100
				WHEN total_files > 0 THEN CAST((scanned_files * 100) / total_files AS INTEGER)
				ELSE 0
			END,
			'',
			'scan:' || id,
			json_object('legacy_table', 'scan_tasks', 'legacy_id', id, 'library_id', library_id, 'scan_type', scan_type),
			'library',
			CAST(library_id AS TEXT),
			COALESCE(error, ''),
			COALESCE(created_at, datetime('now')),
			COALESCE(completed_at, started_at, created_at, datetime('now')),
			started_at,
			completed_at
		FROM scan_tasks
		WHERE NOT EXISTS (
			SELECT 1 FROM tasks WHERE idempotency_key = 'scan:' || scan_tasks.id
		)
	`, DefaultSpaceID).Error
}

func backfillTranscodeTasks(tx *gorm.DB) error {
	return tx.Exec(`
		INSERT INTO tasks(
			scope, space_id, type, status, priority, attempts, max_attempts, progress,
			checkpoint, idempotency_key, payload_json, resource_type, resource_id, error,
			created_at, updated_at, started_at, finished_at
		)
		SELECT
			'space',
			COALESCE(NULLIF(space_id, ''), ?),
			'transcode.hls',
			CASE status
				WHEN 'completed' THEN 'succeeded'
				WHEN 'error' THEN 'failed'
				ELSE status
			END,
			0,
			CASE WHEN status = 'error' THEN 1 ELSE 0 END,
			1,
			CASE WHEN status = 'completed' THEN 100 ELSE 0 END,
			'',
			'transcode:' || id,
			json_object('legacy_table', 'transcode_tasks', 'legacy_id', id, 'media_id', media_id, 'preset_id', preset_id, 'codec', codec, 'width', width, 'height', height),
			'media',
			CAST(media_id AS TEXT),
			COALESCE(error, ''),
			COALESCE(created_at, datetime('now')),
			COALESCE(completed_at, started_at, created_at, datetime('now')),
			started_at,
			completed_at
		FROM transcode_tasks
		WHERE NOT EXISTS (
			SELECT 1 FROM tasks WHERE idempotency_key = 'transcode:' || transcode_tasks.id
		)
	`, DefaultSpaceID).Error
}

func validateTasksCenter(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "tasks") {
		return Validation{}, fmt.Errorf("tasks 表不存在")
	}
	for _, indexName := range []string{
		"idx_tasks_space_status_priority_created",
		"idx_tasks_type_status_priority_created",
		"idx_tasks_space_type_status_updated",
		"idx_tasks_scope_space_type_status_id",
		"idx_tasks_status_next_run_priority_created",
		"idx_tasks_idempotency_active",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("任务索引不存在: %s", indexName)
		}
	}
	if count := countTableWhere(db, "tasks", "status IN ('completed', 'error')"); count != 0 {
		return Validation{}, fmt.Errorf("tasks 存在旧状态记录: %d", count)
	}
	if count := countTableWhere(db, "tasks", "scope = 'space' AND (space_id IS NULL OR space_id = '')"); count != 0 {
		return Validation{}, fmt.Errorf("space 任务缺少 space_id: %d", count)
	}
	return Validation{Summary: "通用任务队列中心已就绪"}, nil
}

func estimateLibraryKinds(_ context.Context, db *gorm.DB) (StepPlan, error) {
	var count int64
	if tableExists(db, "library_paths") {
		_ = db.Table("library_paths").Count(&count).Error
	}
	return StepPlan{EstimatedRows: count}, nil
}

func migrateLibraryKinds(_ context.Context, tx *gorm.DB) error {
	if err := addColumnIfMissing(tx, "library_paths", "library_kind", "TEXT NOT NULL DEFAULT 'mixed'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "library_paths", "library_profile_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE library_paths
		SET library_kind = 'mixed'
		WHERE library_kind IS NULL
			OR library_kind = ''
			OR library_kind NOT IN ('movie', 'series', 'home_video', 'mixed')
	`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE library_paths
		SET library_profile_json = '{}'
		WHERE library_profile_json IS NULL OR library_profile_json = ''
	`).Error; err != nil {
		return err
	}
	return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_library_paths_space_kind_id ON library_paths(space_id, library_kind, id);`).Error
}

func validateLibraryKinds(_ context.Context, db *gorm.DB) (Validation, error) {
	for _, column := range []string{"library_kind", "library_profile_json"} {
		if !columnExists(db, "library_paths", column) {
			return Validation{}, fmt.Errorf("library_paths 缺少 %s", column)
		}
	}
	if count := countTableWhere(
		db,
		"library_paths",
		"library_kind IS NULL OR library_kind = '' OR library_kind NOT IN ('movie', 'series', 'home_video', 'mixed')",
	); count != 0 {
		return Validation{}, fmt.Errorf("library_paths 存在非法 library_kind")
	}
	if !indexExists(db, "idx_library_paths_space_kind_id") {
		return Validation{}, fmt.Errorf("媒体库分型索引不存在: idx_library_paths_space_kind_id")
	}
	return Validation{Summary: "媒体库内容分型已就绪"}, nil
}

func estimateMediaTypeRules(_ context.Context, db *gorm.DB) (StepPlan, error) {
	var count int64
	if tableExists(db, "media_extensions") {
		_ = db.Table("media_extensions").Count(&count).Error
	}
	return StepPlan{EstimatedRows: count}, nil
}

func migrateMediaTypeRules(_ context.Context, tx *gorm.DB) error {
	for _, stmt := range mediaTypeRuleSchemaStatements() {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	if !tableExists(tx, "media_extensions") {
		return nil
	}
	return tx.Exec(`
		INSERT OR IGNORE INTO media_type_rules(
			space_id, library_id, type, extension, label, description, enabled, builtin,
			capabilities_json, created_at, updated_at
		)
		SELECT
			COALESCE(NULLIF(library_paths.space_id, ''), ?),
			media_extensions.library_id,
			media_extensions.type,
			ltrim(lower(trim(media_extensions.extension)), '.'),
			'',
			'',
			1,
			CASE WHEN media_extensions.is_built_in = 1 THEN 1 ELSE 0 END,
			CASE media_extensions.type
				WHEN 'image' THEN '["scan","thumbnail","metadata"]'
				WHEN 'video' THEN '["scan","transcode","thumbnail","metadata"]'
				ELSE '[]'
			END,
			media_extensions.created_at,
			datetime('now')
		FROM media_extensions
		LEFT JOIN library_paths ON library_paths.id = media_extensions.library_id
		WHERE trim(media_extensions.extension) != ''
	`, DefaultSpaceID).Error
}

func validateMediaTypeRules(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "media_type_rules") {
		return Validation{}, fmt.Errorf("media_type_rules 表不存在")
	}
	for _, indexName := range []string{
		"idx_media_type_rules_space_global_type_ext",
		"idx_media_type_rules_space_library_type_ext",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("媒体类型规则索引不存在: %s", indexName)
		}
	}
	return Validation{Summary: "媒体类型规则已就绪"}, nil
}

func estimateMediaFileState(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateMediaFileState(_ context.Context, tx *gorm.DB) error {
	if err := addColumnIfMissing(tx, "media_files", "file_state", "TEXT NOT NULL DEFAULT 'available'"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "scan_tasks", "payload_json", "TEXT"); err != nil {
		return err
	}
	return tx.Exec("CREATE INDEX IF NOT EXISTS idx_media_files_file_state ON media_files(file_state)").Error
}

func validateMediaFileState(_ context.Context, db *gorm.DB) (Validation, error) {
	if !columnExists(db, "media_files", "file_state") {
		return Validation{}, fmt.Errorf("media_files 缺少 file_state")
	}
	if !indexExists(db, "idx_media_files_file_state") {
		return Validation{}, fmt.Errorf("media_files.file_state 索引不存在")
	}
	if !columnExists(db, "scan_tasks", "payload_json") {
		return Validation{}, fmt.Errorf("scan_tasks 缺少 payload_json")
	}
	return Validation{Summary: "媒体文件缺失状态字段与扫描任务载荷已就绪"}, nil
}

func estimateFileHashDedup(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateFileHashDedup(_ context.Context, tx *gorm.DB) error {
	columns := map[string]string{
		"content_hash":             "TEXT DEFAULT ''",
		"content_hash_algo":        "TEXT DEFAULT ''",
		"content_hash_computed_at": "DATETIME",
		"content_hash_stale":       "INTEGER NOT NULL DEFAULT 1",
	}
	for column, definition := range columns {
		if err := addColumnIfMissing(tx, "media_files", column, definition); err != nil {
			return fmt.Errorf("迁移 media_files.%s 失败: %w", column, err)
		}
	}
	if err := tx.AutoMigrate(&models.MediaHashGroup{}); err != nil {
		return fmt.Errorf("迁移 media_hash_groups 失败: %w", err)
	}
	statements := []string{
		`UPDATE media_files SET content_hash_stale = 1 WHERE content_hash_stale IS NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_size_content_hash ON media_files(space_id, file_size, content_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_media_files_space_content_hash_stale ON media_files(space_id, content_hash, content_hash_stale);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_hash_groups_space_size_hash ON media_hash_groups(space_id, file_size, content_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_media_hash_groups_space_first ON media_hash_groups(space_id, first_media_id);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func estimateCacheAssets(_ context.Context, _ *gorm.DB) (StepPlan, error) {
	return StepPlan{EstimatedRows: 0}, nil
}

func migrateCacheAssets(_ context.Context, tx *gorm.DB) error {
	if err := tx.AutoMigrate(&models.CacheAsset{}); err != nil {
		return err
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_cache_assets_space_kind ON cache_assets(space_id, kind);`,
		`CREATE INDEX IF NOT EXISTS idx_cache_assets_library_kind ON cache_assets(library_id, kind);`,
		`CREATE INDEX IF NOT EXISTS idx_cache_assets_media_kind ON cache_assets(media_id, kind);`,
		`CREATE INDEX IF NOT EXISTS idx_cache_assets_relative_path ON cache_assets(relative_path);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cache_assets_relative_path_unique ON cache_assets(relative_path);`,
	}
	for _, stmt := range statements {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateFileHashDedup(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "media_hash_groups") {
		return Validation{}, fmt.Errorf("media_hash_groups 表不存在")
	}
	for _, column := range []string{"content_hash", "content_hash_algo", "content_hash_computed_at", "content_hash_stale"} {
		if !columnExists(db, "media_files", column) {
			return Validation{}, fmt.Errorf("media_files 缺少 %s", column)
		}
	}
	for _, indexName := range []string{
		"idx_media_files_space_size_content_hash",
		"idx_media_files_space_content_hash_stale",
		"idx_media_hash_groups_space_size_hash",
		"idx_media_hash_groups_space_first",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("内容哈希索引不存在: %s", indexName)
		}
	}
	return Validation{Summary: "媒体文件内容哈希字段与索引已就绪"}, nil
}

func validateCacheAssets(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "cache_assets") {
		return Validation{}, fmt.Errorf("cache_assets 表不存在")
	}
	for _, column := range []string{"space_id", "kind", "asset_level", "relative_path", "size_bytes", "file_count", "rebuildable", "missing_at"} {
		if !columnExists(db, "cache_assets", column) {
			return Validation{}, fmt.Errorf("cache_assets 缺少 %s", column)
		}
	}
	for _, indexName := range []string{
		"idx_cache_assets_space_kind",
		"idx_cache_assets_library_kind",
		"idx_cache_assets_media_kind",
		"idx_cache_assets_relative_path",
		"idx_cache_assets_relative_path_unique",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("缓存资产索引不存在: %s", indexName)
		}
	}
	return Validation{Summary: "缓存资产表与索引已就绪"}, nil
}

func mediaTypeRuleSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS media_type_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			space_id TEXT NOT NULL DEFAULT '` + DefaultSpaceID + `',
			library_id INTEGER,
			type TEXT NOT NULL,
			extension TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			builtin INTEGER NOT NULL DEFAULT 0,
			capabilities_json TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_type_rules_space_global_type_ext
			ON media_type_rules(space_id, type, extension)
			WHERE library_id IS NULL;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_type_rules_space_library_type_ext
			ON media_type_rules(space_id, library_id, type, extension)
			WHERE library_id IS NOT NULL;`,
	}
}

func estimateMediaInferences(_ context.Context, db *gorm.DB) (StepPlan, error) {
	var count int64
	if tableExists(db, "media_files") {
		_ = db.Table("media_files").Count(&count).Error
	}
	return StepPlan{EstimatedRows: count}, nil
}

func migrateMediaInferences(_ context.Context, tx *gorm.DB) error {
	return tx.AutoMigrate(&models.MediaInference{})
}

func validateMediaInferences(_ context.Context, db *gorm.DB) (Validation, error) {
	if !tableExists(db, "media_inferences") {
		return Validation{}, fmt.Errorf("media_inferences 表不存在")
	}
	for _, indexName := range []string{
		"idx_media_inferences_media_id",
		"idx_media_inferences_space_media",
	} {
		if !indexExists(db, indexName) {
			return Validation{}, fmt.Errorf("影视推断索引不存在: %s", indexName)
		}
	}
	return Validation{Summary: "本地离线影视信息推断表已就绪"}, nil
}

func estimateSpaceOwnedCollections(_ context.Context, db *gorm.DB) (StepPlan, error) {
	var total int64
	for _, table := range []string{"albums", "album_items", "shares", "media_health_issues"} {
		if tableExists(db, table) {
			var count int64
			if err := db.Table(table).Count(&count).Error; err != nil {
				return StepPlan{}, err
			}
			total += count
		}
	}
	return StepPlan{EstimatedRows: total}, nil
}

func migrateSpaceOwnedCollections(_ context.Context, tx *gorm.DB) error {
	for _, target := range []struct{ table, column, definition string }{
		{"albums", "space_id", "TEXT NOT NULL DEFAULT '" + DefaultSpaceID + "'"},
		{"album_items", "space_id", "TEXT NOT NULL DEFAULT '" + DefaultSpaceID + "'"},
		{"shares", "space_id", "TEXT NOT NULL DEFAULT '" + DefaultSpaceID + "'"},
		{"media_health_issues", "space_id", "TEXT NOT NULL DEFAULT '" + DefaultSpaceID + "'"},
	} {
		if tableExists(tx, target.table) {
			if err := addColumnIfMissing(tx, target.table, target.column, target.definition); err != nil {
				return err
			}
			if err := tx.Table(target.table).Where("space_id IS NULL OR TRIM(space_id) = ''").Update("space_id", DefaultSpaceID).Error; err != nil {
				return err
			}
		}
	}
	return tx.AutoMigrate(&models.Album{}, &models.AlbumItem{}, &models.Share{}, &models.MediaHealthIssue{})
}

func validateSpaceOwnedCollections(_ context.Context, db *gorm.DB) (Validation, error) {
	for _, table := range []string{"albums", "album_items", "shares", "media_health_issues"} {
		if !columnExists(db, table, "space_id") {
			return Validation{}, fmt.Errorf("%s 缺少 space_id", table)
		}
		var count int64
		if err := db.Table(table).Where("space_id IS NULL OR TRIM(space_id) = ''").Count(&count).Error; err != nil {
			return Validation{}, err
		}
		if count != 0 {
			return Validation{}, fmt.Errorf("%s 仍有 %d 条记录缺少 Space 归属", table, count)
		}
	}
	return Validation{Summary: "相册、分享与健康问题已具备非空 Space 归属"}, nil
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
