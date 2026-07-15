package transcoder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

type timelinePreviewFakeGenerator struct {
	mu       sync.Mutex
	requests []TimelinePreviewGenerateRequest
	err      error
	started  chan struct{}
	release  chan struct{}
	delay    time.Duration
	active   int
	peak     int
}

func (g *timelinePreviewFakeGenerator) Generate(ctx context.Context, request TimelinePreviewGenerateRequest) error {
	g.recordStart(request)
	defer g.recordFinish()
	if err := g.wait(ctx); err != nil {
		return err
	}
	if g.err != nil {
		return g.err
	}
	if err := os.MkdirAll(request.OutputDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(request.OutputDir, "index.vtt"), []byte("WEBVTT\n\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(request.OutputDir, "sprite-001.jpg"), []byte("jpeg"), 0o600)
}

func (g *timelinePreviewFakeGenerator) wait(ctx context.Context) error {
	if g.started != nil {
		select {
		case g.started <- struct{}{}:
		default:
		}
	}
	if g.release != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.release:
		}
	}
	if g.delay <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(g.delay):
		return nil
	}
}

func (g *timelinePreviewFakeGenerator) recordStart(request TimelinePreviewGenerateRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, request)
	g.active++
	if g.active > g.peak {
		g.peak = g.active
	}
}

func (g *timelinePreviewFakeGenerator) recordFinish() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
}

func (g *timelinePreviewFakeGenerator) peakConcurrency() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

func newTimelinePreviewTestService(t *testing.T, generator TimelinePreviewGenerator) (*TimelinePreviewService, *tasksvc.Service, *tasksvc.WorkerRegistry, *gorm.DB, string, models.MediaFile) {
	t.Helper()
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "timeline.db")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开时间轴预览测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Task{}, &models.MediaFile{}, &models.MediaTimelinePreview{}, &models.CacheAsset{}); err != nil {
		t.Fatalf("迁移时间轴预览测试表失败: %v", err)
	}
	source := filepath.Join(root, "source.mp4")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatalf("写入测试媒体失败: %v", err)
	}
	media := models.MediaFile{ID: 42, SpaceID: models.DefaultSpaceID, LibraryID: 7, FilePath: source, FileName: "source.mp4", FileSize: 6, Duration: 20, ModifiedAt: time.Now(), FileState: models.MediaFileStateAvailable, ContentHashStale: true}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建测试媒体失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	cache := storage.NewService(db, root)
	service := NewTimelinePreviewService(db, tasks, workers, cache, root, generator)
	if err := service.RegisterWorker(); err != nil {
		t.Fatalf("注册时间轴预览 worker 失败: %v", err)
	}
	return service, tasks, workers, db, root, media
}

func TestTimelinePreviewWorker生成峰值并发为一(t *testing.T) {
	generator := &timelinePreviewFakeGenerator{delay: 100 * time.Millisecond}
	service, _, workers, _, _, _ := newTimelinePreviewTestService(t, generator)
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	for index := 0; index < 2; index++ {
		if _, err := service.Rebuild(context.Background(), identity); err != nil {
			t.Fatalf("第 %d 个重建任务入队失败: %v", index+1, err)
		}
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("真实 worker 执行失败: %v", err)
	}
	if peak := generator.peakConcurrency(); peak != 1 {
		t.Fatalf("时间轴预览 generation 峰值并发必须为 1，实际 %d", peak)
	}
}

