package auth

import (
	"strings"
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

func TestGenerateTokenWithSession_CarriesSID(t *testing.T) {
	token, err := GenerateTokenWithSession("admin", "sess-abc", "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ParseToken(token, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if claims.SessionID != "sess-abc" {
		t.Fatalf("期望 sid=sess-abc, 得到 %q", claims.SessionID)
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

	// 篡改令牌签名段「首字符」（非末位）：末位 base64 字符含 2 个冗余 bit，
	// 替换为同一 4-bit 组内的字符（如 {A,B,C,D} 互换）会解码出相同签名字节、篡改无效（约 6% 误判）。
	// 改签名段首字符——其 6 bit 全有效，替换为不同字符必然改变签名字节，解析必失败、稳定。
	dot := strings.LastIndexByte(token, '.')
	sigStart := dot + 1
	first := token[sigStart]
	var replacement byte = 'A'
	if first == 'A' {
		replacement = 'B'
	}
	tampered := token[:sigStart] + string(replacement) + token[sigStart+1:]
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
