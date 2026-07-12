package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func newCacheTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "jianvideo.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&models.Space{},
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.CacheAsset{},
		&models.AuditEvent{},
		&models.Task{},
	); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("创建默认 Space 失败: %v", err)
	}
	taskSvc := tasksvc.NewService(db)
	return NewService(db, dataDir).WithAudit(audit.NewRecorder(db)).WithTasks(taskSvc), db, dataDir
}

func TestThumbnailInventory仅盘点目标Space路径(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	if err := db.Create(&models.Space{ID: "space-other", Name: "其他 Space", OwnerUserID: 1}).Error; err != nil {
		t.Fatalf("创建其他 Space 失败: %v", err)
	}
	ownPath := filepath.Join(dataDir, "thumbnails", models.DefaultSpaceID, "1", "320.jpg")
	otherPath := filepath.Join(dataDir, "thumbnails", "space-other", "2", "320.jpg")
	mustWriteFile(t, ownPath, "own")
	mustWriteFile(t, otherPath, "other")
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID, LibraryID: 11, MediaID: 1, Kind: CacheKindThumbnail,
		Variant: "320", CacheKey: "space-default/1/320", Path: ownPath,
	}); err != nil {
		t.Fatalf("登记已有缩略图失败: %v", err)
	}

	queued, err := svc.Inventory(context.Background(), InventoryInput{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("盘点任务入队失败: %v", err)
	}
	runCacheWorkers(t, svc)
	assertCacheTaskSucceeded(t, svc.tasks, queued.TaskID)

	var assets []models.CacheAsset
	if err := db.Where("kind = ?", CacheKindThumbnail).Order("relative_path ASC").Find(&assets).Error; err != nil {
		t.Fatalf("查询缩略图资产失败: %v", err)
	}
	if len(assets) != 1 || assets[0].SpaceID != models.DefaultSpaceID || strings.Contains(assets[0].RelativePath, "space-other") {
		t.Fatalf("盘点不得跨 Space 登记缩略图: %+v", assets)
	}
	if assets[0].LibraryID != 11 || assets[0].MediaID != 1 || assets[0].Variant != "320" || assets[0].CacheKey == "" {
		t.Fatalf("盘点不得清空已有缩略图关联信息: %+v", assets[0])
	}
}

func TestCacheRegisterRejectsNonWhitelistPaths(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	outside := filepath.Join(t.TempDir(), "x.jpg")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("写外部文件失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID,
		Kind:    CacheKindThumbnail,
		Path:    outside,
	}); err == nil {
		t.Fatal("数据目录外路径不应允许登记")
	}

	dbFile := filepath.Join(dataDir, "jianvideo.db")
	if err := os.WriteFile(dbFile, []byte("db"), 0o600); err != nil {
		t.Fatalf("写数据库占位失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID,
		Kind:    CacheKindThumbnail,
		Path:    dbFile,
	}); err == nil {
		t.Fatal("数据库文件不应允许登记为缓存")
	}
}

func TestCacheRegisterHLSDirectoryAggregatesFiles(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	dir := filepath.Join(dataDir, "hls", "42")
	mustWriteFile(t, filepath.Join(dir, "master.m3u8"), "m")
	mustWriteFile(t, filepath.Join(dir, "480p_segment_000.ts"), "aaaa")
	mustWriteFile(t, filepath.Join(dir, "480p_segment_001.ts"), "bbbb")

	asset, err := svc.RegisterDirectory(context.Background(), RegisterInput{
		SpaceID:   models.DefaultSpaceID,
		MediaID:   42,
		Kind:      CacheKindHLS,
		ProfileID: "h264",
		Path:      dir,
	})
	if err != nil {
		t.Fatalf("登记 HLS 目录失败: %v", err)
	}
	if asset.AssetLevel != CacheAssetLevelDirectory {
		t.Fatalf("HLS 应按目录登记，实际 level=%s", asset.AssetLevel)
	}
	if asset.FileCount != 3 || asset.SizeBytes != 9 {
		t.Fatalf("HLS 聚合统计不符: %+v", asset)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("kind = ?", CacheKindHLS).Count(&count).Error; err != nil {
		t.Fatalf("统计缓存资产失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("HLS segment 不应逐个登记，实际行数 %d", count)
	}
}

func TestCacheSummaryAndDryRunClean(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	thumb := filepath.Join(dataDir, "thumbnails", "a.jpg")
	proxy := filepath.Join(dataDir, "image_cache", "b.jpg")
	source := filepath.Join(t.TempDir(), "source.mp4")
	mustWriteFile(t, thumb, "12345")
	mustWriteFile(t, proxy, "123")
	mustWriteFile(t, source, "source")

	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindThumbnail, Path: thumb}); err != nil {
		t.Fatalf("登记缩略图失败: %v", err)
	}
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindImageProxy, Path: proxy}); err != nil {
		t.Fatalf("登记图片代理失败: %v", err)
	}

	summary, err := svc.Summary(context.Background(), SummaryQuery{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("查询缓存统计失败: %v", err)
	}
	if summary.TotalSizeBytes != 8 || summary.ByKind[CacheKindThumbnail].SizeBytes != 5 || summary.ByKind[CacheKindImageProxy].SizeBytes != 3 {
		t.Fatalf("缓存统计聚合不符: %+v", summary)
	}

	preview, err := svc.Clean(context.Background(), CleanInput{SpaceID: models.DefaultSpaceID, Kinds: []string{CacheKindThumbnail}, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run 清理失败: %v", err)
	}
	if !preview.DryRun || preview.CandidateCount != 1 || preview.TotalSizeBytes != 5 {
		t.Fatalf("dry-run 影响范围不符: %+v", preview)
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("dry-run 不应删除缩略图: %v", err)
	}

	result, err := svc.Clean(context.Background(), CleanInput{SpaceID: models.DefaultSpaceID, Kinds: []string{CacheKindThumbnail}})
	if err != nil {
		t.Fatalf("清理任务入队失败: %v", err)
	}
	if result.TaskID == 0 || result.DeletedCount != 0 || result.DeletedSizeBytes != 0 {
		t.Fatalf("真实清理请求应只返回任务 ID: %+v", result)
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("worker 执行前不应删除缩略图: %v", err)
	}
	runCacheWorkers(t, svc)
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Fatalf("worker 执行后缩略图应被删除，stat err=%v", err)
	}
	assertCacheTaskSucceeded(t, svc.tasks, result.TaskID)
	if _, err := os.Stat(proxy); err != nil {
		t.Fatalf("非目标 kind 不应被删除: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("原媒体不应被删除: %v", err)
	}
	mustWriteFile(t, thumb, "123456")
	rebuilt, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindThumbnail, Path: thumb})
	if err != nil {
		t.Fatalf("清理后应允许重建登记: %v", err)
	}
	if rebuilt.SizeBytes != 6 {
		t.Fatalf("重建登记尺寸不符: %+v", rebuilt)
	}
}

