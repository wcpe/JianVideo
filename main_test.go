package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/api"
	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/storage"
	"github.com/wcpe/JianVideo/internal/subtitle"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

func TestMainSubtitle装配使用应用数据目录数据库与审计(t *testing.T) {
	root := t.TempDir()
	db := openMainTimelineDB(t, root)
	if err := db.AutoMigrate(&models.MediaFile{}, &models.MediaSubtitleTrack{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移字幕装配测试表失败: %v", err)
	}
	media := models.MediaFile{ID: 1, SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: filepath.Join(root, "source.mkv"), FileName: "source.mkv", FileState: models.MediaFileStateAvailable}
	if err := os.WriteFile(media.FilePath, []byte("source"), 0o600); err != nil {
		t.Fatalf("创建字幕装配测试媒体失败: %v", err)
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建字幕装配测试媒体记录失败: %v", err)
	}
	service := newSubtitleService(db, root, audit.NewRecorder(db))
	track, err := service.Upload(context.Background(), media.SpaceID, media.ID, "main.srt", strings.NewReader("1\n00:00:01,000 --> 00:00:02,000\n主程序\n"))
	if err != nil {
		t.Fatalf("主程序装配后的字幕服务上传失败: %v", err)
	}
	path, err := service.StoredPath(context.Background(), media.SpaceID, media.ID, track.ID)
	if err != nil || !strings.HasPrefix(filepath.Clean(path), filepath.Join(root, "subtitles")) {
		t.Fatalf("字幕服务未使用应用数据目录: path=%q err=%v", path, err)
	}
	if err := service.Delete(context.Background(), media.SpaceID, media.ID, track.ID); err != nil {
		t.Fatalf("主程序装配后的字幕删除失败: %v", err)
	}
	var count int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "subtitle.deleted", track.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("字幕服务未使用注入审计: count=%d err=%v", count, err)
	}
}

func TestResolveHLSPreviewSourceRejectsSingleAudioAfterEnqueue(t *testing.T) {
	libSvc, subtitleSvc, db, media, payload, manifest := setupMainAudioReloadSource(t)
	normalized, _ := json.Marshal(map[string]any{"audio_streams": []map[string]any{{"index": 1, "codec_name": "aac", "title": "主音轨"}}})
	if err := db.Model(&models.MediaMetadata{}).Where("space_id = ? AND media_id = ?", media.SpaceID, media.ID).Update("normalized_json", string(normalized)).Error; err != nil {
		t.Fatalf("更新单音轨元数据失败: %v", err)
	}
	current, err := subtitleSvc.List(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(current.Tracks) != 1 || current.Tracks[0].ID != payload.AudioTrackID || current.Tracks[0].StreamIndex == nil || *current.Tracks[0].StreamIndex != *payload.AudioStreamIndex {
		t.Fatalf("当前元数据必须只保留 payload 指向的同一有效音轨: tracks=%+v payload=%+v err=%v", current.Tracks, payload, err)
	}
	if _, err := resolveHLSPreviewSource(context.Background(), libSvc, subtitleSvc, payload, transcoder.HardwarePolicy{Mode: transcoder.HWAccelModeSoftware, Fallback: true}, true); err == nil {
		t.Fatal("任务目标轨未变化但当前只剩一个有效音轨时必须拒绝")
	}
	if data, err := os.ReadFile(manifest); err != nil || string(data) != "旧清单" {
		t.Fatalf("执行期校验失败不得删除旧 manifest: data=%q err=%v", data, err)
	}
}

func TestResolveHLSPreviewSourceRejectsMediaAndTrackChangesBeforeCleanup(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*testing.T, *gorm.DB, models.MediaFile)
	}{
		{name: "内容哈希变化", apply: func(t *testing.T, db *gorm.DB, media models.MediaFile) {
			if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Update("content_hash", "changed-hash").Error; err != nil {
				t.Fatalf("更新媒体身份失败: %v", err)
			}
		}},
		{name: "轨道变化", apply: func(t *testing.T, db *gorm.DB, media models.MediaFile) {
			normalized, _ := json.Marshal(map[string]any{"audio_streams": []map[string]any{
				{"index": 1, "codec_name": "aac", "title": "已变化"},
				{"index": 2, "codec_name": "aac", "title": "备用音轨"},
			}})
			if err := db.Model(&models.MediaMetadata{}).Where("space_id = ? AND media_id = ?", media.SpaceID, media.ID).Update("normalized_json", string(normalized)).Error; err != nil {
				t.Fatalf("更新轨道身份失败: %v", err)
			}
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			libSvc, subtitleSvc, db, media, payload, manifest := setupMainAudioReloadSource(t)
			change.apply(t, db, media)
			if _, err := resolveHLSPreviewSource(context.Background(), libSvc, subtitleSvc, payload, transcoder.HardwarePolicy{Mode: transcoder.HWAccelModeSoftware, Fallback: true}, true); err == nil {
				t.Fatal("源身份变化必须在清理旧 profile 前失败")
			}
			if data, err := os.ReadFile(manifest); err != nil || string(data) != "旧清单" {
				t.Fatalf("校验失败不得删除旧 manifest: data=%q err=%v", data, err)
			}
		})
	}
}

