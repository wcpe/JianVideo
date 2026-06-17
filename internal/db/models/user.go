package models

import (
	"database/sql"
	"time"
)

// User 用户模型
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // 永远不序列化到 JSON
	CreatedAt    time.Time `json:"created_at"`
}

// CreateUser 创建用户
func CreateUser(d *sql.DB, username, passwordHash string) (*User, error) {
	result, err := d.Exec(
		"INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, datetime('now'))",
		username, passwordHash,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	return &User{
		ID:           int(id),
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}, nil
}

// FindUserByUsername 按用户名查找用户
func FindUserByUsername(d *sql.DB, username string) (*User, error) {
	row := d.QueryRow(
		"SELECT id, username, password_hash, created_at FROM users WHERE username = ?",
		username,
	)

	var u User
	var createdAt string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

// UserExists 检查是否已有用户存在
func UserExists(d *sql.DB) (bool, error) {
	var count int
	err := d.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
