package main

import (
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/config"
	"github.com/wcpe/JianVideo/internal/api"
	"github.com/wcpe/JianVideo/internal/db"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/share"
	"github.com/wcpe/JianVideo/internal/transcoder"
	"github.com/wcpe/JianVideo/internal/watcher"
	"github.com/wcpe/JianVideo/internal/web"
)

//go:embed frontend/dist
var frontendDist embed.FS

// version 应用版本号，构建时经 -ldflags "-X main.version=..." 注入，默认 dev。
var version = "dev"

// resolveTool 按「环境变量 → 可执行文件同目录捆绑版 → PATH 名」解析外部工具路径。
// 用于让随包附带的 ffmpeg/ffprobe（与主二进制同目录）开箱即用。
func resolveTool(envVar, name string) string {
	if p := os.Getenv(envVar); p != "" {
		return p
	}
	exeName := name
	if runtime.GOOS == "windows" {
		exeName = name + ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(exe), exeName)
		if _, err := os.Stat(bundled); err == nil {
			return bundled
		}
	}
	return name
}

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
	// 统一迁移：媒体后缀、媒体文件新增列（软删/显示名/EXIF/收藏/观看状态），及相册/标签/设置新表
	if err := gormDB.AutoMigrate(
		&models.MediaExtension{},
		&models.MediaFile{},
		&models.Album{},
		&models.AlbumItem{},
		&models.Tag{},
		&models.TagMapping{},
		&models.Setting{},
		&models.ScanTask{},
		&models.Share{},
	); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// ffmpeg/ffprobe 路径注入：环境变量 → 同目录捆绑版 → PATH（见 ADR-0027）。
	transcoder.SetFFmpegPath(resolveTool("JIANVIDEO_FFMPEG_PATH", "ffmpeg"))
	ffprobeBin := resolveTool("JIANVIDEO_FFPROBE_PATH", "ffprobe")
	transcoder.SetFFprobePath(ffprobeBin)
	// 媒体时间提取（FR-31）：library 包独立调用 ffprobe 读视频 creation_time，
	// 与 transcoder 共用同一解析结果，避免跨包依赖。
	library.SetFFprobePath(ffprobeBin)
	if transcoder.IsFFmpegAvailable() {
		log.Printf("[INFO] ffmpeg 可用: %s", transcoder.GetFFmpegPath())
	} else {
		log.Printf("[WARN] ffmpeg 不可用（%s），HLS 切片将不会生成", transcoder.GetFFmpegPath())
	}

	// 创建 HLS 切片存储目录
	hlsDir := filepath.Join(filepath.Dir(cfg.DBPath), "hls")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		log.Fatalf("创建 HLS 目录失败: %v", err)
	}
	hlsMgr := player.NewHLSManager(hlsDir)

	// 初始化缩略图存储目录（与数据库、HLS 同处数据目录下）
	library.InitThumbnailDir(filepath.Dir(cfg.DBPath))

	// magick 路径注入与 HEIC/RAW 转换缓存目录初始化（FR-37，见 ADR）：
	// 解析顺序与 ffmpeg 一致：环境变量 → 同目录捆绑版 → PATH。
	library.SetMagickPath(resolveTool("JIANVIDEO_MAGICK_PATH", "magick"))
	library.InitConvertCacheDir(filepath.Dir(cfg.DBPath))
	if library.IsMagickAvailable() {
		log.Printf("[INFO] ImageMagick 可用: %s（HEIC/RAW 将转 JPEG 显示）", library.GetMagickPath())
	} else {
		log.Printf("[WARN] ImageMagick 不可用（%s），HEIC/RAW 图片将无法显示", library.GetMagickPath())
	}

	// 播放服务：用于在 HLS 不可用时提供 /api/play/:id/stream 降级路径
	pbSvc := playback.NewService()
	defer pbSvc.Stop()

	// 创建 API Handler 并注入 HLS 预切片依赖、运行期设置服务（FR-24）
	libSvc := library.NewService(gormDB)
	settingsSvc := settings.NewService(gormDB)
	shareSvc := share.NewService(gormDB)

	// 扫描任务队列（FR-29）：单 worker 串行执行入队扫描，重启先恢复残留 running 再启动。
	scanQueue := library.NewTaskQueue(gormDB, libSvc.ScanLibraryWithType)
	if err := scanQueue.RecoverRunning(); err != nil {
		log.Printf("[WARN] 扫描队列重启恢复失败: %v", err)
	}
	scanQueue.Start()
	defer scanQueue.Stop()

	// 定时扫描调度器（FR-28）：按设置中的可配置周期，周期性对所有启用库入队增量扫描。
	// 周期来自 settings.scan_interval（<=0 关闭）；保存设置后经 Reload 即时生效。
	scanScheduler := library.NewScanScheduler(
		settingsSvc.ScanInterval,
		func() {
			libs, err := libSvc.ListLibraryPaths()
			if err != nil {
				log.Printf("[WARN] 定时扫描枚举媒体库失败: %v", err)
				return
			}
			if n := scanQueue.EnqueueScheduled(libs, models.ScanTypeIncremental); n > 0 {
				log.Printf("[INFO] 定时扫描已入队 %d 个媒体库", n)
			}
		},
	)
	scanScheduler.Start()
	defer scanScheduler.Stop()

	apiHandler := api.NewHandler(libSvc).WithHLSPreSlice(hlsDir, hlsMgr).WithVersion(version).WithSettings(settingsSvc).WithScanQueue(scanQueue).WithSettingsReload(scanScheduler.Reload).WithShareService(shareSvc)

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
	// 自更新重启时新进程可能早于旧进程释放端口启动，故监听带短重试等待端口释放（FR-46）。
	ln, err := listenWithRetry(addr, 10*time.Second)
	if err != nil {
		log.Fatalf("监听端口失败: %v", err)
	}
	if err := http.Serve(ln, r); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// listenWithRetry 在 addr 上监听 TCP；遇端口被占用（自更新重启时旧实例尚未退出）时
// 每 500ms 重试一次，直至成功或超时。端口空闲时立即返回，无额外开销。
func listenWithRetry(addr string, timeout time.Duration) (net.Listener, error) {
	start := time.Now()
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		if time.Since(start) >= timeout {
			return nil, err
		}
		log.Printf("[WARN] 端口 %s 暂被占用，等待旧实例释放后重试…", addr)
		time.Sleep(500 * time.Millisecond)
	}
}