func TestResolveHLSPreviewSourceAcceptsUnchangedRealFile(t *testing.T) {
	libSvc, subtitleSvc, _, media, payload, _ := setupMainAudioReloadSource(t)
	resolved, err := resolveHLSPreviewSource(context.Background(), libSvc, subtitleSvc, payload, transcoder.HardwarePolicy{Mode: transcoder.HWAccelModeSoftware, Fallback: true}, true)
	if err != nil {
		t.Fatalf("未变化的真实源应通过执行期校验: %v", err)
	}
	if resolved.ID != media.ID {
		t.Fatalf("执行期校验返回了错误媒体: got=%d want=%d", resolved.ID, media.ID)
	}
}

func TestResolveHLSPreviewSourceAcceptsStaleDatabaseFileSnapshot(t *testing.T) {
	libSvc, subtitleSvc, db, media, payload, _ := setupMainAudioReloadSource(t)
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Updates(map[string]any{
		"file_size":   media.FileSize + 1,
		"modified_at": media.ModifiedAt.Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("写入过期媒体快照失败: %v", err)
	}
	resolved, err := resolveHLSPreviewSource(context.Background(), libSvc, subtitleSvc, payload, transcoder.HardwarePolicy{Mode: transcoder.HWAccelModeSoftware, Fallback: true}, true)
	if err != nil {
		t.Fatalf("真实文件未变化时不应受过期数据库快照影响: %v", err)
	}
	if resolved.ID != media.ID {
		t.Fatalf("执行期校验返回了错误媒体: got=%d want=%d", resolved.ID, media.ID)
	}
}

func TestResolveHLSPreviewSourceRejectsRealFileChangesBeforeCleanup(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*testing.T, models.MediaFile)
	}{
		{name: "真实大小变化", apply: func(t *testing.T, media models.MediaFile) {
			if err := os.WriteFile(media.FilePath, []byte("source-changed"), 0o600); err != nil {
				t.Fatalf("修改真实媒体大小失败: %v", err)
			}
			if err := os.Chtimes(media.FilePath, media.ModifiedAt, media.ModifiedAt); err != nil {
				t.Fatalf("恢复真实媒体修改时间失败: %v", err)
			}
		}},
		{name: "真实修改时间变化", apply: func(t *testing.T, media models.MediaFile) {
			changedAt := media.ModifiedAt.Add(2 * time.Second)
			if err := os.Chtimes(media.FilePath, changedAt, changedAt); err != nil {
				t.Fatalf("修改真实媒体修改时间失败: %v", err)
			}
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			libSvc, subtitleSvc, db, media, payload, manifest := setupMainAudioReloadSource(t)
			change.apply(t, media)
			var stored models.MediaFile
			if err := db.First(&stored, media.ID).Error; err != nil {
				t.Fatalf("读取未更新的媒体记录失败: %v", err)
			}
			if stored.FileSize != media.FileSize || !stored.ModifiedAt.Equal(media.ModifiedAt) {
				t.Fatalf("测试不得更新媒体数据库快照: stored=%+v media=%+v", stored, media)
			}
			if _, err := resolveHLSPreviewSource(context.Background(), libSvc, subtitleSvc, payload, transcoder.HardwarePolicy{Mode: transcoder.HWAccelModeSoftware, Fallback: true}, true); err == nil {
				t.Fatal("真实源身份变化必须在清理旧 profile 前失败")
			}
			if data, err := os.ReadFile(manifest); err != nil || string(data) != "旧清单" {
				t.Fatalf("真实源校验失败不得删除旧 manifest: data=%q err=%v", data, err)
			}
		})
	}
}

