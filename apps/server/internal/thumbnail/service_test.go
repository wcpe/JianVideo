package thumbnail

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/storage"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupThumbnailService(t *testing.T) (*Service, *gorm.DB, *tasksvc.Service, *tasksvc.WorkerRegistry, string) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "thumbnail.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Space{}, &models.LibraryPath{}, &models.MediaFile{}, &models.Task{}, &models.CacheAsset{}); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	for _, id := range []string{models.DefaultSpaceID, "space-a", "space-b"} {
		if err := db.Create(&models.Space{ID: id, Name: id, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建测试 Space 失败: %v", err)
		}
	}
	taskService := tasksvc.NewService(db)
	cacheService := storage.NewService(db, dataDir).WithTasks(taskService)
	libService := library.NewService(db)
	service := NewService(libService, taskService, cacheService, dataDir)
	service.SetGeneratorForTest(func(_ context.Context, _ models.MediaFile, _ int, outputPath string) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("jpeg"), 0o640)
	})
	registry := tasksvc.NewWorkerRegistry(taskService)
	if err := service.RegisterWorkers(registry, 2); err != nil {
		t.Fatalf("注册缩略图 worker 失败: %v", err)
	}
	if err := cacheService.RegisterWorkers(registry); err != nil {
		t.Fatalf("注册缓存 worker 失败: %v", err)
	}
	return service, db, taskService, registry, dataDir
}

func createThumbnailMedia(t *testing.T, db *gorm.DB, spaceID, name string) models.MediaFile {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("source"), 0o640); err != nil {
		t.Fatalf("写入测试媒体失败: %v", err)
	}
	libraryPath := models.LibraryPath{SpaceID: spaceID, Path: root, Type: "local", Enabled: 1}
	if err := db.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建测试媒体库失败: %v", err)
	}
	media := models.MediaFile{SpaceID: spaceID, LibraryID: libraryPath.ID, FilePath: path, FileName: name, Format: filepath.Ext(name)[1:]}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建测试媒体失败: %v", err)
	}
	return media
}

func TestPathFor按Space媒体尺寸隔离并拒绝危险Space(t *testing.T) {
	root := t.TempDir()
	got, err := PathFor(root, "space-a", 42, 160)
	if err != nil {
		t.Fatalf("构造路径失败: %v", err)
	}
	want := filepath.Join(root, "thumbnails", "space-a", "42", "160.jpg")
	if got != want {
		t.Fatalf("缩略图路径不匹配: got=%s want=%s", got, want)
	}
	if _, err := PathFor(root, "../space-b", 42, 160); err == nil {
		t.Fatal("危险 Space 路径必须被拒绝")
	}
}

func TestEnsure幂等入队且生成后登记三档缓存(t *testing.T) {
	service, db, _, registry, _ := setupThumbnailService(t)
	media := createThumbnailMedia(t, db, "space-a", "image.jpg")

	first, err := service.Ensure(context.Background(), "space-a", media.ID, []int{160, 320, 640})
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	second, err := service.Ensure(context.Background(), "space-a", media.ID, []int{640, 320, 160})
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	if first.TaskID == 0 || first.TaskID != second.TaskID {
		t.Fatalf("活动任务应按尺寸集合幂等复用: first=%d second=%d", first.TaskID, second.TaskID)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行缩略图任务失败: %v", err)
	}

	var assets []models.CacheAsset
	if err := db.Where("space_id = ? AND media_id = ? AND kind = ?", "space-a", media.ID, storage.CacheKindThumbnail).
		Order("variant ASC").Find(&assets).Error; err != nil {
		t.Fatalf("查询缓存资产失败: %v", err)
	}
	if len(assets) != 3 {
		t.Fatalf("应登记三档缓存资产，实际 %d", len(assets))
	}
	for index, size := range []string{"160", "320", "640"} {
		if assets[index].Variant != size {
			t.Fatalf("缓存尺寸登记错误: got=%s want=%s", assets[index].Variant, size)
		}
		path, pathErr := PathFor(service.DataDir(), "space-a", media.ID, mustSize(t, size))
		if pathErr != nil {
			t.Fatalf("构造缓存路径失败: %v", pathErr)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("缩略图文件不存在: %s err=%v", path, err)
		}
	}
}

func TestEnsure跨Space不复用任务(t *testing.T) {
	service, db, _, _, _ := setupThumbnailService(t)
	mediaA := createThumbnailMedia(t, db, "space-a", "a.jpg")
	mediaB := createThumbnailMedia(t, db, "space-b", "b.jpg")

	a, err := service.Ensure(context.Background(), "space-a", mediaA.ID, []int{320})
	if err != nil {
		t.Fatalf("Space A 入队失败: %v", err)
	}
	b, err := service.Ensure(context.Background(), "space-b", mediaB.ID, []int{320})
	if err != nil {
		t.Fatalf("Space B 入队失败: %v", err)
	}
	if a.TaskID == b.TaskID {
		t.Fatalf("跨 Space 任务不得复用: %d", a.TaskID)
	}
}

