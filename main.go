// Package main 是 JianVideo 服务端程序入口，负责初始化各模块并启动 HTTP 服务。
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	thumbsvc "github.com/wcpe/JianVideo/internal/thumbnail"
	toolsvc "github.com/wcpe/JianVideo/internal/tools"
	"github.com/wcpe/JianVideo/internal/transcoder"
	"github.com/wcpe/JianVideo/internal/watcher"
	"github.com/wcpe/JianVideo/internal/web"
)

//go:embed frontend/dist
var frontendDist embed.FS

func sqliteDataSourceName(dbPath string) string {
	return appendSQLiteOptions(dbPath, "_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=on")
}

func sqliteReadOnlyDataSourceName(dbPath string) string {
	fileURL := sqliteFileURL(dbPath)
	query := url.Values{}
	query.Set("mode", "ro")
	query.Set("_busy_timeout", "10000")
	query.Set("_foreign_keys", "on")
	fileURL.RawQuery = query.Encode()
	return fileURL.String()
}

func sqliteFileURL(dbPath string) *url.URL {
	path := filepath.ToSlash(dbPath)
	if strings.HasPrefix(path, "//") {
		escapedPath := (&url.URL{Path: path}).EscapedPath()
		return &url.URL{Scheme: "file", Opaque: "//" + escapedPath}
	}
	if filepath.IsAbs(dbPath) && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return &url.URL{Scheme: "file", Path: path}
}

func appendSQLiteOptions(dbPath, options string) string {
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	return dbPath + separator + options
}

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

func hardwarePolicyFromSettings(svc *settings.Service) transcoder.HardwarePolicy {
	if svc == nil {
		return transcoder.DefaultHardwarePolicy()
	}
	return transcoder.HardwarePolicy{
		Mode:     transcoder.NormalizeHWAccelMode(svc.TranscodeHWAccelMode()),
		Fallback: svc.TranscodeHWAccelFallback(),
	}
}

func thumbnailConcurrencyFromSettings(service *settings.Service) int {
	if service == nil {
		return tasksvc.DefaultConcurrency(thumbsvc.TaskTypeGenerate)
	}
	raw, _ := service.Get(settings.KeyTaskWorkerThumbnailConcurrency)
	value := int(settings.ParseInt64Setting(raw))
	if value <= 0 {
		return tasksvc.DefaultConcurrency(thumbsvc.TaskTypeGenerate)
	}
	return value
}

func registerTaskWorkers(workers *tasksvc.WorkerRegistry, taskSvc *tasksvc.Service, libSvc *library.Service) {
	if err := workers.Register(library.TaskTypeFileHashBackfill, tasksvc.DefaultConcurrency(library.TaskTypeFileHashBackfill), func(ctx context.Context, task models.Task) error {
		return libSvc.HandleContentHashBackfillTask(ctx, taskSvc, task)
	}); err != nil {
		log.Printf("[WARN] 内容哈希回填 worker 注册失败: %v", err)
	}
	if err := api.RegisterInferenceBackfillWorker(workers, libSvc); err != nil {
		log.Fatalf("[ERROR] 注册离线推断回填 worker 失败: %v", err)
	}
	if err := library.RegisterMetadataWorkers(workers, taskSvc, libSvc); err != nil {
		log.Fatalf("[ERROR] 注册文件元数据 worker 失败: %v", err)
	}
}

func applyInstalledTool(result toolsvc.InstallResult) error {
	switch result.Tool {
	case toolsvc.ToolFFmpeg:
		transcoder.SetFFmpegPath(result.Path)
		library.SetFFmpegPath(result.Path)
	case toolsvc.ToolFFprobe:
		transcoder.SetFFprobePath(result.Path)
		library.SetFFprobePath(result.Path)
	case toolsvc.ToolMagick:
		library.SetMagickPath(result.Path)
	}
	return nil
}

