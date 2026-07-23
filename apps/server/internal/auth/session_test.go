package auth

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func setupSessionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE auth_sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL,
			last_seen_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			ip_hash TEXT NOT NULL DEFAULT '',
			revoked_at DATETIME
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users(id, username, password_hash, status, created_at) VALUES (1, 'alice', 'x', 'active', datetime('now'))`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return db
}

func TestCreateSessionAndToken_RoundTrip(t *testing.T) {
	db := setupSessionDB(t)
	svc := NewService(db, "secret")
	token, sid, err := svc.CreateSessionAndToken(1, "alice", "TestAgent/1.0", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSessionAndToken: %v", err)
	}
	if sid == "" || token == "" {
		t.Fatal("sid/token 不应为空")
	}
	claims, err := ParseToken(token, "secret")
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.Username != "alice" || claims.SessionID != sid {
		t.Fatalf("claims 不符: %+v", claims)
	}
	if err := svc.ValidateSession(sid, 1); err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
}

func TestValidateSession_Revoked(t *testing.T) {
	db := setupSessionDB(t)
	svc := NewService(db, "secret")
	_, sid, err := svc.CreateSessionAndToken(1, "alice", "ua", "1.1.1.1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeSession(sid, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateSession(sid, 1); err == nil {
		t.Fatal("已撤销会话应失败")
	}
}

func TestRevokeOtherSessions_KeepsCurrent(t *testing.T) {
	db := setupSessionDB(t)
	svc := NewService(db, "secret")
	_, keep, err := svc.CreateSessionAndToken(1, "alice", "ua-a", "1.1.1.1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := svc.CreateSessionAndToken(1, "alice", "ua-b", "1.1.1.2", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeOtherSessions(1, keep); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateSession(keep, 1); err != nil {
		t.Fatalf("当前会话应保留: %v", err)
	}
	if err := svc.ValidateSession(other, 1); err == nil {
		t.Fatal("其它会话应已撤销")
	}
	list, err := svc.ListSessions(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != keep {
		t.Fatalf("列表应仅剩当前会话: %+v", list)
	}
}

func TestListSessions_ExcludesExpired(t *testing.T) {
	db := setupSessionDB(t)
	// CreateAuthSession 写入 SQLite 友好时间格式；过期 1 小时前
	if err := models.CreateAuthSession(db, "expired-sid", 1, time.Now().Add(-time.Hour), "ua", "hash"); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, "secret")
	list, err := svc.ListSessions(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("过期会话不应列出: %+v", list)
	}
}

func TestValidateSession_EmptySIDCompatible(t *testing.T) {
	db := setupSessionDB(t)
	svc := NewService(db, "secret")
	if err := svc.ValidateSession("", 1); err != nil {
		t.Fatalf("空 sid 应兼容放行: %v", err)
	}
}