func Test清理缩略图后可重新生成(t *testing.T) {
	service, db, taskService, registry, _ := setupThumbnailService(t)
	media := createThumbnailMedia(t, db, "space-a", "rebuild.jpg")

	first, err := service.Ensure(context.Background(), "space-a", media.ID, []int{320})
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	path, err := PathFor(service.DataDir(), "space-a", media.ID, 320)
	if err != nil {
		t.Fatalf("构造路径失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("首次生成文件不存在: %v", err)
	}

	clean, err := service.Cache().Clean(context.Background(), storage.CleanInput{SpaceID: "space-a", Kinds: []string{storage.CacheKindThumbnail}})
	if err != nil {
		t.Fatalf("入队清理失败: %v", err)
	}
	if clean.TaskID == 0 {
		t.Fatal("真实清理必须返回任务 ID")
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行清理失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("清理后缩略图应不存在: %v", err)
	}

	second, err := service.Ensure(context.Background(), "space-a", media.ID, []int{320})
	if err != nil {
		t.Fatalf("清理后重新入队失败: %v", err)
	}
	if second.TaskID == first.TaskID {
		t.Fatalf("已完成任务不能阻止清理后重建: %d", second.TaskID)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("重新生成失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("重建后缩略图不存在: %v", err)
	}
	if _, err := taskService.Get(context.Background(), second.TaskID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("重建任务不可查询: %v", err)
	}
}

func TestBackfill批量生成并记录检查点(t *testing.T) {
	service, db, taskService, registry, _ := setupThumbnailService(t)
	for _, name := range []string{"one.jpg", "two.mp4", "three.png"} {
		createThumbnailMedia(t, db, "space-a", name)
	}
	result, err := service.Backfill(context.Background(), "space-a", []int{160, 320})
	if err != nil {
		t.Fatalf("批量入队失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("批量生成失败: %v", err)
	}
	task, err := taskService.Get(context.Background(), result.TaskID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("查询批量任务失败: %v", err)
	}
	if task.Status != models.TaskStatusSucceeded || task.Progress != 100 || task.Checkpoint == "" {
		t.Fatalf("批量任务终态不完整: status=%s progress=%d checkpoint=%q", task.Status, task.Progress, task.Checkpoint)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("space_id = ? AND kind = ?", "space-a", storage.CacheKindThumbnail).Count(&count).Error; err != nil {
		t.Fatalf("统计缓存资产失败: %v", err)
	}
	if count != 6 {
		t.Fatalf("三媒体两尺寸应登记 6 个资产，实际 %d", count)
	}
}

func TestBackfill跳过不具备缩略图能力的媒体(t *testing.T) {
	service, db, taskService, registry, _ := setupThumbnailService(t)
	createThumbnailMedia(t, db, "space-a", "supported.jpg")
	createThumbnailMedia(t, db, "space-a", "unsupported.txt")

	result, err := service.Backfill(context.Background(), "space-a", []int{320})
	if err != nil {
		t.Fatalf("批量入队失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("批量任务执行失败: %v", err)
	}
	task, err := taskService.Get(context.Background(), result.TaskID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("查询批量任务失败: %v", err)
	}
	if task.Status != models.TaskStatusSucceeded {
		t.Fatalf("不支持缩略图的媒体不应拖垮批量任务: status=%s error=%s", task.Status, task.Error)
	}
	var count int64
	if err := db.Model(&models.CacheAsset{}).Where("space_id = ? AND kind = ?", "space-a", storage.CacheKindThumbnail).Count(&count).Error; err != nil {
		t.Fatalf("统计缓存资产失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("仅支持的媒体应生成缩略图，实际资产数 %d", count)
	}
}

func TestGenerate任务拒绝空尺寸载荷(t *testing.T) {
	service, _, _, _, _ := setupThumbnailService(t)
	spaceID := "space-a"
	task := models.Task{
		Type: TaskTypeGenerate, Scope: models.TaskScopeSpace, SpaceID: &spaceID,
		ResourceType: "media", ResourceID: "1", PayloadJSON: `{"space_id":"space-a","media_id":1,"sizes":[]}`,
	}
	if err := service.handleGenerate(context.Background(), task); err == nil {
		t.Fatal("空尺寸任务载荷必须被拒绝")
	}
}

func mustSize(t *testing.T, raw string) int {
	t.Helper()
	switch raw {
	case "160":
		return 160
	case "320":
		return 320
	case "640":
		return 640
	default:
		t.Fatalf("未知尺寸: %s", raw)
		return 0
	}
}
