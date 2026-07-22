package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaultDBPathUnderData(t *testing.T) {
	t.Setenv("DB_PATH", "")
	t.Setenv("JWT_SECRET", "test-secret-for-coverage")
	t.Setenv("SERVER_PORT", "")
	cfg := Load()
	want := filepath.Join("data", "jianvideo.db")
	if cfg.DBPath != want {
		t.Fatalf("默认 DBPath = %q，希望 %q", cfg.DBPath, want)
	}
	if cfg.ServerPort != 8080 {
		t.Fatalf("默认 ServerPort = %d，希望 8080", cfg.ServerPort)
	}
	if cfg.JWTSecret != "test-secret-for-coverage" {
		t.Fatalf("JWTSecret 未保留环境变量: %q", cfg.JWTSecret)
	}
	if cfg.JWTExpiresIn <= 0 {
		t.Fatalf("JWTExpiresIn 应大于 0，得到 %v", cfg.JWTExpiresIn)
	}
}

func TestLoadDBPathEnvOverride(t *testing.T) {
	t.Setenv("DB_PATH", "jianvideo.db")
	t.Setenv("JWT_SECRET", "override-secret")
	cfg := Load()
	if cfg.DBPath != "jianvideo.db" {
		t.Fatalf("DB_PATH 覆盖失败: %q", cfg.DBPath)
	}
}

func TestLoadServerPortEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", "port-secret")
	t.Setenv("SERVER_PORT", "9090")
	cfg := Load()
	if cfg.ServerPort != 9090 {
		t.Fatalf("SERVER_PORT 覆盖失败: %d", cfg.ServerPort)
	}
}

func TestLoadServerPortInvalidFallsBack(t *testing.T) {
	t.Setenv("JWT_SECRET", "port-secret")
	t.Setenv("SERVER_PORT", "not-a-number")
	cfg := Load()
	if cfg.ServerPort != 8080 {
		t.Fatalf("无效 SERVER_PORT 应回退默认 8080，得到 %d", cfg.ServerPort)
	}
}

func TestEnvStringAndEnvInt(t *testing.T) {
	t.Setenv("JV_TEST_STR", "hello")
	if got := envString("JV_TEST_STR", "def"); got != "hello" {
		t.Fatalf("envString 有值: %q", got)
	}
	if got := envString("JV_TEST_STR_MISSING", "def"); got != "def" {
		t.Fatalf("envString 缺省: %q", got)
	}

	t.Setenv("JV_TEST_INT", "42")
	if got := envInt("JV_TEST_INT", 7); got != 42 {
		t.Fatalf("envInt 有值: %d", got)
	}
	if got := envInt("JV_TEST_INT_MISSING", 7); got != 7 {
		t.Fatalf("envInt 缺省: %d", got)
	}
	t.Setenv("JV_TEST_INT_ZERO", "0")
	if got := envInt("JV_TEST_INT_ZERO", 7); got != 0 {
		t.Fatalf("envInt 允许 0: %d", got)
	}
}

func TestAtoi(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"0", 0},
		{"123", 123},
		{"", 0},
		{"12a", 0},
		{"-1", 0},
	}
	for _, tc := range cases {
		if got := atoi(tc.in); got != tc.want {
			t.Fatalf("atoi(%q)=%d, want %d", tc.in, got, tc.want)
		}
	}
}
