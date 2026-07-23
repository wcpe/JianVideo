// Package auth 提供认证与 JWT 令牌签发、校验能力。
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 自定义 JWT 声明。SessionID 对应 auth_sessions.id（FR2-062）；旧令牌可为空以兼容过渡。
type Claims struct {
	Username  string `json:"username"`
	SessionID string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT 令牌（无会话 id，兼容测试与过渡签发）。
func GenerateToken(username, secret string, expiresIn time.Duration) (string, error) {
	return GenerateTokenWithSession(username, "", secret, expiresIn)
}

// GenerateTokenWithSession 生成带会话 id 的 JWT。
func GenerateTokenWithSession(username, sessionID, secret string, expiresIn time.Duration) (string, error) {
	claims := Claims{
		Username:  username,
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 解析并验证 JWT 令牌
func ParseToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("无效的令牌")
	}

	return claims, nil
}
