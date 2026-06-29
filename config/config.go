// Package config 提供服务端运行配置的加载与环境变量解析（含默认值与凭据生成）。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
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

// Load 加载配置（环境变量优先，未设置 JWT_SECRET 时生成随机密钥）
func Load() *Config {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("[ERROR] 生成随机 JWT 密钥失败: %v", err)
		}
		secret = hex.EncodeToString(b)
		log.Printf("[WARN] JWT_SECRET 未设置，已生成随机密钥（重启后需重新登录），建议设置 JWT_SECRET 环境变量以持久化会话")
	}
	return &Config{
		ServerPort:   envInt("SERVER_PORT", 8080),
		JWTSecret:    secret,
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
		n := atoi(v)
		if n == 0 && v != "0" {
			log.Printf("[WARN] 无效的 %s 值: %q，使用默认值 %d", key, v, def)
			return def
		}
		return n
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
