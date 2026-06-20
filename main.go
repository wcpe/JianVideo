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
	"jianvideo/internal/api"
	"jianvideo/internal/db"
	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
	"jianvideo/internal/playback"
	"jianvideo/internal/player"
	"jianvideo/internal/transcoder"
	"jianvideo/internal/watcher"
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

	// ffmpeg 路径注入：JIANVIDEO_FFMPEG_PATH 优先；找不到则尝试 PATH 中的 ffmpeg。
	ffmpegPath := os.Getenv("JIANVIDEO_FFMPEG_PATH")
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	transcoder.SetFFmpegPath(ffmpegPath)
	if transcoder.IsFFmpegAvailable() {
		log.Printf("[INFO] ffmpeg 可用: %s", transcoder.GetFFmpegPath())
	} else {
		log.Printf("[WARN] ffmpeg 不可用（%s），HLS 切片将不会生成", ffmpegPath)
	}

	// 创建 HLS 切片存储目录
	hlsDir := filepath.Join(filepath.Dir(cfg.DBPath), "hls")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		log.Fatalf("创建 HLS 目录失败: %v", err)
	}
	hlsMgr := player.NewHLSManager(hlsDir)

	// 初始化缩略图存储目录（与数据库、HLS 同处数据目录下）
	library.InitThumbnailDir(filepath.Dir(cfg.DBPath))

	// 播放服务：用于在 HLS 不可用时提供 /api/play/:id/stream 降级路径
	pbSvc := playback.NewService()
	defer pbSvc.Stop()

	// 创建 API Handler 并注入 HLS 预切片依赖
	libSvc := library.NewService(gormDB)
	apiHandler := api.NewHandler(libSvc).WithHLSPreSlice(hlsDir, hlsMgr)

	// 启动文件监听（FR-03）：对所有已注册本地目录开启 fsnotify 实时监听，
	// 新增/删除文件 500ms 去抖后自动入库/移除；失败仅记日志，不阻断启动。
	if w, err := watcher.New(libSvc); err != nil {
		log.Printf("[WARN] 文件监听初始化失败: %v", err)
	} else if err := w.Start(); err != nil {
		log.Printf("[WARN] 文件监听启动失败: %v", err)
	} else {
		defer w.Stop()
		log.Printf("[INFO] 文件监听已启动 (fsnotify)")
	}

	r := web.NewRouter(cfg, gormDB, hlsMgr, frontendDist, apiHandler, pbSvc)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.ServerPort)
	log.Printf("JianVideo 启动于 %s", addr)
	if err := r.Run(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
}
