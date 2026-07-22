package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaultDBPathUnderData(t *testing.T) {
	t.Setenv("DB_PATH", "")
	// 清除可能继承的 DB_PATH
	t.Setenv("DB_PATH", "")
	// Load 读 JWT_SECRET 等；空 secret 会生成随机密钥，可接受
	cfg := Load()
	want := filepath.Join("data", "jianvideo.db")
	if cfg.DBPath != want {
		t.Fatalf("默认 DBPath = %q，希望 %q", cfg.DBPath, want)
	}
}

func TestLoadDBPathEnvOverride(t *testing.T) {
	t.Setenv("DB_PATH", "jianvideo.db")
	cfg := Load()
	if cfg.DBPath != "jianvideo.db" {
		t.Fatalf("DB_PATH 覆盖失败: %q", cfg.DBPath)
	}
}