func startTaskWorkers(ctx context.Context, workers *tasksvc.WorkerRegistry) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := workers.RunPending(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[WARN] 通用任务 worker 执行失败: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func main() {
	migrationDryRun := flag.Bool("migration-dry-run", false, "只读输出数据库迁移计划后退出")
	flag.Parse()

	// 记录进程启动时刻，供系统诊断「运行环境」计算运行时长（FR-60）。
	startTime := time.Now()
	cfg := config.Load()

	// 可运行时切换级别的 GORM 日志器（FR-110）：默认安静（不刷 record-not-found/普通 SQL），
	// 由「调试日志」开关在运行期切到 Info 级；启动时下方读取设置决定初始级别。
	dbLogger := dblog.NewDefault()

	// 使用 gorm 打开数据库（同时兼容 db 包的 InitSchema）
	dataSourceName := sqliteDataSourceName(cfg.DBPath)
	if *migrationDryRun {
		dataSourceName = sqliteReadOnlyDataSourceName(cfg.DBPath)
	}
	gormDB, err := gorm.Open(sqlite.Open(dataSourceName), &gorm.Config{Logger: dbLogger})
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		log.Fatalf("获取数据库连接池失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)

	registry, err := migration.NewRegistry(migration.DefaultMigrations()...)
	if err != nil {
		log.Fatalf("数据库迁移注册失败: %v", err)
	}
	runner := migration.NewRunner(gormDB, migration.RunnerOptions{
		DBPath:    cfg.DBPath,
		BackupDir: filepath.Join(filepath.Dir(cfg.DBPath), "backups"),
		Registry:  registry,
	})
	if *migrationDryRun {
		plan, err := runner.DryRun(context.Background())
		if err != nil {
			log.Fatalf("数据库迁移 dry-run 失败: %v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(plan); err != nil {
			log.Fatalf("输出数据库迁移 dry-run JSON 失败: %v", err)
		}
		if len(plan.Blockers) > 0 {
			os.Exit(1)
		}
		return
	}
	migrationResult, err := runner.Run(context.Background())
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
	library.SetFFmpegPath(transcoder.GetFFmpegPath())
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
	dataDir := filepath.Dir(cfg.DBPath)
	library.InitThumbnailDir(dataDir)

	// magick 路径注入与 HEIC/RAW 转换缓存目录初始化（FR-37，见 ADR）：
	// 解析顺序与 ffmpeg 一致：环境变量 → 同目录捆绑版 → PATH。
	library.SetMagickPath(resolveTool("JIANVIDEO_MAGICK_PATH", "magick"))
	// 持久化设置优先于自动发现（FR-63）：settings 中 magick_path 非空则覆盖。
	if p, _ := settingsSvc.Get(settings.KeyMagickPath); p != "" {
		library.SetMagickPath(p)
		log.Printf("[INFO] 采用持久化设置的 magick 路径: %s", p)
	}
	library.InitConvertCacheDir(dataDir)
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
	libSvc := library.NewService(gormDB).WithAudit(auditSvc).WithInferenceConfigProvider(func(_ string, _ int64) library.InferenceConfig {
		enabledRaw, _ := settingsSvc.Get(settings.KeyMediaInferenceEnabled)
		disabledRaw, _ := settingsSvc.Get(settings.KeyMediaInferenceDisabledLibraries)
		generationRaw, _ := settingsSvc.Get(settings.KeyMediaInferenceGeneration)
		return library.InferenceConfig{
			Enabled:           settings.ParseBoolSetting(enabledRaw, true),
			DisabledLibraries: library.ParseDisabledInferenceLibraries(disabledRaw),
			Generation:        settings.ParseInt64Setting(generationRaw),
		}
	})
	shareSvc := share.NewService(gormDB)
	taskSvc := tasksvc.NewService(gormDB).WithAudit(auditSvc)
	cacheSvc := storage.NewService(gormDB, dataDir).WithAudit(auditSvc).WithTasks(taskSvc)
	if err := taskSvc.RecoverRunning(context.Background()); err != nil {
		log.Printf("[WARN] 通用任务队列重启恢复失败: %v", err)
	}
	taskWorkers := tasksvc.NewWorkerRegistry(taskSvc)
	registerTaskWorkers(taskWorkers, taskSvc, libSvc)
	libSvc.WithScanChangeHook(libSvc.MetadataScanChangeHook(taskSvc, taskWorkers.Wake))
	libSvc.WithInferenceCompensation(api.NewInferenceCompensationEnqueuer(taskSvc), taskWorkers.Wake)
	if err := cacheSvc.RegisterWorkers(taskWorkers); err != nil {
		log.Fatalf("[ERROR] 注册缓存任务 worker 失败: %v", err)
	}
	thumbnailSvc := thumbsvc.NewService(libSvc, taskSvc, cacheSvc, dataDir)
	if err := thumbnailSvc.RegisterWorkers(taskWorkers, thumbnailConcurrencyFromSettings(settingsSvc)); err != nil {
		log.Fatalf("[ERROR] 注册缩略图任务 worker 失败: %v", err)
	}
	toolsManager := toolsvc.NewManager(toolsvc.ManagerOptions{
		Installer: toolsvc.NewInstaller(filepath.Join(filepath.Dir(cfg.DBPath), "tools"), nil),
		Settings:  settingsSvc,
		Tasks:     taskSvc,
		Apply:     applyInstalledTool,
	})
	if err := toolsManager.RegisterWorker(taskWorkers); err != nil {
		log.Printf("[WARN] 工具下载 worker 注册失败: %v", err)
	}
	taskWorkerCtx, stopTaskWorkers := context.WithCancel(context.Background())
	go startTaskWorkers(taskWorkerCtx, taskWorkers)
	defer stopTaskWorkers()

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
			libs, err := libSvc.ListAllLibraryPaths()
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
	if err := capSvc.LoadCachedSnapshot(context.Background()); err != nil {
		log.Printf("[WARN] 启动时加载硬件加速能力缓存失败: %v", err)
	}

	// 媒体健康巡检服务（FR-73）：后台只读巡检全部未软删媒体，问题落 media_health_issues 表。
	healthSvc := library.NewDefaultHealthService(gormDB)

	// HLS preview 统一任务（FR2-008）：执行真源只使用通用 tasks，旧转码 API 作为兼容适配层。
	presetStore := transcoder.NewPresetStore(gormDB)
	hlsPreview := transcoder.NewHLSPreviewService(taskSvc, taskWorkers, hlsDir, func(ctx context.Context, _ int64, payload transcoder.HLSPreviewPayload) error {
		mf, err := libSvc.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
		if err != nil {
			return fmt.Errorf("HLS 预览反查媒体失败: mediaID=%d: %w", payload.MediaID, err)
		}
		outputDir, err := transcoder.HLSProfileDir(hlsDir, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		manifest := "index.m3u8"
		if transcoder.SelectOutputPath(payload.Codec) == transcoder.OutputPathTS {
			manifest = "master.m3u8"
		}
		if !payload.ForceRebuild {
			if _, statErr := os.Stat(filepath.Join(outputDir, manifest)); statErr == nil {
				_, err = cacheSvc.RegisterDirectory(ctx, storage.RegisterInput{SpaceID: mf.SpaceID, LibraryID: mf.LibraryID, MediaID: mf.ID, Kind: storage.CacheKindHLS, ProfileID: payload.ProfileID, Path: outputDir})
				return err
			}
		}
		if err := cacheSvc.PrepareHLSRebuild(ctx, payload.SpaceID, payload.MediaID, payload.ProfileID, outputDir); err != nil {
			return fmt.Errorf("安全清理旧 HLS profile 失败: %w", err)
		}
		previewWidth, previewHeight := transcoder.HLSPreviewResolution(payload, mf.Width, mf.Height)
		result, err := transcoder.PreSliceWithCodecAndPolicyToDir(ctx, mf.ID, mf.FilePath, previewWidth, previewHeight, payload.Codec, hardwarePolicyFromSettings(settingsSvc), outputDir)
		if err != nil {
			return err
		}
		_, err = cacheSvc.RegisterDirectory(ctx, storage.RegisterInput{
			SpaceID: mf.SpaceID, LibraryID: mf.LibraryID, MediaID: mf.ID,
			Kind: storage.CacheKindHLS, ProfileID: payload.ProfileID, Path: result.OutputDir,
		})
		return err
	})
	if err := hlsPreview.RegisterWorker(); err != nil {
		log.Fatalf("[ERROR] 注册 HLS preview worker 失败: %v", err)
	}

	apiHandler := api.NewHandler(libSvc).WithHLSPreSlice(hlsDir, hlsMgr).WithVersion(version).WithSettings(settingsSvc).WithScanQueue(scanQueue).WithSettingsReload(scanScheduler.Reload).WithShareService(shareSvc).WithCapabilityService(capSvc).WithPlayback(pbSvc).WithStartTime(startTime).WithDBPath(cfg.DBPath).WithHealthService(healthSvc).WithTranscodePresets(presetStore, nil).WithHLSPreview(hlsPreview).WithDebugLogApply(dbLogger.SetEnabled).WithMetrics(metricsSampler).WithAudit(auditSvc).WithTasks(taskSvc).WithTaskWorkers(taskWorkers).WithTools(toolsManager).WithCache(cacheSvc).WithThumbnail(thumbnailSvc)

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
