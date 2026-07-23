package models

import (
	"database/sql"
	"time"
)

// AuthSession 登录会话（FR2-062）：JWT sid 与撤销状态的真源。
type AuthSession struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"user_id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	UserAgent  string     `json:"user_agent"`
	IPHash     string     `json:"ip_hash"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	Current    bool       `json:"current,omitempty"` // 仅 API 填充，不入库
}

// sessionTimeLayout SQLite datetime 友好格式，便于与 datetime('now') 比较。
const sessionTimeLayout = "2006-01-02 15:04:05"

// CreateAuthSession 写入新会话行。
func CreateAuthSession(d *sql.DB, id string, userID int64, expiresAt time.Time, userAgent, ipHash string) error {
	_, err := d.Exec(`
		INSERT INTO auth_sessions (id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_hash, revoked_at)
		VALUES (?, ?, datetime('now'), datetime('now'), ?, ?, ?, NULL)`,
		id, userID, expiresAt.UTC().Format(sessionTimeLayout), userAgent, ipHash,
	)
	return err
}

// FindAuthSessionByID 按会话 id 查找；不存在返回 nil。
func FindAuthSessionByID(d *sql.DB, id string) (*AuthSession, error) {
	row := d.QueryRow(`
		SELECT id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_hash, revoked_at
		FROM auth_sessions WHERE id = ?`, id)
	return scanAuthSession(row)
}

// ListAuthSessionsByUserID 列出用户未过期且未撤销的会话，最近活跃优先。
func ListAuthSessionsByUserID(d *sql.DB, userID int64) ([]AuthSession, error) {
	rows, err := d.Query(`
		SELECT id, user_id, created_at, last_seen_at, expires_at, user_agent, ip_hash, revoked_at
		FROM auth_sessions
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > datetime('now')
		ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []AuthSession
	for rows.Next() {
		s, err := scanAuthSession(rows)
		if err != nil {
			return nil, err
		}
		if s != nil {
			out = append(out, *s)
		}
	}
	return out, rows.Err()
}

// RevokeAuthSession 撤销指定会话；仅当归属 userID 且尚未撤销时成功。
// 返回 affected 行数。
func RevokeAuthSession(d *sql.DB, sessionID string, userID int64) (int64, error) {
	res, err := d.Exec(`
		UPDATE auth_sessions SET revoked_at = datetime('now')
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeOtherAuthSessions 撤销用户除 keepSessionID 以外的全部未撤销会话。
func RevokeOtherAuthSessions(d *sql.DB, userID int64, keepSessionID string) (int64, error) {
	res, err := d.Exec(`
		UPDATE auth_sessions SET revoked_at = datetime('now')
		WHERE user_id = ? AND revoked_at IS NULL AND id != ?`, userID, keepSessionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeAllAuthSessions 撤销指定用户全部尚未撤销的会话（含未过期与已过期）。
// 返回受影响行数。管理员强制下线 / 禁用用户时调用。
func RevokeAllAuthSessions(d *sql.DB, userID int64) (int64, error) {
	res, err := d.Exec(`
		UPDATE auth_sessions SET revoked_at = datetime('now')
		WHERE user_id = ? AND revoked_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// TouchAuthSession 更新 last_seen_at（节流由调用方决定）。
func TouchAuthSession(d *sql.DB, sessionID string) error {
	_, err := d.Exec(`UPDATE auth_sessions SET last_seen_at = datetime('now') WHERE id = ? AND revoked_at IS NULL`, sessionID)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAuthSession(row scannable) (*AuthSession, error) {
	var s AuthSession
	var createdAt, lastSeenAt, expiresAt string
	var revoked sql.NullString
	err := row.Scan(&s.ID, &s.UserID, &createdAt, &lastSeenAt, &expiresAt, &s.UserAgent, &s.IPHash, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.CreatedAt = parseSessionTime(createdAt)
	s.LastSeenAt = parseSessionTime(lastSeenAt)
	s.ExpiresAt = parseSessionTime(expiresAt)
	if revoked.Valid && revoked.String != "" {
		t := parseSessionTime(revoked.String)
		s.RevokedAt = &t
	}
	return &s, nil
}

func parseSessionTime(v string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	// SQLite datetime('now') 无时区：按 UTC 解析
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.UTC); err == nil {
		return t
	}
	return time.Time{}
}
