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

	// 篡改令牌：必须保证替换后的字符与原末位字符不同。
	// 不能简单拼接固定字符（如 "X"），因为 JWT 签名段末位随 IssuedAt 变化，
	// 约 1/64 概率原末位恰为该字符，拼接后令牌不变，解析会成功而导致误判。
	last := token[len(token)-1]
	var replacement byte = 'A'
	if last == 'A' {
		replacement = 'B'
	}
	tampered := token[:len(token)-1] + string(replacement)
	_, err = ParseToken(tampered, "secret")
	if err == nil {
		t.Fatal("篡改令牌解析应失败")
	}
}

func TestParseToken_EmptyToken(t *testing.T) {
	_, err := ParseToken("", "secret")
	if err == nil {
		t.Fatal("空令牌解析应失败")
	}
}

func TestParseToken_InvalidFormat(t *testing.T) {
	_, err := ParseToken("not.a.valid.jwt.token", "secret")
	if err == nil {
		t.Fatal("格式错误的令牌解析应失败")
	}
}

func TestParseToken_EmptySecret(t *testing.T) {
	token, err := GenerateToken("admin", "secret", time.Hour)
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}

	// 用空密钥解析（与生成密钥不同，应失败）
	_, err = ParseToken(token, "")
	if err == nil {
		t.Fatal("用空密钥解析应失败")
	}
}

func TestGenerateToken_EmptyUsername(t *testing.T) {
	token, err := GenerateToken("", "secret", time.Hour)
	if err != nil {
		t.Fatalf("空用户名生成令牌应成功: %v", err)
	}
	if token == "" {
		t.Fatal("令牌不应为空")
	}

	claims, err := ParseToken(token, "secret")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.Username != "" {
		t.Fatal("用户名为空")
	}
}
