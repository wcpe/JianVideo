package main

import (
	"fmt"
	"log"
	"net/http"

	"jianvideo/config"
	"jianvideo/internal/db"
	"jianvideo/internal/web"
)

func main() {
	cfg := config.Load()

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer d.Close()

	if err := db.InitSchema(d); err != nil {
		log.Fatalf("数据库建表失败: %v", err)
	}

	r := web.NewRouter(cfg, d)

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("JianVideo 启动于 %s", addr)
	if err := r.Run(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
}
