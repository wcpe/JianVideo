package thumbnail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupCoverService(t *testing.T) (*Service, *gorm.DB, *tasksvc.Service, *tasksvc.WorkerRegistry, *storage.Service) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "covers.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.Task{},
		&models.CacheAsset{}, &models.MediaCover{}, &models.CoverCandidate{}, &models.AuditEvent{},
	); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	for _, spaceID := range []string{"space-a", "space-b"} {
		if err := db.Create(&models.Space{ID: spaceID, Name: spaceID, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建测试 Space 失败: %v", err)
		}
	}
	recorder := audit.NewRecorder(db)
	tasks := tasksvc.NewService(db).WithAudit(recorder)
	cache := storage.NewService(db, dataDir).WithTasks(tasks)
	service := NewService(library.NewService(db), tasks, cache, dataDir).WithAudit(recorder)
	service.SetCoverGeneratorForTest(func(ctx context.Context, _ models.MediaFile, timestamp float64, outputPath string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte(fmt.Sprintf("%.3f", timestamp)), 0o640)
	})
	registry := tasksvc.NewWorkerRegistry(tasks)
	if err := service.RegisterWorkers(registry, 2); err != nil {
		t.Fatalf("注册封面 worker 失败: %v", err)
	}
	if err := cache.RegisterWorkers(registry); err != nil {
		t.Fatalf("注册缓存 worker 失败: %v", err)
	}
	return service, db, tasks, registry, cache
}

func createCoverMedia(t *testing.T, db *gorm.DB, spaceID, name string, duration float64) models.MediaFile {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("source"), 0o640); err != nil {
		t.Fatalf("写入源媒体失败: %v", err)
	}
	libraryPath := models.LibraryPath{SpaceID: spaceID, Path: root, Type: "local", Enabled: 1}
	if err := db.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media := models.MediaFile{
		SpaceID: spaceID, LibraryID: libraryPath.ID, FilePath: path, FileName: name,
		Format: filepath.Ext(name)[1:], Duration: duration, FileSize: 6,
		ModifiedAt: time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC), FileState: models.MediaFileStateAvailable,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return media
}

func TestCoverCandidateTimestamps短视频去重并避开越界(t *testing.T) {
	if got := CoverCandidateTimestamps(100); !equalFloat64s(got, []float64{10, 30, 50, 70, 90}) {
		t.Fatalf("长视频候选时间点错误: %v", got)
	}
	short := CoverCandidateTimestamps(1.2)
	if len(short) < 2 || len(short) > 5 {
		t.Fatalf("短视频应生成去重后的多个候选: %v", short)
	}
	seen := map[float64]bool{}
	for _, timestamp := range short {
		if timestamp < 0 || timestamp >= 1.2 || seen[timestamp] {
			t.Fatalf("短视频候选时间点无效或重复: %v", short)
		}
		seen[timestamp] = true
	}
}

func TestCoverGenerate幂等且隔离Space媒体与候选(t *testing.T) {
	service, db, _, registry, _ := setupCoverService(t)
	mediaA := createCoverMedia(t, db, "space-a", "a.mp4", 10)
	mediaB := createCoverMedia(t, db, "space-b", "b.mp4", 10)

	first, err := service.GenerateCovers(context.Background(), "space-a", mediaA.ID, false)
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	duplicate, err := service.GenerateCovers(context.Background(), "space-a", mediaA.ID, false)
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	other, err := service.GenerateCovers(context.Background(), "space-b", mediaB.ID, false)
	if err != nil {
		t.Fatalf("另一 Space 入队失败: %v", err)
	}
	if first.TaskID == 0 || duplicate.TaskID != first.TaskID || other.TaskID == first.TaskID {
		t.Fatalf("封面任务幂等或 Space 隔离错误: first=%d duplicate=%d other=%d", first.TaskID, duplicate.TaskID, other.TaskID)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行封面任务失败: %v", err)
	}

	covers, err := service.ListCovers(context.Background(), "space-a", mediaA.ID)
	if err != nil {
		t.Fatalf("查询封面失败: %v", err)
	}
	if len(covers.Candidates) != 5 || covers.Cover == nil || covers.Cover.SelectedAssetID == 0 {
		t.Fatalf("自动封面结果不完整: %+v", covers)
	}
	for _, candidate := range covers.Candidates {
		if candidate.SpaceID != "space-a" || candidate.MediaID != mediaA.ID || candidate.AssetID == 0 || candidate.Fingerprint == "" {
			t.Fatalf("候选隔离或缓存关联错误: %+v", candidate)
		}
	}
}