func TestTimelinePreviewStatus返回时长版本与受控Sprite文件名(t *testing.T) {
	service, _, workers, _, root, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	pending, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if pending.Duration != 20 || pending.Version != DefaultTimelinePreviewProfile().Version {
		t.Fatalf("pending 应返回时长和版本: %+v", pending)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行任务失败: %v", err)
	}
	payload := timelinePayloadFromStatus(pending)
	dir := TimelinePreviewGenerationPath(root, payload)
	if err := os.WriteFile(filepath.Join(dir, "sprite-002.jpg"), []byte("jpeg"), 0o600); err != nil {
		t.Fatalf("写入第二张 sprite 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private.txt"), []byte(root), 0o600); err != nil {
		t.Fatalf("写入非资源文件失败: %v", err)
	}
	status, err := service.Status(context.Background(), identity)
	if err != nil {
		t.Fatalf("查询可用状态失败: %v", err)
	}
	if status.Duration != 20 || status.Version != 1 || len(status.SpriteNames) != 2 || status.SpriteNames[0] != "sprite-001.jpg" || status.SpriteNames[1] != "sprite-002.jpg" {
		t.Fatalf("可用状态元数据错误: %+v", status)
	}
	for _, name := range status.SpriteNames {
		if filepath.IsAbs(name) || strings.ContainsAny(name, `\\/`) || strings.Contains(name, root) {
			t.Fatalf("状态不得暴露绝对路径: %q", name)
		}
	}
}

func TestTimelinePreviewEnqueue普通请求复用未完成GenerationForce每次换代(t *testing.T) {
	service, _, _, _, _, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	first, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	second, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	if first.TaskID != second.TaskID || first.GenerationID != second.GenerationID {
		t.Fatalf("普通请求应复用未完成 generation: first=%+v second=%+v", first, second)
	}
	forcedA, err := service.Rebuild(context.Background(), identity)
	if err != nil {
		t.Fatalf("首次强制重建失败: %v", err)
	}
	forcedB, err := service.Rebuild(context.Background(), identity)
	if err != nil {
		t.Fatalf("再次强制重建失败: %v", err)
	}
	if forcedA.TaskID == forcedB.TaskID || forcedA.GenerationID == forcedB.GenerationID || forcedA.GenerationID == first.GenerationID {
		t.Fatalf("force 每次必须创建新 generation: first=%+v a=%+v b=%+v", first, forcedA, forcedB)
	}
}

func TestTimelinePreviewStatus仅返回当前指针或未完成任务(t *testing.T) {
	service, tasks, workers, _, _, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	pending, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	status, err := service.Status(context.Background(), identity)
	if err != nil || status.State != TimelinePreviewPending || status.TaskID != pending.TaskID {
		t.Fatalf("未完成任务状态错误: status=%+v err=%v", status, err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("运行时间轴预览任务失败: %v", err)
	}
	status, err = service.Status(context.Background(), identity)
	if err != nil || status.State != TimelinePreviewAvailable || status.TaskID != 0 {
		t.Fatalf("成功后应返回当前指针: status=%+v err=%v", status, err)
	}
	task, err := tasks.Get(context.Background(), pending.TaskID, tasksvc.Query{SpaceID: models.DefaultSpaceID})
	if err != nil || task.Status != models.TaskStatusSucceeded {
		t.Fatalf("任务应成功: task=%+v err=%v", task, err)
	}
}

func TestTimelinePreview任务完成登记Cache并事务切换指针(t *testing.T) {
	service, _, workers, db, root, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	pending, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行任务失败: %v", err)
	}
	var pointer models.MediaTimelinePreview
	if err := db.Where("space_id = ? AND media_id = ? AND profile_id = ?", identity.SpaceID, identity.MediaID, pending.ProfileID).First(&pointer).Error; err != nil {
		t.Fatalf("读取当前指针失败: %v", err)
	}
	if pointer.GenerationID != pending.GenerationID || pointer.SourceFingerprint != pending.SourceFingerprint || pointer.AssetID <= 0 {
		t.Fatalf("当前指针错误: %+v", pointer)
	}
	var asset models.CacheAsset
	if err := db.First(&asset, pointer.AssetID).Error; err != nil {
		t.Fatalf("读取缓存资产失败: %v", err)
	}
	if asset.Kind != storage.CacheKindTimelinePreview || asset.CacheKey != TimelinePreviewCacheKey(TimelinePreviewPayload{SpaceID: identity.SpaceID, MediaID: identity.MediaID, ProfileID: pending.ProfileID, SourceFingerprint: pending.SourceFingerprint, GenerationID: pending.GenerationID}) {
		t.Fatalf("缓存登记错误: %+v", asset)
	}
	if _, err := os.Stat(filepath.Join(root, asset.RelativePath, "index.vtt")); err != nil {
		t.Fatalf("已登记 generation 产物不存在: %v", err)
	}
}

