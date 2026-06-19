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
		d.Close()
		return nil, fmt.Errorf("启用 WAL 模式失败: %w", err)
	}

	if err := d.Ping(); err != nil {
		d.Close()
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
	}
	for _, q := range queries {
		if _, err := d.Exec(q); err != nil {
			return fmt.Errorf("初始化数据库失败: %w", err)
		}
	}
	return nil
}
