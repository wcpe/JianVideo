package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envValue string
		setEnv   bool
		def      string
		want     string
	}{
		{
			name:     "key 存在且非空，返回环境变量值",
			key:      "JIANVIDEO_TEST_KEY",
			envValue: "hello",
			setEnv:   true,
			def:      "default",
			want:     "hello",
		},
		{
			name:     "key 存在但为空字符串，返回默认值",
			key:      "JIANVIDEO_TEST_KEY",
			envValue: "",
			setEnv:   true,
			def:      "default",
			want:     "default",
		},
		{
			name:   "key 不存在，返回默认值",
			key:    "JIANVIDEO_TEST_KEY",
			setEnv: false,
			def:    "default",
			want:   "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.key, tt.envValue)
			} else {
				// 清除测试 key 以验证缺省路径，key 不存在时 Unsetenv 也返回 nil，忽略错误
				_ = os.Unsetenv(tt.key)
			}
			got := getEnv(tt.key, tt.def)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envValue string
		setEnv   bool
		def      int
		want     int
	}{
		{
			name:     "key 存在且为合法整数，返回该整数",
			key:      "JIANVIDEO_TEST_INT",
			envValue: "9090",
			setEnv:   true,
			def:      8080,
			want:     9090,
		},
		{
			name:     "key 存在但为空字符串，返回默认值",
			key:      "JIANVIDEO_TEST_INT",
			envValue: "",
			setEnv:   true,
			def:      8080,
			want:     8080,
		},
		{
			name:     "key 存在但为非数字字符串，返回默认值",
			key:      "JIANVIDEO_TEST_INT",
			envValue: "not-a-number",
			setEnv:   true,
			def:      8080,
			want:     8080,
		},
		{
			name:   "key 不存在，返回默认值",
			key:    "JIANVIDEO_TEST_INT",
			setEnv: false,
			def:    8080,
			want:   8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.key, tt.envValue)
			} else {
				// 清除测试 key 以验证缺省路径，key 不存在时 Unsetenv 也返回 nil，忽略错误
				_ = os.Unsetenv(tt.key)
			}
			got := getEnvInt(tt.key, tt.def)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoad_Defaults(t *testing.T) {
	// 清除所有 JIANVIDEO_* 环境变量
	envKeys := []string{
		"JIANVIDEO_SERVER_PORT",
		"JIANVIDEO_FFMPEG_PATH",
		"JIANVIDEO_FFPROBE_PATH",
		"JIANVIDEO_DB_PATH",
	}
	for _, key := range envKeys {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("清除环境变量 %s 失败: %v", key, err)
		}
	}

	cfg := Load()

	assert.Equal(t, 8080, cfg.ServerPort)
	assert.Equal(t, "ffmpeg", cfg.FFmpegPath)
	assert.Equal(t, "ffprobe", cfg.FFprobePath)
	assert.Equal(t, filepath.Join("data", "jianvideo.db"), cfg.DBPath)
}

func TestLoad_FromEnv(t *testing.T) {
	t.Setenv("JIANVIDEO_SERVER_PORT", "3000")
	t.Setenv("JIANVIDEO_FFMPEG_PATH", "/usr/local/bin/ffmpeg")
	t.Setenv("JIANVIDEO_FFPROBE_PATH", "/usr/local/bin/ffprobe")
	t.Setenv("JIANVIDEO_DB_PATH", "/var/data/jianvideo.db")

	cfg := Load()

	assert.Equal(t, 3000, cfg.ServerPort)
	assert.Equal(t, "/usr/local/bin/ffmpeg", cfg.FFmpegPath)
	assert.Equal(t, "/usr/local/bin/ffprobe", cfg.FFprobePath)
	assert.Equal(t, "/var/data/jianvideo.db", cfg.DBPath)
}

func TestLoad_PartialEnv(t *testing.T) {
	// 先清除所有相关环境变量
	envKeys := []string{
		"JIANVIDEO_SERVER_PORT",
		"JIANVIDEO_FFMPEG_PATH",
		"JIANVIDEO_FFPROBE_PATH",
		"JIANVIDEO_DB_PATH",
	}
	for _, key := range envKeys {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("清除环境变量 %s 失败: %v", key, err)
		}
	}

	// 只设置部分环境变量
	t.Setenv("JIANVIDEO_SERVER_PORT", "9090")
	t.Setenv("JIANVIDEO_DB_PATH", "/custom/data.db")

	cfg := Load()

	// 已设置的取环境值
	assert.Equal(t, 9090, cfg.ServerPort)
	assert.Equal(t, "/custom/data.db", cfg.DBPath)

	// 未设置的取默认值
	assert.Equal(t, "ffmpeg", cfg.FFmpegPath)
	assert.Equal(t, "ffprobe", cfg.FFprobePath)
}
