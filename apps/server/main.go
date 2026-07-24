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
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/config"
	"github.com/wcpe/JianVideo/internal/ai"
	"github.com/wcpe/JianVideo/internal/api"
	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/dblog"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/metrics"
	"github.com/wcpe/JianVideo/internal/migration"
	"github.com/wcpe/JianVideo/internal/netproxy"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/rollback"
	"github.com/wcpe/JianVideo/internal/settings"
	"github.com/wcpe/JianVideo/internal/share"
	spacepkg "github.com/wcpe/JianVideo/internal/space"
	"github.com/wcpe/JianVideo/internal/storage"
	"github.com/wcpe/JianVideo/internal/subtitle"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	thumbsvc "github.com/wcpe/JianVideo/internal/thumbnail"
	toolsvc "github.com/wcpe/JianVideo/internal/tools"
	"github.com/wcpe/JianVideo/internal/transcoder"
	"github.com/wcpe/JianVideo/internal/watcher"
	"github.com/wcpe/JianVideo/internal/web"
)

//go:embed all:web/dist
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

func transcodeConcurrencyFromSettings(service *settings.Service) int {
	if service == nil {
		return tasksvc.DefaultConcurrency(transcoder.TaskTypeHLSABR)
	}
	raw, _ := service.Get(settings.KeyTaskWorkerTranscodeConcurrency)
	value := int(settings.ParseInt64Setting(raw))
	if value <= 0 {
		return tasksvc.DefaultConcurrency(transcoder.TaskTypeHLSABR)
	}
	return value
}

func hlsPreviewCacheVariant(payload transcoder.HLSPreviewPayload, taskID int64) string {
	if transcoder.IsAudioReloadProfileID(payload.ProfileID) {
		return strconv.FormatInt(taskID, 10)
	}
	return ""
}