func setupMainAudioReloadSource(t *testing.T) (*library.Service, *subtitle.Service, *gorm.DB, models.MediaFile, transcoder.HLSPreviewPayload, string) {
	t.Helper()
	root := t.TempDir()
	db := openMainTimelineDB(t, root)
	if err := db.AutoMigrate(&models.MediaFile{}, &models.MediaMetadata{}, &models.MediaSubtitleTrack{}); err != nil {
		t.Fatalf("迁移音轨源校验测试表失败: %v", err)
	}
	media := models.MediaFile{ID: 1, SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: filepath.Join(root, "source.mkv"), FileName: "source.mkv", FileState: models.MediaFileStateAvailable, ContentHash: "hash", ContentHashStale: false}
	if err := os.WriteFile(media.FilePath, []byte("source"), 0o600); err != nil {
		t.Fatalf("创建音轨源文件失败: %v", err)
	}
	fileInfo, err := os.Stat(media.FilePath)
	if err != nil {
		t.Fatalf("读取音轨源文件身份失败: %v", err)
	}
	media.FileSize = fileInfo.Size()
	media.ModifiedAt = fileInfo.ModTime()
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建音轨源媒体失败: %v", err)
	}
	normalized, _ := json.Marshal(map[string]any{"audio_streams": []map[string]any{
		{"index": 1, "codec_name": "aac", "title": "主音轨"},
		{"index": 2, "codec_name": "aac", "title": "备用音轨"},
	}})
	metadata := models.MediaMetadata{MediaID: media.ID, SpaceID: media.SpaceID, Source: "ffprobe", Tool: "ffprobe", ToolVersion: "7", RawJSON: `{}`, NormalizedJSON: string(normalized), ParsedAt: time.Now(), Stale: false}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatalf("创建音轨元数据失败: %v", err)
	}
	subtitleSvc := subtitle.NewService(db, root)
	tracks, err := subtitleSvc.List(context.Background(), media.SpaceID, media.ID)
	if err != nil || len(tracks.Tracks) != 2 || tracks.Tracks[0].StreamIndex == nil {
		t.Fatalf("读取测试音轨失败: tracks=%+v err=%v", tracks.Tracks, err)
	}
	track := tracks.Tracks[0]
	payload := transcoder.HLSPreviewPayload{SpaceID: media.SpaceID, MediaID: media.ID, ProfileID: transcoder.AudioReloadProfileID(track.ID), Codec: transcoder.DefaultTargetCodec, AudioTrackID: track.ID, AudioStreamIndex: track.StreamIndex, SourceFingerprint: audioReloadFingerprint(media, track), ForceRebuild: true}
	profileDir := filepath.Join(root, "hls", media.SpaceID, "1", payload.ProfileID)
	if err := os.MkdirAll(profileDir, 0o750); err != nil {
		t.Fatalf("创建旧 profile 目录失败: %v", err)
	}
	manifest := filepath.Join(profileDir, "master.m3u8")
	if err := os.WriteFile(manifest, []byte("旧清单"), 0o600); err != nil {
		t.Fatalf("创建旧 manifest 失败: %v", err)
	}
	return library.NewService(db), subtitleSvc, db, media, payload, manifest
}

func TestMainTimelinePreview装配失败时返回错误(t *testing.T) {
	root := t.TempDir()
	db := openMainTimelineDB(t, root)
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	_, err := newTimelinePreviewGateway(db, tasks, workers, storage.NewService(db, root), root, nil)
	if err == nil {
		t.Fatal("生成器缺失时必须阻断时间轴预览装配")
	}
}

