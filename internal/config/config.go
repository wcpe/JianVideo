package config

import (
	"os"
	"path/filepath"
)

// Config 应用配置。
type Config struct {
	ServerPort  int
	FFmpegPath  string
	FFprobePath string
	DBPath      string
}

// Load 从环境变量和默认值加载配置。
func Load() *Config {
	return &Config{
		ServerPort:  getEnvInt("JIANVIDEO_SERVER_PORT", 8080),
		FFmpegPath:  getEnv("JIANVIDEO_FFMPEG_PATH", "ffmpeg"),
		FFprobePath: getEnv("JIANVIDEO_FFPROBE_PATH", "ffprobe"),
		DBPath:      getEnv("JIANVIDEO_DB_PATH", filepath.Join("data", "jianvideo.db")),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		// 简化：忽略解析错误，用默认值
		return def
	}
	return def
}
