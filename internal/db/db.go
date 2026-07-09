// Package db 提供 SQLite 数据库的打开与表结构初始化等纯数据读写能力。
package db

import (
	"database/sql"
	"fmt"
)

// Open 打开 SQLite 数据库并启用 WAL 模式
func Open(dataSourceName string) (*sql.DB, error) {
	d, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	if _, err := d.Exec("PRAGMA journal_mode=WAL"); err != nil {
		// 错误清理路径，关闭出错无从恢复，忽略
		_ = d.Close()
		return nil, fmt.Errorf("启用 WAL 模式失败: %w", err)
	}

	if err := d.Ping(); err != nil {
		// 错误清理路径，关闭出错无从恢复，忽略
		_ = d.Close()
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	return d, nil
}

// InitSchema 初始化数据库表结构
func InitSchema(d *sql.DB) error {
	queries := []string{
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
    file_state TEXT NOT NULL DEFAULT 'available',
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
    space_id TEXT NOT NULL DEFAULT 'space-default',
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_type_rules_space_global_type_ext ON media_type_rules(space_id, type, extension) WHERE library_id IS NULL;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_type_rules_space_library_type_ext ON media_type_rules(space_id, library_id, type, extension) WHERE library_id IS NOT NULL;`,
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
    before_json TEXT,
    after_json TEXT,
    metadata_json TEXT,
    request_id TEXT,
    created_at DATETIME NOT NULL
);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_scope_space_created ON audit_events(scope, space_id, created_at, id);`,
		`CREATE TABLE IF NOT EXISTS media_inferences (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id INTEGER NOT NULL,
    space_id TEXT NOT NULL DEFAULT 'space-default',
    kind TEXT NOT NULL DEFAULT 'mixed',
    title TEXT,
    year INTEGER DEFAULT 0,
    season INTEGER DEFAULT 0,
    episode INTEGER DEFAULT 0,
    episode_title TEXT,
    confidence REAL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'offline_rule',
    rule_version TEXT NOT NULL DEFAULT 'fr2-031-v1',
    manual INTEGER DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME
);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_inferences_media_id ON media_inferences(media_id);`,
		`CREATE INDEX IF NOT EXISTS idx_media_inferences_space_media ON media_inferences(space_id, media_id);`,
	}
	for _, q := range queries {
		if _, err := d.Exec(q); err != nil {
			return fmt.Errorf("初始化数据库失败: %w", err)
		}
	}
	return nil
}