func registerABRAssets(ctx context.Context, cache *storage.Service, media *models.MediaFile, outputDir string, ladder []transcoder.QualityDefinition) error {
	if _, err := cache.RegisterFile(ctx, storage.RegisterInput{
		SpaceID: media.SpaceID, LibraryID: media.LibraryID, MediaID: media.ID,
		Kind: storage.CacheKindHLS, ProfileID: transcoder.ABRProfileID, Variant: "master",
		Path: filepath.Join(outputDir, "master.m3u8"),
	}); err != nil {
		return err
	}
	for _, variant := range ladder {
		if _, err := cache.RegisterDirectory(ctx, storage.RegisterInput{
			SpaceID: media.SpaceID, LibraryID: media.LibraryID, MediaID: media.ID,
			Kind: storage.CacheKindHLS, ProfileID: transcoder.ABRProfileID, Variant: variant.Name,
			Path: filepath.Join(outputDir, variant.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func registerTaskWorkers(workers *tasksvc.WorkerRegistry, taskSvc *tasksvc.Service, libSvc *library.Service, dataDir string) {
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
	exportRunner := library.NewExportTaskRunner(dataDir, libSvc, taskSvc)
	if err := exportRunner.RegisterExportWorkers(workers); err != nil {
		log.Fatalf("[ERROR] 注册导出任务 worker 失败: %v", err)
	}
	writebackRunner := library.NewWritebackTaskRunner(dataDir, libSvc, taskSvc)
	if err := writebackRunner.RegisterWritebackWorker(workers); err != nil {
		log.Fatalf("[ERROR] 注册元数据写回 worker 失败: %v", err)
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

func resolveHLSPreviewSource(ctx context.Context, libSvc *library.Service, subtitleSvc *subtitle.Service, payload transcoder.HLSPreviewPayload, policy transcoder.HardwarePolicy, ffmpegAvailable bool) (*models.MediaFile, error) {
	media, err := libSvc.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
	if err != nil {
		return nil, fmt.Errorf("HLS 预览反查媒体失败: mediaID=%d: %w", payload.MediaID, err)
	}
	if payload.AudioTrackID == "" && payload.AudioStreamIndex == nil {
		return media, nil
	}
	if payload.AudioTrackID == "" || payload.AudioStreamIndex == nil || strings.TrimSpace(payload.SourceFingerprint) == "" {
		return nil, fmt.Errorf("HLS 音轨任务源指纹不完整")
	}
	tracks, err := subtitleSvc.List(ctx, payload.SpaceID, payload.MediaID)
	if err != nil {
		return nil, fmt.Errorf("HLS 音轨任务读取轨道失败: %w", err)
	}
	if err := validateAudioReloadTrack(*media, payload, tracks.Tracks, policy, ffmpegAvailable); err != nil {
		return nil, err
	}
	return media, nil
}

func validateAudioReloadTrack(media models.MediaFile, payload transcoder.HLSPreviewPayload, tracks []subtitle.Track, policy transcoder.HardwarePolicy, ffmpegAvailable bool) error {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(media.FilePath)), "smb://") {
		return fmt.Errorf("HLS 音轨任务当前来源不是本地媒体")
	}
	if countCurrentAudioReloadTargets(tracks) <= 1 {
		return fmt.Errorf("HLS 音轨任务当前有效内嵌音轨不足两个")
	}
	track := findCurrentAudioReloadTarget(tracks, payload.AudioTrackID)
	if track == nil {
		return fmt.Errorf("HLS 音轨任务目标轨道不存在或无效")
	}
	if *track.StreamIndex != *payload.AudioStreamIndex {
		return fmt.Errorf("HLS 音轨任务流索引已变化")
	}
	if !ffmpegAvailable {
		return fmt.Errorf("HLS 音轨任务当前不可重载：FFmpeg 不可用")
	}
	if _, _, _, err := transcoder.SelectCurrentEncoderForCodecWithPolicy(transcoder.DefaultTargetCodec, policy); err != nil {
		return fmt.Errorf("HLS 音轨任务当前不可重载：%w", err)
	}
	fileInfo, err := os.Stat(media.FilePath)
	if err != nil {
		return fmt.Errorf("HLS 音轨任务读取真实源失败: %w", err)
	}
	media.FileSize = fileInfo.Size()
	media.ModifiedAt = fileInfo.ModTime()
	if audioReloadFingerprint(media, *track) != payload.SourceFingerprint {
		return fmt.Errorf("HLS 音轨任务真实源已变化")
	}
	return nil
}

func countCurrentAudioReloadTargets(tracks []subtitle.Track) int {
	count := 0
	for _, track := range tracks {
		if isCurrentAudioReloadTarget(track) {
			count++
		}
	}
	return count
}

func findCurrentAudioReloadTarget(tracks []subtitle.Track, trackID string) *subtitle.Track {
	for index := range tracks {
		if tracks[index].ID == trackID && isCurrentAudioReloadTarget(tracks[index]) {
			return &tracks[index]
		}
	}
	return nil
}

func isCurrentAudioReloadTarget(track subtitle.Track) bool {
	return track.Kind == subtitle.KindAudio && track.Source == subtitle.SourceEmbedded && track.Available && track.StreamIndex != nil && *track.StreamIndex >= 0
}

func audioReloadFingerprint(media models.MediaFile, track subtitle.Track) string {
	return transcoder.AudioReloadSourceFingerprint(transcoder.MediaIdentity{
		SpaceID: media.SpaceID, MediaID: media.ID, Path: media.FilePath, Size: media.FileSize,
		ModifiedAt: media.ModifiedAt, ContentHash: media.ContentHash, ContentHashStale: media.ContentHashStale,
	}, transcoder.AudioTrackIdentity{
		ID: track.ID, Index: *track.StreamIndex, Codec: track.Codec, Language: track.Language,
		Title: track.Title, Channels: track.Channels, ChannelLayout: track.ChannelLayout,
	})
}

func newSubtitleService(gormDB *gorm.DB, dataDir string, recorder audit.Recorder) *subtitle.Service {
	return subtitle.NewService(gormDB, dataDir).WithAudit(recorder)
}

func newTimelinePreviewGateway(gormDB *gorm.DB, taskSvc *tasksvc.Service, taskWorkers *tasksvc.WorkerRegistry, cacheSvc *storage.Service, dataDir string, generator transcoder.TimelinePreviewGenerator) (api.TimelinePreviewGateway, error) {
	service := transcoder.NewTimelinePreviewService(gormDB, taskSvc, taskWorkers, cacheSvc, dataDir, generator)
	if err := service.RegisterWorker(); err != nil {
		return nil, err
	}
	return api.NewTimelinePreviewGateway(service), nil
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
	os.Exit(run())
}

func run() int {
	migrationDryRun := flag.Bool("migration-dry-run", false, "只读输出数据库迁移计划后退出")
	flag.Parse()

	// 记录进程启动时刻，供系统诊断「运行环境」计算运行时长（FR-60）。
	startTime := time.Now()
	cfg := config.Load()

	// 确保数据库父目录存在（默认 data/，FR2-065），避免 SQLite 因缺目录打开失败。
	if dbDir := filepath.Dir(cfg.DBPath); dbDir != "" && dbDir != "." {
		if err := os.MkdirAll(dbDir, 0o750); err != nil {
			log.Fatalf("创建数据目录失败: %v", err)
		}
	}

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
			return 1
		}
		return 0
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
	library.InitExportDir(dataDir)
	library.InitWritebackSnapshotDir(dataDir)

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
	subtitleSvc := newSubtitleService(gormDB, dataDir, auditSvc)
	if err := taskSvc.RecoverRunning(context.Background()); err != nil {
		log.Printf("[WARN] 通用任务队列重启恢复失败: %v", err)
	}
	taskWorkers := tasksvc.NewWorkerRegistry(taskSvc)
	registerTaskWorkers(taskWorkers, taskSvc, libSvc, dataDir)
	aiSvc := ai.NewService(ai.NewGormRepository(gormDB), settingsSvc).WithTasks(taskSvc).WithAudit(auditSvc)
	// 开发/单测友好：注册 stub 节点实现（表内节点由 Seed 或管理 API 写入；此处仅内存 handler）
	aiSvc.RegisterNode(ai.NewStubNode("stub-local"))
	if err := ai.RegisterAIWorker(taskWorkers, aiSvc); err != nil {
		log.Printf("[ERROR] 注册 AI worker 失败: %v", err)
		return 1
	}
	libSvc.WithScanChangeHook(libSvc.MetadataScanChangeHook(taskSvc, taskWorkers.Wake))
	libSvc.WithInferenceCompensation(api.NewInferenceCompensationEnqueuer(taskSvc), taskWorkers.Wake)
	if err := cacheSvc.RegisterWorkers(taskWorkers); err != nil {
		log.Printf("[ERROR] 注册缓存任务处理器失败: %v", err)
		return 1
	}
	thumbnailSvc := thumbsvc.NewService(libSvc, taskSvc, cacheSvc, dataDir).WithAudit(auditSvc)
	if err := thumbnailSvc.RegisterWorkers(taskWorkers, thumbnailConcurrencyFromSettings(settingsSvc)); err != nil {
		log.Printf("[ERROR] 注册缩略图任务处理器失败: %v", err)
		return 1
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

	// 回收站到期自动清理调度（FR2-054）：周期仍全局；每 Space 用 ForSpace 解析 days/enabled。
	// 复用 ScanScheduler 定时骨架；遍历全部 Space，单 Space 失败不阻断其余。
	recycleRetentionScheduler := library.NewScanScheduler(
		settingsSvc.RecycleAutoCleanupInterval,
		func() {
			raw, err := settingsSvc.Get(settings.KeyRecycleBinPaths)
			if err != nil {
				log.Printf("[WARN] 回收站自动清理读取路径配置失败: %v", err)
				return
			}
			drivePaths := map[string]string{}
			if strings.TrimSpace(raw) != "" {
				if err := json.Unmarshal([]byte(raw), &drivePaths); err != nil {
					log.Printf("[WARN] 回收站自动清理路径 JSON 无效: %v", err)
					return
				}
			}
			// 列出全部 Space；表不存在或查询失败时回退默认 Space，保证单 Space 模式仍可清理。
			spaceIDs := []string{models.DefaultSpaceID}
			if gormDB.Migrator().HasTable(&models.Space{}) {
				var ids []string
				if qerr := gormDB.Model(&models.Space{}).Order("id ASC").Pluck("id", &ids).Error; qerr != nil {
					log.Printf("[WARN] 回收站自动清理枚举 Space 失败，回退默认: %v", qerr)
				} else if len(ids) > 0 {
					spaceIDs = ids
				}
			}
			for _, spaceID := range spaceIDs {
				if !settingsSvc.RecycleAutoCleanupEnabledForSpace(spaceID) {
					continue
				}
				days := settingsSvc.RecycleRetentionDaysForSpace(spaceID)
				if days <= 0 {
					continue
				}
				before := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
				result, err := libSvc.AutoCleanupExpiredInSpace(spaceID, drivePaths, before, 50)
				if err != nil {
					log.Printf("[WARN] 回收站自动清理失败: space=%s err=%v", spaceID, err)
					continue
				}
				if result.Candidate > 0 {
					log.Printf("[INFO] 回收站自动清理: space=%s candidate=%d moved=%d failed=%d skipped=%d",
						spaceID, result.Candidate, result.Moved, result.Failed, result.Skipped)
				}
				if auditSvc != nil && result.Candidate > 0 {
					_ = auditSvc.Record(context.Background(), audit.EventInput{
						Scope:        audit.ScopeSpace,
						SpaceID:      spaceID,
						ActorType:    "system",
						ActorID:      "recycle.retention.tick",
						Action:       "recycle.auto_cleanup",
						ResourceType: "recycle",
						ResourceID:   spaceID,
						After: map[string]any{
							"candidate": result.Candidate, "moved": result.Moved,
							"failed": result.Failed, "skipped": result.Skipped,
							"missing_drives": result.MissingDrives,
						},
					})
				}
			}
		},
	)
	recycleRetentionScheduler.Start()
	defer recycleRetentionScheduler.Stop()

	// 写回快照保留期清理（FR2-033）：固定 24h 周期；days=0 时 trigger 空跑。
	// 与 recycle 调度并列注册，改动集中于此块，减少与 recycle 段落的合并冲突。
	writebackSnapshotCleanupScheduler := library.NewScanScheduler(
		func() time.Duration { return 24 * time.Hour },
		func() {
			days := settingsSvc.WritebackSnapshotRetentionDays()
			if days <= 0 {
				return
			}
			before := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
			n, err := library.CleanupWritebackSnapshots(dataDir, before)
			if err != nil {
				log.Printf("[WARN] 写回快照清理失败: %v", err)
				return
			}
			if n > 0 {
				log.Printf("[INFO] 写回快照清理: removed=%d before=%s", n, before.UTC().Format(time.RFC3339))
			}
		},
	)
	writebackSnapshotCleanupScheduler.Start()
	defer writebackSnapshotCleanupScheduler.Stop()

	// 硬件加速能力服务（FR-49）：编码器实测唯一真源 + SQLite 缓存，后台预热。
	capSvc := transcoder.NewCapabilityService(gormDB)
	if err := capSvc.LoadCachedSnapshot(context.Background()); err != nil {
		log.Printf("[WARN] 启动时加载硬件加速能力缓存失败: %v", err)
	}

	// 媒体健康巡检服务（FR-73）：后台只读巡检全部未软删媒体，问题落 media_health_issues 表。
	healthSvc := library.NewDefaultHealthService(gormDB)

	// HLS preview 统一任务（FR2-008）：执行真源只使用通用 tasks，旧转码 API 作为兼容适配层。
	presetStore := transcoder.NewPresetStore(gormDB)
	hlsPreview := transcoder.NewHLSPreviewService(taskSvc, taskWorkers, hlsDir, func(ctx context.Context, taskID int64, payload transcoder.HLSPreviewPayload) error {
		policy := hardwarePolicyFromSettings(settingsSvc)
		mf, err := resolveHLSPreviewSource(ctx, libSvc, subtitleSvc, payload, policy, transcoder.IsFFmpegAvailable())
		if err != nil {
			return err
		}
		outputDir, err := transcoder.HLSPreviewOutputDir(hlsDir, taskID, payload)
		if err != nil {
			return err
		}
		cacheVariant := hlsPreviewCacheVariant(payload, taskID)
		manifest := "index.m3u8"
		if transcoder.SelectOutputPath(payload.Codec) == transcoder.OutputPathTS {
			manifest = "master.m3u8"
		}
		if !payload.ForceRebuild {
			if _, statErr := os.Stat(filepath.Join(outputDir, manifest)); statErr == nil {
				_, err = cacheSvc.RegisterDirectory(ctx, storage.RegisterInput{SpaceID: mf.SpaceID, LibraryID: mf.LibraryID, MediaID: mf.ID, Kind: storage.CacheKindHLS, ProfileID: payload.ProfileID, Variant: cacheVariant, Path: outputDir})
				return err
			}
		}
		if err := cacheSvc.PrepareHLSRebuild(ctx, payload.SpaceID, payload.MediaID, payload.ProfileID, outputDir); err != nil {
			return fmt.Errorf("安全清理旧 HLS profile 失败: %w", err)
		}
		previewWidth, previewHeight := transcoder.HLSPreviewResolution(payload, mf.Width, mf.Height)
		var result *transcoder.PreSliceResult
		if payload.AudioStreamIndex != nil {
			result, err = transcoder.PreSliceAudioTrackWithPolicyToDir(ctx, mf.ID, mf.FilePath, previewWidth, previewHeight, *payload.AudioStreamIndex, policy, outputDir)
		} else {
			result, err = transcoder.PreSliceWithCodecAndPolicyToDir(ctx, mf.ID, mf.FilePath, previewWidth, previewHeight, payload.Codec, policy, outputDir)
		}
		if err != nil {
			return err
		}
		_, err = cacheSvc.RegisterDirectory(ctx, storage.RegisterInput{
			SpaceID: mf.SpaceID, LibraryID: mf.LibraryID, MediaID: mf.ID,
			Kind: storage.CacheKindHLS, ProfileID: payload.ProfileID, Variant: cacheVariant, Path: result.OutputDir,
		})
		return err
	})
	if err := hlsPreview.RegisterWorker(); err != nil {
		log.Printf("[ERROR] 注册 HLS 预览任务处理器失败: %v", err)
		return 1
	}

	abrService := transcoder.NewABRService(taskSvc, taskWorkers, hlsDir, func(ctx context.Context, taskID int64, payload transcoder.ABRPayload) error {
		mf, err := libSvc.GetMediaFileByIDInSpace(payload.SpaceID, payload.MediaID)
		if err != nil {
			return fmt.Errorf("ABR 反查媒体失败: mediaID=%d: %w", payload.MediaID, err)
		}
		outputDir, err := transcoder.HLSProfileDir(hlsDir, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		masterPath := filepath.Join(outputDir, "master.m3u8")
		if !payload.ForceRebuild {
			if _, statErr := os.Stat(masterPath); statErr == nil {
				return registerABRAssets(ctx, cacheSvc, mf, outputDir, payload.Ladder)
			}
		}
		if err := cacheSvc.PrepareHLSRebuild(ctx, payload.SpaceID, payload.MediaID, payload.ProfileID, outputDir); err != nil {
			return fmt.Errorf("安全清理旧 ABR profile 失败: %w", err)
		}
		if err := taskSvc.UpdateProgress(ctx, taskID, tasksvc.ProgressInput{Progress: 20, Checkpoint: "已清理旧 ABR 产物"}); err != nil {
			return err
		}
		policy := transcoder.HardwarePolicy{
			Mode: transcoder.NormalizeHWAccelMode(payload.HWAccelPreference), Fallback: settingsSvc.TranscodeHWAccelFallback(),
		}
		if _, err := transcoder.PreSliceABRWithPolicyToDir(ctx, mf.ID, mf.FilePath, payload.Ladder, policy, outputDir); err != nil {
			return err
		}
		if err := taskSvc.UpdateProgress(ctx, taskID, tasksvc.ProgressInput{Progress: 85, Checkpoint: "已生成全部 ABR 档位"}); err != nil {
			return err
		}
		return registerABRAssets(ctx, cacheSvc, mf, outputDir, payload.Ladder)
	})
	if err := abrService.RegisterWorker(transcodeConcurrencyFromSettings(settingsSvc)); err != nil {
		log.Printf("[ERROR] 注册 ABR 任务处理器失败: %v", err)
		return 1
	}
	timelineGateway, err := newTimelinePreviewGateway(
		gormDB, taskSvc, taskWorkers, cacheSvc, dataDir,
		transcoder.NewFFmpegTimelinePreviewGenerator(""),
	)
	if err != nil {
		log.Printf("[ERROR] 注册时间轴预览任务处理器失败: %v", err)
		return 1
	}
	taskWorkerCtx, stopTaskWorkers := context.WithCancel(context.Background())
	go startTaskWorkers(taskWorkerCtx, taskWorkers)
	defer stopTaskWorkers()

	authSvc := auth.NewService(sqlDB, cfg.JWTSecret)
	spaceSvc := spacepkg.NewService(gormDB)
	rollbackSvc := rollback.NewService(auditSvc, settingsSvc, libSvc)
	apiHandler := api.NewHandler(libSvc).WithVersion(version).WithSettings(settingsSvc).WithScanQueue(scanQueue).WithSettingsReload(func() {
		scanScheduler.Reload()
		recycleRetentionScheduler.Reload()
		// writeback 快照清理周期固定 24h，Reload 仅占位；days 热读 settings 无需重启
		writebackSnapshotCleanupScheduler.Reload()
	}).WithShareService(shareSvc).WithCapabilityService(capSvc).WithPlayback(pbSvc).WithStartTime(startTime).WithDBPath(cfg.DBPath).WithHealthService(healthSvc).WithTranscodePresets(presetStore, nil).WithHLSPreview(hlsPreview).WithHLSABR(abrService).WithHLSPreSlice(hlsDir, hlsMgr).WithDebugLogApply(dbLogger.SetEnabled).WithMetrics(metricsSampler).WithAudit(auditSvc).WithRollback(rollbackSvc).WithTasks(taskSvc).WithTaskWorkers(taskWorkers).WithTools(toolsManager).WithCache(cacheSvc).WithThumbnail(thumbnailSvc).WithTimelinePreview(timelineGateway).WithSubtitle(subtitleSvc).WithAuth(authSvc).WithSpace(spaceSvc).WithAI(aiSvc)

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
		return 0
	}
	server := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("[ERROR] 服务启动失败: %v", err)
		return 0
	}
	return 0
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
