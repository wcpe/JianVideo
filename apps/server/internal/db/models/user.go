package models

import (
	"database/sql"
	"time"
)

// 用户状态（FR2-010）。
const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

// User 用户模型
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // 永远不序列化到 JSON
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateUser 创建用户（默认 active）。
func CreateUser(d *sql.DB, username, passwordHash string) (*User, error) {
	result, err := d.Exec(
		"INSERT INTO users (username, password_hash, status, created_at) VALUES (?, ?, ?, datetime('now'))",
		username, passwordHash, UserStatusActive,
	)
	if err != nil {
		// 兼容尚未迁移 status 列的库（测试/半迁移）。
		result, err = d.Exec(
			"INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, datetime('now'))",
			username, passwordHash,
		)
		if err != nil {
			return nil, err
		}
	}

	id, _ := result.LastInsertId()

	return &User{
		ID:           int(id),
		Username:     username,
		PasswordHash: passwordHash,
		Status:       UserStatusActive,
		CreatedAt:    time.Now(),
	}, nil
}

// FindUserByUsername 按用户名查找用户
func FindUserByUsername(d *sql.DB, username string) (*User, error) {
	row := d.QueryRow(
		"SELECT id, username, password_hash, COALESCE(status, 'active'), created_at FROM users WHERE username = ?",
		username,
	)

	var u User
	var createdAt string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Status, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		// 兼容无 status 列。
		row = d.QueryRow(
			"SELECT id, username, password_hash, created_at FROM users WHERE username = ?",
			username,
		)
		err = row.Scan(&u.ID, &u.Username, &u.PasswordHash, &createdAt)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		u.Status = UserStatusActive
	}
	if u.Status == "" {
		u.Status = UserStatusActive
	}

	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

// ListUsers 列出全部用户（不含密码哈希用途时由调用方忽略 PasswordHash）。
func ListUsers(d *sql.DB) ([]User, error) {
	rows, err := d.Query(`SELECT id, username, password_hash, COALESCE(status, 'active'), created_at FROM users ORDER BY id ASC`)
	if err != nil {
		rows, err = d.Query(`SELECT id, username, password_hash, created_at FROM users ORDER BY id ASC`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []User
		for rows.Next() {
			var u User
			var createdAt string
			if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &createdAt); err != nil {
				return nil, err
			}
			u.Status = UserStatusActive
			u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
			out = append(out, u)
		}
		return out, rows.Err()
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var createdAt string
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Status, &createdAt); err != nil {
			return nil, err
		}
		if u.Status == "" {
			u.Status = UserStatusActive
		}
		u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserStatus 设置用户状态。
func SetUserStatus(d *sql.DB, userID int, status string) error {
	res, err := d.Exec(`UPDATE users SET status = ? WHERE id = ?`, status, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// FindUserByID 按 id 查找。
func FindUserByID(d *sql.DB, id int) (*User, error) {
	row := d.QueryRow(
		"SELECT id, username, password_hash, COALESCE(status, 'active'), created_at FROM users WHERE id = ?",
		id,
	)
	var u User
	var createdAt string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Status, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		row = d.QueryRow("SELECT id, username, password_hash, created_at FROM users WHERE id = ?", id)
		err = row.Scan(&u.ID, &u.Username, &u.PasswordHash, &createdAt)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		u.Status = UserStatusActive
	}
	if u.Status == "" {
		u.Status = UserStatusActive
	}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

// UpdateUserPassword 更新指定用户的密码哈希；用户不存在返回 sql.ErrNoRows。
func UpdateUserPassword(d *sql.DB, username, passwordHash string) error {
	res, err := d.Exec(
		"UPDATE users SET password_hash = ? WHERE username = ?",
		passwordHash, username,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
