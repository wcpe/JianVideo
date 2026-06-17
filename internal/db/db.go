package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
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
	query := `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
`
	if _, err := d.Exec(query); err != nil {
		return fmt.Errorf("创建 users 表失败: %w", err)
	}
	return nil
}