func TestTimelinePreview媒体指纹变化时不切指针并保留旧指针(t *testing.T) {
	generator := &timelinePreviewFakeGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	service, _, workers, db, root, _ := newTimelinePreviewTestService(t, generator)
	oldPayload := TimelinePreviewPayload{SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: DefaultTimelinePreviewProfile().ID, SourceFingerprint: "old-source", GenerationID: "old-generation"}
	oldDir := TimelinePreviewGenerationPath(root, oldPayload)
	if err := (&timelinePreviewFakeGenerator{}).Generate(context.Background(), TimelinePreviewGenerateRequest{OutputDir: oldDir}); err != nil {
		t.Fatalf("准备旧 generation 失败: %v", err)
	}
	oldRelative, err := filepath.Rel(root, oldDir)
	if err != nil {
		t.Fatalf("计算旧资产相对路径失败: %v", err)
	}
	oldAsset := models.CacheAsset{SpaceID: models.DefaultSpaceID, MediaID: 42, Kind: storage.CacheKindTimelinePreview, AssetLevel: storage.CacheAssetLevelDirectory, ProfileID: oldPayload.ProfileID, Variant: "old-source:old-generation", CacheKey: TimelinePreviewCacheKey(oldPayload), RelativePath: filepath.ToSlash(oldRelative), Rebuildable: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&oldAsset).Error; err != nil {
		t.Fatalf("创建旧资产失败: %v", err)
	}
	oldPointer := models.MediaTimelinePreview{SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: DefaultTimelinePreviewProfile().ID, SourceFingerprint: "old-source", GenerationID: "old-generation", AssetID: oldAsset.ID, UpdatedAt: time.Now()}
	if err := db.Create(&oldPointer).Error; err != nil {
		t.Fatalf("创建旧指针失败: %v", err)
	}
	pending, err := service.Enqueue(context.Background(), TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- workers.RunPending(context.Background()) }()
	<-generator.started
	source := filepath.Join(root, "source.mp4")
	if err := os.WriteFile(source, []byte("source-changed"), 0o600); err != nil {
		t.Fatalf("修改源文件失败: %v", err)
	}
	close(generator.release)
	if err := <-done; err != nil {
		t.Fatalf("worker 执行失败: %v", err)
	}
	var got models.MediaTimelinePreview
	if err := db.First(&got, oldPointer.ID).Error; err != nil {
		t.Fatalf("读取旧指针失败: %v", err)
	}
	if got.GenerationID != "old-generation" || got.AssetID != oldAsset.ID {
		t.Fatalf("源指纹变化时必须保留旧指针: %+v", got)
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("源指纹变化时必须保留旧 generation: %v", err)
	}
	var task models.Task
	if err := db.First(&task, pending.TaskID).Error; err != nil || task.Status != models.TaskStatusPending || task.Attempts != 1 {
		t.Fatalf("指纹变化任务应进入重试等待: task=%+v err=%v", task, err)
	}
	assertTimelineGenerationRemoved(t, db, root, pending)
}

func TestTimelinePreview生成失败保留旧指针(t *testing.T) {
	generator := &timelinePreviewFakeGenerator{err: errors.New("模拟生成失败")}
	service, _, workers, db, _, _ := newTimelinePreviewTestService(t, generator)
	pointer := models.MediaTimelinePreview{SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: DefaultTimelinePreviewProfile().ID, SourceFingerprint: "old-source", GenerationID: "old-generation", AssetID: 99, UpdatedAt: time.Now()}
	if err := db.Create(&pointer).Error; err != nil {
		t.Fatalf("创建旧指针失败: %v", err)
	}
	if _, err := service.Rebuild(context.Background(), TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}); err != nil {
		t.Fatalf("重建入队失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("worker 返回异常: %v", err)
	}
	var got models.MediaTimelinePreview
	if err := db.First(&got, pointer.ID).Error; err != nil || got.GenerationID != "old-generation" || got.AssetID != 99 {
		t.Fatalf("失败时旧指针应保留: got=%+v err=%v", got, err)
	}
}