func TestMainTimelinePreview装配注册Worker并使用正式根目录(t *testing.T) {
	gateway, workers, generator, root := setupMainTimelinePreview(t)
	status, err := gateway.Enqueue(context.Background(), api.TimelinePreviewIdentity{
		SpaceID: models.DefaultSpaceID, MediaID: 1,
	})
	if err != nil || status.TaskID <= 0 {
		t.Fatalf("装配后的网关入队失败: status=%+v err=%v", status, err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("装配后的 worker 无法处理任务: %v", err)
	}
	rootDir := filepath.Join(root, "timeline_previews")
	if generator.outputDir == "" || !strings.HasPrefix(filepath.Clean(generator.outputDir), rootDir) {
		t.Fatalf("正式根目录错误: %q", generator.outputDir)
	}
}

func setupMainTimelinePreview(t *testing.T) (api.TimelinePreviewGateway, *tasksvc.WorkerRegistry, *mainTimelineGenerator, string) {
	t.Helper()
	root := t.TempDir()
	db := openMainTimelineDB(t, root)
	if err := db.AutoMigrate(&models.Task{}, &models.MediaFile{}, &models.MediaTimelinePreview{}, &models.CacheAsset{}); err != nil {
		t.Fatalf("迁移时间轴预览测试表失败: %v", err)
	}
	createMainTimelineMedia(t, db, root)
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	generator := &mainTimelineGenerator{}
	gateway, err := newTimelinePreviewGateway(db, tasks, workers, storage.NewService(db, root), root, generator)
	if err != nil {
		t.Fatalf("装配时间轴预览失败: %v", err)
	}
	return gateway, workers, generator, root
}

func openMainTimelineDB(t *testing.T, root string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "timeline-main.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取测试数据库连接失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func createMainTimelineMedia(t *testing.T, db *gorm.DB, root string) {
	t.Helper()
	source := filepath.Join(root, "source.mp4")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatalf("写入测试媒体失败: %v", err)
	}
	media := models.MediaFile{ID: 1, SpaceID: models.DefaultSpaceID, LibraryID: 1, FilePath: source,
		FileName: "source.mp4", FileSize: 6, Duration: 10, FileState: models.MediaFileStateAvailable, ContentHashStale: true}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建测试媒体失败: %v", err)
	}
}

type mainTimelineGenerator struct {
	outputDir string
}