func TestCover人工选择持久化并在缓存清理后按指纹恢复(t *testing.T) {
	service, db, _, registry, cache := setupCoverService(t)
	media := createCoverMedia(t, db, "space-a", "manual.mp4", 20)
	queued, err := service.GenerateCovers(context.Background(), "space-a", media.ID, false)
	if err != nil || queued.TaskID == 0 {
		t.Fatalf("封面生成入队失败: %+v err=%v", queued, err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行封面生成失败: %v", err)
	}
	initial, err := service.ListCovers(context.Background(), "space-a", media.ID)
	if err != nil || len(initial.Candidates) < 3 {
		t.Fatalf("读取初始候选失败: %+v err=%v", initial, err)
	}
	chosen := initial.Candidates[2]
	selected, err := service.SelectCover(context.Background(), "space-a", media.ID, chosen.ID)
	if err != nil {
		t.Fatalf("人工选择失败: %v", err)
	}
	oldAssetID := selected.SelectedAssetID
	if !selected.Manual || selected.SelectedSource != CoverSourceVideoFrame || selected.SelectedTimestampSeconds != chosen.TimestampSeconds || selected.SelectedFingerprint != chosen.Fingerprint {
		t.Fatalf("人工选择语义未完整持久化: %+v", selected)
	}

	cleaned, err := cache.Clean(context.Background(), storage.CleanInput{SpaceID: "space-a", Kinds: []string{storage.CacheKindCover}})
	if err != nil || cleaned.TaskID == 0 {
		t.Fatalf("封面缓存清理入队失败: %+v err=%v", cleaned, err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行封面缓存清理失败: %v", err)
	}
	stale, err := service.ListCovers(context.Background(), "space-a", media.ID)
	if err != nil {
		t.Fatalf("清理后查询失败: %v", err)
	}
	if stale.Cover == nil || !stale.Cover.Manual || stale.Cover.SelectedFingerprint != chosen.Fingerprint || stale.Cover.SelectedAssetID != oldAssetID {
		t.Fatalf("缓存清理不应丢失人工语义: %+v", stale.Cover)
	}
	if len(stale.Candidates) != len(initial.Candidates) {
		t.Fatalf("缓存清理不应删除候选语义记录: before=%d after=%d", len(initial.Candidates), len(stale.Candidates))
	}

	rebuild, err := service.GenerateCovers(context.Background(), "space-a", media.ID, false)
	if err != nil || rebuild.TaskID == 0 {
		t.Fatalf("清理后重建入队失败: %+v err=%v", rebuild, err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行重建失败: %v", err)
	}
	restored, err := service.ListCovers(context.Background(), "space-a", media.ID)
	if err != nil {
		t.Fatalf("重建后查询失败: %v", err)
	}
	if restored.Cover == nil || !restored.Cover.Manual || restored.Cover.SelectedFingerprint != chosen.Fingerprint || restored.Cover.SelectedAssetID == 0 || restored.Cover.SelectedAssetID == oldAssetID {
		t.Fatalf("重建未按指纹恢复新缓存资产: old=%d cover=%+v", oldAssetID, restored.Cover)
	}
}

func TestCoverRefresh源视频失效时不静默换帧(t *testing.T) {
	service, db, tasks, registry, _ := setupCoverService(t)
	media := createCoverMedia(t, db, "space-a", "missing.mp4", 12)
	queued, _ := service.GenerateCovers(context.Background(), "space-a", media.ID, false)
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	initial, _ := service.ListCovers(context.Background(), "space-a", media.ID)
	chosen := initial.Candidates[len(initial.Candidates)-1]
	selected, err := service.SelectCover(context.Background(), "space-a", media.ID, chosen.ID)
	if err != nil {
		t.Fatalf("人工选择失败: %v", err)
	}
	if err := os.Remove(media.FilePath); err != nil {
		t.Fatalf("删除源视频失败: %v", err)
	}
	refresh, err := service.GenerateCovers(context.Background(), "space-a", media.ID, true)
	if err != nil {
		t.Fatalf("刷新入队失败: %v", err)
	}
	if refresh.TaskID == queued.TaskID {
		t.Fatal("refresh 必须使用独立任务")
	}
	for attempt := 0; attempt < coverMaxAttempts; attempt++ {
		if err := db.Model(&models.Task{}).Where("id = ?", refresh.TaskID).Update("next_run_at", nil).Error; err != nil {
			t.Fatalf("推进重试时间失败: %v", err)
		}
		if err := registry.RunPending(context.Background()); err != nil {
			t.Fatalf("worker 调度失败: %v", err)
		}
	}
	task, err := tasks.Get(context.Background(), refresh.TaskID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil || task.Status != models.TaskStatusFailed {
		t.Fatalf("源失效刷新耗尽重试后应失败: task=%+v err=%v", task, err)
	}
	after, _ := service.ListCovers(context.Background(), "space-a", media.ID)
	if after.Cover == nil || after.Cover.SelectedFingerprint != selected.SelectedFingerprint || !after.Cover.Manual {
		t.Fatalf("源失效不得静默换帧: before=%+v after=%+v", selected, after.Cover)
	}
}

func TestCover任务支持取消后重试(t *testing.T) {
	service, db, tasks, registry, _ := setupCoverService(t)
	media := createCoverMedia(t, db, "space-a", "cancel.mp4", 10)
	started := make(chan struct{})
	var calls atomic.Int32
	service.SetCoverGeneratorForTest(func(ctx context.Context, _ models.MediaFile, _ float64, outputPath string) error {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("retry"), 0o640)
	})
	queued, err := service.GenerateCovers(context.Background(), "space-a", media.ID, false)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- registry.RunPending(context.Background()) }()
	<-started
	if err := tasks.Cancel(context.Background(), queued.TaskID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("取消任务失败: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("取消后的 worker 不应报错: %v", err)
	}
	if err := tasks.Retry(context.Background(), queued.TaskID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("重试任务失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("重试执行失败: %v", err)
	}
	final, _ := tasks.Get(context.Background(), queued.TaskID, tasksvc.Query{SpaceID: "space-a"})
	if final.Status != models.TaskStatusSucceeded {
		t.Fatalf("重试后任务应成功: %+v", final)
	}
}

func TestCover生成与选择写审计(t *testing.T) {
	service, db, _, registry, _ := setupCoverService(t)
	media := createCoverMedia(t, db, "space-a", "audit.mp4", 10)
	_, _ = service.GenerateCovers(context.Background(), "space-a", media.ID, false)
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	result, _ := service.ListCovers(context.Background(), "space-a", media.ID)
	if _, err := service.SelectCover(context.Background(), "space-a", media.ID, result.Candidates[0].ID); err != nil {
		t.Fatalf("选择失败: %v", err)
	}
	var actions []string
	if err := db.Model(&models.AuditEvent{}).Where("resource_type = ? AND resource_id = ?", "media", media.ID).Order("id ASC").Pluck("action", &actions).Error; err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if !containsString(actions, "cover.generated") || !containsString(actions, "cover.selected") {
		t.Fatalf("封面审计事件不完整: %v", actions)
	}
}

func TestCover候选与选择拒绝跨Space和跨媒体(t *testing.T) {
	service, db, _, registry, _ := setupCoverService(t)
	mediaA := createCoverMedia(t, db, "space-a", "a.mp4", 10)
	mediaB := createCoverMedia(t, db, "space-a", "b.mp4", 10)
	_, _ = service.GenerateCovers(context.Background(), "space-a", mediaA.ID, false)
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	result, _ := service.ListCovers(context.Background(), "space-a", mediaA.ID)
	candidateID := result.Candidates[0].ID
	if _, err := service.SelectCover(context.Background(), "space-b", mediaA.ID, candidateID); err == nil {
		t.Fatal("跨 Space 选择必须失败")
	}
	if _, err := service.SelectCover(context.Background(), "space-a", mediaB.ID, candidateID); err == nil {
		t.Fatal("跨媒体选择必须失败")
	}
}

func equalFloat64s(left, right []float64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