func TestTimelinePreviewOpenResource校验完整身份(t *testing.T) {
	service, _, workers, _, _, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	pending, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行任务失败: %v", err)
	}
	resource, err := service.OpenResource(context.Background(), TimelinePreviewResourceIdentity{TimelinePreviewIdentity: identity, GenerationID: pending.GenerationID, SourceFingerprint: pending.SourceFingerprint, ResourceName: "index.vtt"})
	if err != nil {
		t.Fatalf("打开 VTT 失败: %v", err)
	}
	defer func() { _ = resource.Body.Close() }()
	if resource.ContentType != "text/vtt; charset=utf-8" || resource.Size <= 0 {
		t.Fatalf("VTT 资源元数据错误: %+v", resource)
	}
	if _, err := io.ReadAll(resource.Body); err != nil {
		t.Fatalf("读取 VTT 失败: %v", err)
	}
	_, err = service.OpenResource(context.Background(), TimelinePreviewResourceIdentity{TimelinePreviewIdentity: identity, GenerationID: "wrong-generation", SourceFingerprint: pending.SourceFingerprint, ResourceName: "index.vtt"})
	if !errors.Is(err, ErrTimelinePreviewNotFound) {
		t.Fatalf("跨 generation 读取应返回不存在: %v", err)
	}
	_, err = service.OpenResource(context.Background(), TimelinePreviewResourceIdentity{TimelinePreviewIdentity: identity, GenerationID: pending.GenerationID, SourceFingerprint: pending.SourceFingerprint, ResourceName: "../source.mp4"})
	if !errors.Is(err, ErrTimelinePreviewInvalid) {
		t.Fatalf("非法资源名应被拒绝: %v", err)
	}
}

func TestTimelinePreview两个服务实例并发普通入队只创建一个Generation(t *testing.T) {
	service, _, _, db, root, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	secondTasks := tasksvc.NewService(db)
	second := NewTimelinePreviewService(db, secondTasks, tasksvc.NewWorkerRegistry(secondTasks), storage.NewService(db, root), root, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	start := make(chan struct{})
	results := make(chan TimelinePreviewStatus, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, target := range []*TimelinePreviewService{service, second} {
		wg.Add(1)
		go func(target *TimelinePreviewService) {
			defer wg.Done()
			<-start
			status, err := target.Enqueue(context.Background(), identity)
			results <- status
			errs <- err
		}(target)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发入队失败: %v", err)
		}
	}
	first := <-results
	secondResult := <-results
	if first.TaskID != secondResult.TaskID || first.GenerationID != secondResult.GenerationID {
		t.Fatalf("两个实例未复用 generation: first=%+v second=%+v", first, secondResult)
	}
	var count int64
	if err := db.Model(&models.Task{}).Where("type = ?", TaskTypeTimelinePreviewGenerate).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("并发普通请求应只创建一个任务: count=%d err=%v", count, err)
	}
}

func TestTimelinePreview两个Force反序完成旧任务不残留目录或缓存(t *testing.T) {
	service, _, _, db, root, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	first, err := service.Rebuild(context.Background(), identity)
	if err != nil {
		t.Fatalf("首次重建入队失败: %v", err)
	}
	latest, err := service.Rebuild(context.Background(), identity)
	if err != nil {
		t.Fatalf("最新重建入队失败: %v", err)
	}
	latestTask := loadRunningTimelineTask(t, db, latest.TaskID)
	firstTask := loadRunningTimelineTask(t, db, first.TaskID)
	if err := service.handleTask(context.Background(), latestTask); err != nil {
		t.Fatalf("最新重建完成失败: %v", err)
	}
	if err := service.handleTask(context.Background(), firstTask); err != nil {
		t.Fatalf("旧重建完成失败: %v", err)
	}
	var pointer models.MediaTimelinePreview
	if err := db.Where("space_id = ? AND media_id = ? AND profile_id = ?", identity.SpaceID, identity.MediaID, latest.ProfileID).First(&pointer).Error; err != nil {
		t.Fatalf("读取指针失败: %v", err)
	}
	if pointer.GenerationID != latest.GenerationID || pointer.PendingGenerationID != "" || pointer.PendingTaskID != 0 {
		t.Fatalf("旧任务覆盖了最新 generation 或 pending 未清理: %+v", pointer)
	}
	assertTimelineGenerationRemoved(t, db, root, first)
	assertTimelineGenerationPresent(t, root, latest)
}