func TestCacheInventoryMarksMissingAndDiscoversWhitelistFiles(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	missing := filepath.Join(dataDir, "thumbnails", "missing.jpg")
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{SpaceID: models.DefaultSpaceID, Kind: CacheKindThumbnail, Path: missing}); err == nil {
		t.Fatal("不存在文件不能登记")
	}
	existing := filepath.Join(dataDir, "thumbnails", "existing.jpg")
	mustWriteFile(t, existing, "abc")
	first, err := svc.Inventory(context.Background(), InventoryInput{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("盘点任务入队失败: %v", err)
	}
	if first.TaskID == 0 {
		t.Fatal("盘点任务应返回任务 ID")
	}
	var before int64
	if err := db.Model(&models.CacheAsset{}).Count(&before).Error; err != nil {
		t.Fatalf("统计 worker 前缓存资产失败: %v", err)
	}
	if before != 0 {
		t.Fatalf("worker 执行前不应盘点缓存，实际资产数 %d", before)
	}
	runCacheWorkers(t, svc)
	assertCacheTaskSucceeded(t, svc.tasks, first.TaskID)
	var asset models.CacheAsset
	if err := db.Where("kind = ? AND relative_path = ?", CacheKindThumbnail, filepath.ToSlash(filepath.Join("thumbnails", "existing.jpg"))).First(&asset).Error; err != nil {
		t.Fatalf("盘点应发现白名单文件: %v", err)
	}
	if asset.SizeBytes != 3 || asset.MissingAt != nil {
		t.Fatalf("盘点登记字段异常: %+v", asset)
	}

	if err := os.Remove(existing); err != nil {
		t.Fatalf("删除缓存文件失败: %v", err)
	}
	second, err := svc.Inventory(context.Background(), InventoryInput{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("二次盘点任务入队失败: %v", err)
	}
	runCacheWorkers(t, svc)
	assertCacheTaskSucceeded(t, svc.tasks, second.TaskID)
	if err := db.First(&asset, asset.ID).Error; err != nil {
		t.Fatalf("读取盘点资产失败: %v", err)
	}
	if asset.MissingAt == nil {
		t.Fatalf("磁盘缺失资产应标记 missing_at: %+v", asset)
	}
}

func TestCacheWorkersRejectInvalidTaskPayload(t *testing.T) {
	svc, _, dataDir := newCacheTestService(t)
	thumb := filepath.Join(dataDir, "thumbnails", "safe.jpg")
	mustWriteFile(t, thumb, "safe")
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID,
		Kind:    CacheKindThumbnail,
		Path:    thumb,
	}); err != nil {
		t.Fatalf("登记测试缓存失败: %v", err)
	}

	mismatchedSpace, err := json.Marshal(map[string]any{
		"space_id": "space-other",
		"kinds":    []string{CacheKindThumbnail},
	})
	if err != nil {
		t.Fatalf("编码跨 Space payload 失败: %v", err)
	}
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "损坏 JSON", payload: `{"space_id":`, want: "payload"},
		{name: "跨 Space", payload: string(mismatchedSpace), want: "Space"},
		{name: "非法类型", payload: `{"space_id":"space-default","kinds":["database"]}`, want: "缓存类型无效"},
	}
	ids := make(map[string]int64, len(cases))
	for _, tc := range cases {
		task, err := svc.tasks.Enqueue(context.Background(), tasksvc.EnqueueInput{
			Scope:        models.TaskScopeSpace,
			SpaceID:      models.DefaultSpaceID,
			Type:         TaskTypeCacheClean,
			MaxAttempts:  1,
			PayloadJSON:  tc.payload,
			ResourceType: "cache",
			ResourceID:   "clean",
		})
		if err != nil {
			t.Fatalf("%s 任务入队失败: %v", tc.name, err)
		}
		ids[tc.name] = task.ID
	}

	runCacheWorkers(t, svc)
	for _, tc := range cases {
		task, err := svc.tasks.Get(context.Background(), ids[tc.name], tasksvc.Query{SpaceID: models.DefaultSpaceID})
		if err != nil {
			t.Fatalf("读取 %s 任务失败: %v", tc.name, err)
		}
		if task.Status != models.TaskStatusFailed || !strings.Contains(task.Error, tc.want) {
			t.Fatalf("%s 应被 worker 二次校验拒绝: %+v", tc.name, task)
		}
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("非法任务不得删除白名单内文件: %v", err)
	}
}

