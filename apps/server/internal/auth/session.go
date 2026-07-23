package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// lastSeenTouchMinInterval 节流更新 last_seen_at，避免热路径写库。
const lastSeenTouchMinInterval = 5 * time.Minute

// CreateSessionAndToken 创建会话行并签发含 sid 的 JWT。
func (s *Service) CreateSessionAndToken(userID int64, username, userAgent, clientIP string, expiresIn time.Duration) (token string, sessionID string, err error) {
	sessionID, err = newSessionID()
	if err != nil {
		return "", "", err
	}
	expiresAt := time.Now().Add(expiresIn)
	if err := models.CreateAuthSession(s.db, sessionID, userID, expiresAt, truncateUA(userAgent), HashIP(clientIP)); err != nil {
		return "", "", fmt.Errorf("创建会话失败: %w", err)
	}
	token, err = GenerateTokenWithSession(username, sessionID, s.jwtSecret, expiresIn)
	if err != nil {
		return "", "", err
	}
	return token, sessionID, nil
}

// ValidateSession 校验 sid 对应会话仍有效（存在、未撤销、未过期、归属 userID）。
// 成功时可选节流更新 last_seen。
func (s *Service) ValidateSession(sessionID string, userID int64) error {
	if sessionID == "" {
		// 兼容无 sid 的旧 JWT：过渡期放行（迁移后新登录均带 sid）。
		return nil
	}
	sess, err := models.FindAuthSessionByID(s.db, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("会话不存在")
	}
	if sess.UserID != userID {
		return fmt.Errorf("会话不属于当前用户")
	}
	if sess.RevokedAt != nil {
		return fmt.Errorf("会话已撤销")
	}
	if !sess.ExpiresAt.IsZero() && time.Now().After(sess.ExpiresAt) {
		return fmt.Errorf("会话已过期")
	}
	// 节流 touch
	if time.Since(sess.LastSeenAt) >= lastSeenTouchMinInterval {
		_ = models.TouchAuthSession(s.db, sessionID)
	}
	return nil
}

// ListSessions 列出当前用户有效会话。
func (s *Service) ListSessions(userID int64) ([]models.AuthSession, error) {
	return models.ListAuthSessionsByUserID(s.db, userID)
}

// RevokeSession 撤销指定会话（须归属 userID）。
func (s *Service) RevokeSession(sessionID string, userID int64) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("会话 id 不能为空")
	}
	n, err := models.RevokeAuthSession(s.db, sessionID, userID)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("会话不存在或已撤销")
	}
	return nil
}

// RevokeOtherSessions 改密后撤销除当前会话外的其它会话。
func (s *Service) RevokeOtherSessions(userID int64, keepSessionID string) error {
	_, err := models.RevokeOtherAuthSessions(s.db, userID, keepSessionID)
	return err
}

// SessionTableReady 检测 auth_sessions 表是否存在（测试/半迁移环境可跳过校验）。
func (s *Service) SessionTableReady() bool {
	if s == nil || s.db == nil {
		return false
	}
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='auth_sessions'`).Scan(&name)
	return err == nil && name == "auth_sessions"
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func truncateUA(ua string) string {
	ua = strings.TrimSpace(ua)
	const max = 512
	if len(ua) > max {
		return ua[:max]
	}
	return ua
}