func assertTimelineGenerationRemoved(t *testing.T, db *gorm.DB, root string, status TimelinePreviewStatus) {
	t.Helper()
	payload := timelinePayloadFromStatus(status)
	if _, err := os.Stat(TimelinePreviewGenerationPath(root, payload)); !os.IsNotExist(err) {
		t.Fatalf("被取代 generation 目录应删除: %v", err)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("cache_key = ?", TimelinePreviewCacheKey(payload)).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("被取代 generation 不得登记缓存: count=%d err=%v", count, err)
	}
}

func assertTimelineGenerationPresent(t *testing.T, root string, status TimelinePreviewStatus) {
	t.Helper()
	if _, err := os.Stat(TimelinePreviewGenerationPath(root, timelinePayloadFromStatus(status))); err != nil {
		t.Fatalf("最新 generation 目录应保留: %v", err)
	}
}

func timelinePayloadFromStatus(status TimelinePreviewStatus) TimelinePreviewPayload {
	return TimelinePreviewPayload{
		SpaceID: models.DefaultSpaceID, MediaID: 42, ProfileID: status.ProfileID,
		SourceFingerprint: status.SourceFingerprint, GenerationID: status.GenerationID,
	}
}

func loadRunningTimelineTask(t *testing.T, db *gorm.DB, id int64) models.Task {
	t.Helper()
	if err := db.Model(&models.Task{}).Where("id = ?", id).Update("status", models.TaskStatusRunning).Error; err != nil {
		t.Fatalf("更新任务状态失败: %v", err)
	}
	var task models.Task
	if err := db.First(&task, id).Error; err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	return task
}

func TestTimelinePreview激活事务失败回滚缓存与指针(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, err := service.Enqueue(context.Background(), TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task := loadRunningTimelineTask(t, db, status.TaskID)
	payload, err := DecodeTimelinePreviewPayload(task.PayloadJSON)
	if err != nil {
		t.Fatalf("解析任务失败: %v", err)
	}
	output := TimelinePreviewGenerationPath(root, payload)
	if err := (&timelinePreviewFakeGenerator{}).Generate(context.Background(), TimelinePreviewGenerateRequest{OutputDir: output}); err != nil {
		t.Fatalf("准备产物失败: %v", err)
	}
	callback := "测试时间线指针事务失败"
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "media_timeline_previews" {
			_ = tx.AddError(errors.New("模拟指针事务失败"))
		}
	}); err != nil {
		t.Fatalf("注册事务失败回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	if err := service.registerAndActivate(context.Background(), task.ID, media, payload, output); err == nil {
		t.Fatal("指针事务失败应返回错误")
	}
	var assets int64
	if err := db.Model(&models.CacheAsset{}).Where("cache_key = ?", TimelinePreviewCacheKey(payload)).Count(&assets).Error; err != nil || assets != 0 {
		t.Fatalf("指针事务失败不得残留缓存登记: count=%d err=%v", assets, err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("指针事务失败应补偿删除 generation: %v", err)
	}
}

func TestTimelinePreview缓存注册失败不切指针(t *testing.T) {
	generator := &timelinePreviewFakeGenerator{}
	service, _, _, db, _, _ := newTimelinePreviewTestService(t, generator)
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	status, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task := loadRunningTimelineTask(t, db, status.TaskID)
	payload, err := DecodeTimelinePreviewPayload(task.PayloadJSON)
	if err != nil {
		t.Fatalf("解析任务失败: %v", err)
	}
	output := TimelinePreviewGenerationPath(service.dataDir, payload)
	if err := generator.Generate(context.Background(), TimelinePreviewGenerateRequest{OutputDir: output}); err != nil {
		t.Fatalf("准备产物失败: %v", err)
	}
	if err := os.RemoveAll(output); err != nil {
		t.Fatalf("删除产物失败: %v", err)
	}
	if err := service.registerAndActivate(context.Background(), task.ID, models.MediaFile{LibraryID: 7}, payload, output); err == nil {
		t.Fatal("缓存注册失败应返回错误")
	}
	assertTimelineNotActivated(t, db, status)
}

func TestTimelinePreview事务内发现被取代不登记并清理(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	_, task, payload, output := prepareTimelineRegistration(t, service, db, root)
	callback := "测试时间线事务前被取代"
	if err := db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "tasks" {
			return
		}
		tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE media_timeline_previews SET pending_generation_id = ?, pending_task_id = ? WHERE pending_task_id = ?", "generation-latest", task.ID+1, task.ID)
	}); err != nil {
		t.Fatalf("注册被取代回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	if err := service.registerAndActivate(context.Background(), task.ID, media, payload, output); err != nil {
		t.Fatalf("事务内发现被取代应清理后成功结束: %v", err)
	}
	assertTimelineNotRegistered(t, db, payload)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("事务内发现被取代应清理 generation: %v", err)
	}
	var pointer models.MediaTimelinePreview
	if err := db.Where("media_id = ?", payload.MediaID).First(&pointer).Error; err != nil {
		t.Fatalf("读取最新 pending 指针失败: %v", err)
	}
	if pointer.PendingGenerationID != "generation-latest" || pointer.PendingTaskID != task.ID+1 || pointer.GenerationID != "" {
		t.Fatalf("旧任务不得影响最新 pending 或 current: %+v", pointer)
	}
}

