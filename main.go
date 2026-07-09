// Package main 是 JianVideo 服务端程序入口，负责初始化各模块并启动 HTTP 服务。
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/config"
	"github.com/wcpe/JianVideo/internal/api"
	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/dblog"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/metrics"
	"github.com/wcpe/JianVideo/internal/migration"
	"github.com/wcpe/JianVideo/internal/netproxy"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/share"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
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
	// 记录进程启动时刻，供系统诊断「运行环境」计算运行时长（FR-60）。
	startTime := time.Now()
	cfg := config.Load()

	// 可运行时切换级别的 GORM 日志器（FR-110）：默认安静（不刷 record-not-found/普通 SQL），
	// 由「调试日志」开关在运行期切到 Info 级；启动时下方读取设置决定初始级别。
	dbLogger := dblog.NewDefault()

	// 使用 gorm 打开数据库（同时兼容 db 包的 InitSchema）
	gormDB, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{Logger: dbLogger})
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	registry, err := migration.NewRegistry(migration.DefaultMigrations()...)
	if err != nil {
		log.Fatalf("数据库迁移注册失败: %v", err)
	}
	migrationResult, err := migration.NewRunner(gormDB, migration.RunnerOptions{
		DBPath:    cfg.DBPath,
		BackupDir: filepath.Join(filepath.Dir(cfg.DBPath), "backups"),
		Registry:  registry,
	}).Run(context.Background())
	if err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	if migrationResult.Backup.Path != "" {
		log.Printf("[INFO] 数据库迁移备份已创建: path=%s, size=%d", migrationResult.Backup.Path, migrationResult.Backup.Size)
	}

	// 设置服务（FR-24）：先建好，供下方 ffmpeg 路径持久化设置覆盖与 API 注入复用。
	auditSvc := audit.NewRecorder(gormDB)
	settingsSvc := settings.NewService(gormDB).WithAudit(auditSvc)

	// 调试日志初始级别（FR-110）：读取持久化的「调试日志」开关，决定本次启动初始安静/详细。
	if settingsSvc.DebugLog() {
		dbLogger.SetEnabled(true)
		log.Printf("[INFO] 调试日志已开启（GORM 详细日志）")
	}

	// ffmpeg/ffprobe 路径注入：环境变量 → 同目录捆绑版 → PATH（见 ADR-0027）。
	transcoder.SetFFmpegPath(resolveTool("JIANVIDEO_FFMPEG_PATH", "ffmpeg"))
	ffprobeBin := resolveTool("JIANVIDEO_FFPROBE_PATH", "ffprobe")
	transcoder.SetFFprobePath(ffprobeBin)
	// 持久化设置优先于自动发现（FR-56）：settings 中 ffmpeg_path/ffprobe_path 非空则覆盖。
	if p, _ := settingsSvc.Get(settings.KeyFFmpegPath); p != "" {
		transcoder.SetFFmpegPath(p)
		log.Printf("[INFO] 采用持久化设置的 ffmpeg 路径: %s", p)
	}
	if p, _ := settingsSvc.Get(settings.KeyFFprobePath); p != "" {
		transcoder.SetFFprobePath(p)
		ffprobeBin = p
		log.Printf("[INFO] 采用持久化设置的 ffprobe 路径: %s", p)
	}
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
	if err := os.MkdirAll(hlsDir, 0o750); err != nil {
		log.Fatalf("创建 HLS 目录失败: %v", err)
	}
	hlsMgr := player.NewHLSManager(hlsDir)

	// 初始化缩略图存储目录（与数据库、HLS 同处数据目录下）
	library.InitThumbnailDir(filepath.Dir(cfg.DBPath))

	// magick 路径注入与 HEIC/RAW 转换缓存目录初始化（FR-37，见 ADR）：
	// 解析顺序与 ffmpeg 一致：环境变量 → 同目录捆绑版 → PATH。
	library.SetMagickPath(resolveTool("JIANVIDEO_MAGICK_PATH", "magick"))
	// 持久化设置优先于自动发现（FR-63）：settings 中 magick_path 非空则覆盖。
	if p, _ := settingsSvc.Get(settings.KeyMagickPath); p != "" {
		library.SetMagickPath(p)
		log.Printf("[INFO] 采用持久化设置的 magick 路径: %s", p)
	}
	library.InitConvertCacheDir(filepath.Dir(cfg.DBPath))
	if library.IsMagickAvailable() {
		log.Printf("[INFO] ImageMagick 可用: %s（HEIC/RAW 将转 JPEG 显示）", library.GetMagickPath())
	} else {
		log.Printf("[WARN] ImageMagick 不可用（%s），HEIC/RAW 图片将无法显示", library.GetMagickPath())
	}

	// 后端出站网络代理注入（FR-80）：settings 中 network_proxy 非空则设为出站代理，空=直连。
	// 非法 URL 仅记 WARN、不阻断启动；自更新等后端外部 HTTP 出站经此代理（解决直连 GitHub 不可达）。
	if p, _ := settingsSvc.Get(settings.KeyNetworkProxy); p != "" {
		if err := netproxy.SetProxy(p); err != nil {
			log.Printf("[WARN] 持久化网络代理设置无效，忽略并走直连: %v", err)
		} else {
			log.Printf("[INFO] 采用持久化设置的出站网络代理: %s", netproxy.Raw())
		}
	}

	// 播放服务：用于在 HLS 不可用时提供 /api/play/:id/stream 降级路径
	pbSvc := playback.NewService()
	defer pbSvc.Stop()

	// 系统指标采样器（FR-119，见 ADR-0044）：后台定时采样 CPU/内存/磁盘/转码并发落 SQLite，
	// 数据盘取数据库所在目录、转码并发由播放服务活跃会话数注入；随服务启停，关闭时等待 goroutine 干净退出。
	metricsSampler := metrics.NewSampler(gormDB, filepath.Dir(cfg.DBPath), pbSvc.ActiveSessions)
	metricsSampler.Start(context.Background())
	defer metricsSampler.Stop()

	// 创建 API Handler 并注入 HLS 预切片依赖、运行期设置服务（FR-24）
	// settingsSvc 已在 ffmpeg 路径注入处创建（FR-56），此处复用。
	libSvc := library.NewService(gormDB).WithAudit(auditSvc)
	shareSvc := share.NewService(gormDB)
	taskSvc := tasksvc.NewService(gormDB).WithAudit(auditSvc)
	if err := taskSvc.RecoverRunning(context.Background()); err != nil {
		log.Printf("[WARN] 通用任务队列重启恢复失败: %v", err)
	}

	// 扫描任务队列（FR-29）：单 worker 串行执行入队扫描，重启先恢复残留 running 再启动。
	scanQueue := library.NewTaskQueue(gormDB, libSvc.ScanLibraryWithType).WithChangeExec(libSvc.ApplyScanChange).WithAudit(auditSvc).WithTasks(taskSvc)
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

	// 硬件加速能力服务（FR-49）：编码器实测唯一真源 + SQLite 缓存，后台预热。
	capSvc := transcoder.NewCapabilityService(gormDB)

	// 媒体健康巡检服务（FR-73）：后台只读巡检全部未软删媒体，问题落 media_health_issues 表。
	healthSvc := library.NewDefaultHealthService(gormDB)

	// 转码预设与预生成队列（FR-77）：预设存储 + 单 worker 串行预生成队列。
	// exec 闭包反查媒体路径并按预设编码调 PreSliceWithCodec 预热切片到 hlsDir/{mediaID}/。
	// 复用 FR-29 任务队列范式（见 ADR-0039），重启先恢复残留 running 再启动。
	presetStore := transcoder.NewPresetStore(gormDB)
	pregenQueue := transcoder.NewPregenQueue(gormDB, func(mediaID int64, codec string) error {
		mf, err := libSvc.GetMediaFileByID(mediaID)
		if err != nil {
			return fmt.Errorf("预生成反查媒体失败: mediaID=%d: %w", mediaID, err)
		}
		_, err = transcoder.PreSliceWithCodec(context.Background(), mf.ID, mf.FilePath, mf.Width, mf.Height, codec, hlsMgr, hlsDir)
		return err
	}).WithAudit(auditSvc).WithTasks(taskSvc)
	if err := pregenQueue.RecoverRunning(); err != nil {
		log.Printf("[WARN] 预生成队列重启恢复失败: %v", err)
	}
	pregenQueue.Start()
	defer pregenQueue.Stop()

	apiHandler := api.NewHandler(libSvc).WithHLSPreSlice(hlsDir, hlsMgr).WithVersion(version).WithSettings(settingsSvc).WithScanQueue(scanQueue).WithSettingsReload(scanScheduler.Reload).WithShareService(shareSvc).WithCapabilityService(capSvc).WithPlayback(pbSvc).WithStartTime(startTime).WithDBPath(cfg.DBPath).WithHealthService(healthSvc).WithTranscodePresets(presetStore, pregenQueue).WithDebugLogApply(dbLogger.SetEnabled).WithMetrics(metricsSampler).WithAudit(auditSvc).WithTasks(taskSvc)

	// 启动文件监听（FR-03）：对所有已注册本地目录开启 fsnotify 实时监听，
	// 新增/删除文件 500ms 去抖后自动入库/移除；失败仅记日志，不阻断启动。
	if w, err := watcher.New(libSvc); err != nil {
		log.Printf("[WARN] 文件监听初始化失败: %v", err)
	} else if err := w.WithScanQueue(scanQueue).Start(); err != nil {
		log.Printf("[WARN] 文件监听启动失败: %v", err)
	} else {
		defer w.Stop()
		log.Printf("[INFO] 文件监听已启动 (fsnotify)")
	}

	// gin 运行模式：默认 release（仅 info 级请求日志，不输出 [GIN-debug] 调试噪声）；
	// 仅当环境变量 JIANVIDEO_DEBUG=1/true 时启用 debug 模式。须在创建 gin 引擎前设置。
	if v := os.Getenv("JIANVIDEO_DEBUG"); v == "1" || v == "true" {
		gin.SetMode(gin.DebugMode)
		log.Printf("[INFO] gin 运行于 debug 模式（JIANVIDEO_DEBUG 已开启）")
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := web.NewRouter(cfg, gormDB, hlsMgr, frontendDist, apiHandler, pbSvc)

	// 后台预热硬件加速能力缓存（FR-49）：非阻塞，避免首次访问 /system 触发 3 分钟同步实测。
	capSvc.WarmCacheAsync()

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.ServerPort)
	log.Printf("JianVideo 启动于 %s", addr)
	// 自更新重启时新进程可能早于旧进程释放端口启动，故监听带短重试等待端口释放（FR-46）。
	ln, err := listenWithRetry(addr, 10*time.Second)
	if err != nil {
		log.Printf("[ERROR] 监听端口失败: %v", err)
		return
	}
	server := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("[ERROR] 服务启动失败: %v", err)
		return
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