func TestCacheCleanRetryIsIdempotent(t *testing.T) {
	svc, db, dataDir := newCacheTestService(t)
	base := time.Date(2026, 7, 9, 11, 0, 0, 0, time.UTC)
	svc.tasks.SetNowForTest(func() time.Time { return base })
	recorder := &failOnceAuditRecorder{
		Recorder: svc.audit,
		action:   "cache.clean.executed",
		err:      errors.New("模拟审计暂时失败"),
	}
	svc.WithAudit(recorder)
	thumb := filepath.Join(dataDir, "thumbnails", "retry.jpg")
	mustWriteFile(t, thumb, "retry")
	if _, err := svc.RegisterFile(context.Background(), RegisterInput{
		SpaceID: models.DefaultSpaceID,
		Kind:    CacheKindThumbnail,
		Path:    thumb,
	}); err != nil {
		t.Fatalf("登记重试缓存失败: %v", err)
	}

	queued, err := svc.Clean(context.Background(), CleanInput{
		SpaceID: models.DefaultSpaceID,
		Kinds:   []string{CacheKindThumbnail},
	})
	if err != nil {
		t.Fatalf("清理任务入队失败: %v", err)
	}
	runCacheWorkers(t, svc)
	pending, err := svc.tasks.Get(context.Background(), queued.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("读取待重试任务失败: %v", err)
	}
	if pending.Status != models.TaskStatusPending || pending.Attempts != 1 || pending.NextRunAt == nil {
		t.Fatalf("首次暂时失败应进入退避重试: %+v", pending)
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Fatalf("首次执行已删除的文件不应恢复，stat err=%v", err)
	}
	var assets int64
	if err := db.Model(&models.CacheAsset{}).Where("relative_path = ?", filepath.ToSlash(filepath.Join("thumbnails", "retry.jpg"))).Count(&assets).Error; err != nil {
		t.Fatalf("统计重试缓存资产失败: %v", err)
	}
	if assets != 0 {
		t.Fatalf("首次执行后缓存资产应已删除，实际 %d", assets)
	}

	svc.tasks.SetNowForTest(func() time.Time { return base.Add(2 * time.Second) })
	runCacheWorkers(t, svc)
	assertCacheTaskSucceeded(t, svc.tasks, queued.TaskID)
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).Where("action = ?", "cache.clean.executed").Count(&auditCount).Error; err != nil {
		t.Fatalf("统计清理审计失败: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("幂等重试最终应只落一条成功清理审计，实际 %d", auditCount)
	}
}

type failOnceAuditRecorder struct {
	audit.Recorder
	action string
	err    error
	failed bool
}

func (r *failOnceAuditRecorder) Record(ctx context.Context, input audit.EventInput) error {
	if input.Action == r.action && !r.failed {
		r.failed = true
		return r.err
	}
	return r.Recorder.Record(ctx, input)
}

func runCacheWorkers(t *testing.T, svc *Service) {
	t.Helper()
	registry := tasksvc.NewWorkerRegistry(svc.tasks)
	if err := svc.RegisterWorkers(registry); err != nil {
		t.Fatalf("注册缓存 worker 失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("运行缓存 worker 失败: %v", err)
	}
}

func assertCacheTaskSucceeded(t *testing.T, tasks *tasksvc.Service, taskID int64) {
	t.Helper()
	task, err := tasks.Get(context.Background(), taskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("读取缓存任务失败: %v", err)
	}
	if task.Status != models.TaskStatusSucceeded || task.Progress != 100 || task.FinishedAt == nil {
		t.Fatalf("缓存任务终态不符: %+v", task)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}