func TestTimelinePreview被取代清理失败返回错误可重试(t *testing.T) {
	service, _, _, db, root, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	first, err := service.Rebuild(context.Background(), identity)
	if err != nil {
		t.Fatalf("首次重建入队失败: %v", err)
	}
	latest, err := service.Rebuild(context.Background(), identity)
	if err != nil {
		t.Fatalf("最新重建入队失败: %v", err)
	}
	if err := service.handleTask(context.Background(), loadRunningTimelineTask(t, db, latest.TaskID)); err != nil {
		t.Fatalf("最新重建完成失败: %v", err)
	}
	firstTask := loadRunningTimelineTask(t, db, first.TaskID)
	cleanCompensation := service.cleanCompensation
	service.cleanCompensation = func(context.Context, TimelinePreviewPayload, string) error {
		return errors.New("模拟删除失败")
	}
	if err := service.handleTask(context.Background(), firstTask); err == nil {
		t.Fatal("清理失败必须返回错误供任务重试")
	}
	assertTimelineNotRegistered(t, db, timelinePayloadFromStatus(first))
	assertTimelineGenerationPresent(t, root, first)
	assertTimelinePointerCurrent(t, db, latest)
	service.cleanCompensation = cleanCompensation
	if err := service.handleTask(context.Background(), firstTask); err != nil {
		t.Fatalf("清理重试应成功: %v", err)
	}
	assertTimelineGenerationRemoved(t, db, root, first)
	assertTimelinePointerCurrent(t, db, latest)
}

func assertTimelineNotRegistered(t *testing.T, db *gorm.DB, payload TimelinePreviewPayload) {
	t.Helper()
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("cache_key = ?", TimelinePreviewCacheKey(payload)).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("被取代 generation 不得登记缓存: count=%d err=%v", count, err)
	}
}

func assertTimelinePointerCurrent(t *testing.T, db *gorm.DB, status TimelinePreviewStatus) {
	t.Helper()
	var pointer models.MediaTimelinePreview
	if err := db.Where("generation_id = ? AND asset_id > 0", status.GenerationID).First(&pointer).Error; err != nil {
		t.Fatalf("当前 pointer 不得受旧任务清理影响: %v", err)
	}
}

