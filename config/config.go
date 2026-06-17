package config

import (
	"os"
	"time"
)

// Config 应用配置
type Config struct {
	ServerPort   int           `yaml:"server_port"`
	JWTSecret    string        `yaml:"jwt_secret"`
	JWTExpiresIn time.Duration `yaml:"jwt_expires_in"`
	DBPath       string        `yaml:"db_path"`
}

// Load 加载配置（环境变量优先，硬编码默认值兜底）
func Load() *Config {
	return &Config{
		ServerPort:   envInt("SERVER_PORT", 8080),
		JWTSecret:    envString("JWT_SECRET", "jianvideo-default-secret-change-me"),
		JWTExpiresIn: 72 * time.Hour,
		DBPath:       envString("DB_PATH", "jianvideo.db"),
	}
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		return int(atoi(v))
	}
	return def
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