func (g *mainTimelineGenerator) Generate(_ context.Context, request transcoder.TimelinePreviewGenerateRequest) error {
	g.outputDir = request.OutputDir
	if err := os.MkdirAll(request.OutputDir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(request.OutputDir, "index.vtt"), []byte("WEBVTT\n\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(request.OutputDir, "sprite-001.jpg"), []byte("jpeg"), 0o600)
}

func TestSQLiteDataSourceNamesSeparateNormalWALAndDryRunReadOnlyModes(t *testing.T) {
	const normalPath = "jianvideo.db"
	normal := sqliteDataSourceName(normalPath)
	wantNormal := normalPath + "?_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=on"
	if normal != wantNormal {
		t.Fatalf("普通启动 DSN 应保持原 WAL 行为: got=%s want=%s", normal, wantNormal)
	}

	dbPath := filepath.Join(t.TempDir(), "资料 # 100%", "jian video.sqlite")
	dryRun := sqliteReadOnlyDataSourceName(dbPath)
	parsed, err := url.Parse(dryRun)
	if err != nil {
		t.Fatalf("dry-run DSN 应为有效 file URI: %v", err)
	}
	if parsed.Scheme != "file" || parsed.Query().Get("mode") != "ro" {
		t.Fatalf("dry-run DSN 应使用 file URI 只读模式: %s", dryRun)
	}
	if strings.Contains(dryRun, "_journal_mode") {
		t.Fatalf("dry-run DSN 不得设置 journal_mode: %s", dryRun)
	}
	if gotPath := sqliteURIPath(parsed); filepath.Clean(gotPath) != filepath.Clean(dbPath) {
		t.Fatalf("dry-run file URI 未保留特殊字符路径: got=%s want=%s", gotPath, dbPath)
	}
}

func TestSQLiteReadOnlyDataSourceNameUsesAuthorityFreeUNCURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC URI 驱动验证仅适用于 Windows")
	}
	const uncPath = `\\127.0.0.1\jianvideo-missing-share\资料 # 100%\missing.sqlite`
	dsn := sqliteReadOnlyDataSourceName(uncPath)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("UNC dry-run DSN 应为有效 file URI: %v", err)
	}
	if parsed.Host != "" {
		t.Fatalf("UNC file URI 不得包含 authority: host=%s dsn=%s", parsed.Host, dsn)
	}
	if !strings.HasPrefix(parsed.Path, "//127.0.0.1/jianvideo-missing-share/") {
		t.Fatalf("UNC file URI 路径不正确: path=%s dsn=%s", parsed.Path, dsn)
	}
	if parsed.Query().Get("mode") != "ro" {
		t.Fatalf("UNC dry-run DSN 应保持只读模式: %s", dsn)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("创建 UNC SQLite 连接失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = db.PingContext(ctx)
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("UNC SQLite 连接验证超时")
	}
	if err == nil {
		t.Fatal("测试用不存在 UNC 数据库不应成功打开")
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid uri authority") {
		t.Fatalf("go-sqlite3 拒绝了 UNC URI authority: %v", err)
	}
	t.Logf("go-sqlite3 已解析无 authority UNC URI，预期路径打开失败: %v", err)
}

func TestSQLiteReadOnlyDataSourceNameRejectsWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "资料 # 100%")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("创建特殊字符数据库目录失败: %v", err)
	}
	dbPath := filepath.Join(dir, "jian video.sqlite")
	writable, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建只读连接测试数据库失败: %v", err)
	}
	if err := writable.Exec("CREATE TABLE write_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("创建写入探针表失败: %v", err)
	}
	writableSQL, err := writable.DB()
	if err != nil {
		t.Fatalf("获取可写测试连接失败: %v", err)
	}
	if err := writableSQL.Close(); err != nil {
		t.Fatalf("关闭可写测试连接失败: %v", err)
	}

	readOnly, err := gorm.Open(sqlite.Open(sqliteReadOnlyDataSourceName(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("使用只读 file URI 打开数据库失败: %v", err)
	}
	readOnlySQL, err := readOnly.DB()
	if err != nil {
		t.Fatalf("获取只读测试连接失败: %v", err)
	}
	defer func() { _ = readOnlySQL.Close() }()
	if err := readOnly.Exec("INSERT INTO write_probe(id) VALUES (1)").Error; err == nil {
		t.Fatal("通过 dry-run 只读连接写入必须失败")
	}
}

func sqliteURIPath(uri *url.URL) string {
	path := filepath.FromSlash(uri.Path)
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '\\' && path[2] == ':' {
		return path[1:]
	}
	return path
}

// TestSQLiteDataSourceNameAllowsConcurrentWrites 验证并行请求在 WAL 与 busy timeout 下不会直接报 database locked。
func TestSQLiteDataSourceNameAllowsConcurrentWrites(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := gorm.Open(sqlite.Open(sqliteDataSourceName(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取连接池失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE TABLE writes (id INTEGER PRIMARY KEY AUTOINCREMENT, value TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("创建测试表失败: %v", err)
	}

	const workers = 16
	const writesPerWorker = 20
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for index := 0; index < writesPerWorker; index++ {
				if err := db.Exec("INSERT INTO writes(value) VALUES (?)", fmt.Sprintf("%d-%d", worker, index)).Error; err != nil {
					errs <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发写入不应失败: %v", err)
	}
	var count int64
	if err := db.Table("writes").Count(&count).Error; err != nil {
		t.Fatalf("统计写入数量失败: %v", err)
	}
	if count != workers*writesPerWorker {
		t.Fatalf("写入数量不完整: got=%d want=%d", count, workers*writesPerWorker)
	}
}