func TestTimelinePreview激活Busy一次后重试成功(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, task, payload, output := prepareTimelineRegistration(t, service, db, root)
	attempts := registerTimelineBusyCallback(t, db, 1)
	if err := service.registerAndActivate(context.Background(), task.ID, media, payload, output); err != nil {
		t.Fatalf("busy 一次后应重试成功: %v", err)
	}
	if *attempts != 2 {
		t.Fatalf("busy 一次应执行两次缓存登记: %d", *attempts)
	}
	assertTimelineActivated(t, db, status)
	assertTimelineGenerationPresent(t, root, status)
}

func TestTimelinePreview激活持续Busy失败并清理目录(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, task, payload, output := prepareTimelineRegistration(t, service, db, root)
	attempts := registerTimelineBusyCallback(t, db, 100)
	if err := service.registerAndActivate(context.Background(), task.ID, media, payload, output); err == nil || !isSQLiteBusy(err) {
		t.Fatalf("持续 busy 应返回 busy 错误: %v", err)
	}
	if *attempts != timelineWriteAttempts {
		t.Fatalf("持续 busy 应有限重试 %d 次，实际 %d", timelineWriteAttempts, *attempts)
	}
	assertTimelineNotActivated(t, db, status)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("持续 busy 失败应清理 generation: %v", err)
	}
}

func TestTimelinePreviewBusy重试前检查取消并清理(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, task, payload, output := prepareTimelineRegistration(t, service, db, root)
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	callback := "测试时间线busy后取消"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "cache_assets" {
			return
		}
		attempts++
		cancel()
		_ = tx.AddError(errors.New("database is locked"))
	}); err != nil {
		t.Fatalf("注册 busy 取消回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	if err := service.registerAndActivate(ctx, task.ID, media, payload, output); !errors.Is(err, context.Canceled) {
		t.Fatalf("busy 重试前取消应返回 context.Canceled: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("取消后不得继续重试: %d", attempts)
	}
	assertTimelineNotActivated(t, db, status)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("busy 后取消应清理 generation: %v", err)
	}
}

func TestTimelinePreview事务内取消回滚并清理目录(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, task, payload, output := prepareTimelineRegistration(t, service, db, root)
	ctx, cancel := context.WithCancel(context.Background())
	callback := "测试时间线事务内取消"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "cache_assets" {
			cancel()
		}
	}); err != nil {
		t.Fatalf("注册事务内取消回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	if err := service.registerAndActivate(ctx, task.ID, media, payload, output); !errors.Is(err, context.Canceled) {
		t.Fatalf("事务内取消应返回 context.Canceled: %v", err)
	}
	assertTimelineNotActivated(t, db, status)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("事务内取消应清理 generation: %v", err)
	}
}

