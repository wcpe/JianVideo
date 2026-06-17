package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"jianvideo/internal/api"
	"jianvideo/internal/config"
	"jianvideo/internal/db"
	"jianvideo/internal/library"
)

func main() {
	cfg := config.Load()

	database, err := db.Init(cfg.DBPath)
	if err != nil {
		log.Fatalf("[ERROR] 数据库初始化失败: %v", err)
	}

	libService := library.NewService(database.DB)
	handler := api.NewHandler(libService)

	r := gin.Default()
	api.RegisterRoutes(r, handler)

	log.Printf("[INFO] 服务启动，端口: %d", cfg.ServerPort)
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("[ERROR] 服务启动失败: %v", err)
	}
}
