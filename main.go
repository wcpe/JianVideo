package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/config"
	"jianvideo/internal/db"
	"jianvideo/internal/db/models"
	"jianvideo/internal/player"
	"jianvideo/internal/web"
)

//go:embed frontend/dist
var frontendDist embed.FS

func main() {
	cfg := config.Load()

	// 使用 gorm 打开数据库（同时兼容 db 包的 InitSchema）
	gormDB, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	// 使用底层 sql.DB 进行 schema 初始化
	sqlDB, _ := gormDB.DB()
	if err := db.InitSchema(sqlDB); err != nil {
		log.Fatalf("数据库建表失败: %v", err)
	}
	if err := gormDB.AutoMigrate(&models.MediaExtension{}); err != nil {
		log.Fatalf("媒体后缀表迁移失败: %v", err)
	}

	// 创建 HLS 切片存储目录
	hlsDir := filepath.Join(filepath.Dir(cfg.DBPath), "hls")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		log.Fatalf("创建 HLS 目录失败: %v", err)
	}
	hlsMgr := player.NewHLSManager(hlsDir)

	r := web.NewRouter(cfg, gormDB, hlsMgr, frontendDist)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.ServerPort)
	log.Printf("JianVideo 启动于 %s", addr)
	if err := r.Run(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
}