func prepareTimelineRegistration(t *testing.T, service *TimelinePreviewService, db *gorm.DB, root string) (TimelinePreviewStatus, models.Task, TimelinePreviewPayload, string) {
	t.Helper()
	status, err := service.Enqueue(context.Background(), TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task := loadRunningTimelineTask(t, db, status.TaskID)
	payload, err := DecodeTimelinePreviewPayload(task.PayloadJSON)
	if err != nil {
		t.Fatalf("解析任务失败: %v", err)
	}
	output := TimelinePreviewGenerationPath(root, payload)
	if err := (&timelinePreviewFakeGenerator{}).Generate(context.Background(), TimelinePreviewGenerateRequest{OutputDir: output}); err != nil {
		t.Fatalf("准备产物失败: %v", err)
	}
	return status, task, payload, output
}

func registerTimelineBusyCallback(t *testing.T, db *gorm.DB, failures int) *int {
	t.Helper()
	attempts := 0
	callback := fmt.Sprintf("测试时间线busy-%d", failures)
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "cache_assets" {
			return
		}
		attempts++
		if attempts <= failures {
			_ = tx.AddError(errors.New("database is locked"))
		}
	}); err != nil {
		t.Fatalf("注册 busy 回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	return &attempts
}

func assertTimelineActivated(t *testing.T, db *gorm.DB, status TimelinePreviewStatus) {
	t.Helper()
	var pointer models.MediaTimelinePreview
	if err := db.Where("generation_id = ? AND asset_id > 0", status.GenerationID).First(&pointer).Error; err != nil {
		t.Fatalf("generation 应成功激活: %v", err)
	}
}

func TestTimelinePreview提交前取消不激活(t *testing.T) {
	service, _, _, db, root, media := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, err := service.Enqueue(context.Background(), TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task := loadRunningTimelineTask(t, db, status.TaskID)
	payload, err := DecodeTimelinePreviewPayload(task.PayloadJSON)
	if err != nil {
		t.Fatalf("解析任务失败: %v", err)
	}
	output := TimelinePreviewGenerationPath(root, payload)
	if err := (&timelinePreviewFakeGenerator{}).Generate(context.Background(), TimelinePreviewGenerateRequest{OutputDir: output}); err != nil {
		t.Fatalf("准备产物失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	callback := "测试时间线提交前取消"
	if err := db.Callback().Update().After("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			cancel()
		}
	}); err != nil {
		t.Fatalf("注册取消回调失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	if err := service.registerAndActivate(ctx, task.ID, media, payload, output); !errors.Is(err, context.Canceled) {
		t.Fatalf("提交前取消应返回 context.Canceled: %v", err)
	}
	assertTimelineNotActivated(t, db, status)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("提交前取消应补偿删除 generation: %v", err)
	}
}

func assertTimelineNotActivated(t *testing.T, db *gorm.DB, status TimelinePreviewStatus) {
	t.Helper()
	var pointer models.MediaTimelinePreview
	if err := db.Where("pending_task_id = ?", status.TaskID).First(&pointer).Error; err != nil {
		t.Fatalf("读取 pending 指针失败: %v", err)
	}
	if pointer.AssetID != 0 || pointer.GenerationID != "" || pointer.PendingGenerationID != status.GenerationID {
		t.Fatalf("失败或取消不得激活当前指针: %+v", pointer)
	}
	var assets int64
	if err := db.Model(&models.CacheAsset{}).Where("cache_key = ?", TimelinePreviewCacheKey(TimelinePreviewPayload{
		SpaceID: pointer.SpaceID, MediaID: pointer.MediaID, ProfileID: pointer.ProfileID,
		SourceFingerprint: status.SourceFingerprint, GenerationID: status.GenerationID,
	})).Count(&assets).Error; err != nil || assets != 0 {
		t.Fatalf("失败或取消不得登记缓存: count=%d err=%v", assets, err)
	}
}

func TestTimelinePreview提交后取消Handler仍成功(t *testing.T) {
	service, _, _, db, _, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	status, err := service.Enqueue(context.Background(), TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	task := loadRunningTimelineTask(t, db, status.TaskID)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.handleTask(ctx, task) }()
	waitTimelineActivated(t, db, status.GenerationID)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("事务提交后取消不应让 handler 失败: %v", err)
	}
}

func waitTimelineActivated(t *testing.T, db *gorm.DB, generationID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		err := db.Model(&models.MediaTimelinePreview{}).Where("generation_id = ? AND asset_id > 0", generationID).Count(&count).Error
		if err == nil && count == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 generation %s 激活超时", generationID)
}

func TestTimelinePreview终态任务后普通请求创建新Generation(t *testing.T) {
	service, _, _, db, _, _ := newTimelinePreviewTestService(t, &timelinePreviewFakeGenerator{})
	identity := TimelinePreviewIdentity{SpaceID: models.DefaultSpaceID, MediaID: 42}
	first, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	if err := db.Model(&models.Task{}).Where("id = ?", first.TaskID).Update("status", models.TaskStatusFailed).Error; err != nil {
		t.Fatalf("设置终态失败: %v", err)
	}
	second, err := service.Enqueue(context.Background(), identity)
	if err != nil {
		t.Fatalf("终态后再次入队失败: %v", err)
	}
	if second.TaskID == first.TaskID || second.GenerationID == first.GenerationID {
		t.Fatalf("终态后应创建新 generation: first=%+v second=%+v", first, second)
	}
}
