package auth

import (
	"testing"
	"time"
)

func TestGenerateAndParseToken(t *testing.T) {
	secret := "test-secret-key"
	username := "admin"
	expiresIn := 72 * time.Hour

	token, err := GenerateToken(username, secret, expiresIn)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	if token == "" {
		t.Fatal("令牌不应为空")
	}

	claims, err := ParseToken(token, secret)
	if err != nil {
		t.Fatalf("解析令牌失败: %v", err)
	}
	if claims.Username != username {
		t.Errorf("期望用户名 %q, 得到 %q", username, claims.Username)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("ExpiresAt 不应为 nil")
	}
}

func TestParseToken_InvalidSecret(t *testing.T) {
	token, err := GenerateToken("admin", "secret-a", time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	_, err = ParseToken(token, "secret-b")
	if err == nil {
		t.Fatal("用错误密钥解析应失败")
	}
}

func TestParseToken_Expired(t *testing.T) {
	token, err := GenerateToken("admin", "secret", -time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	_, err = ParseToken(token, "secret")
	if err == nil {
		t.Fatal("过期令牌解析应失败")
	}
}

func TestParseToken_Tampered(t *testing.T) {
	token, err := GenerateToken("admin", "secret", time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	// 篡改令牌
	tampered := token[:len(token)-1] + "X"
	_, err = ParseToken(tampered, "secret")
	if err == nil {
		t.Fatal("篡改令牌解析应失败")
	}
}
