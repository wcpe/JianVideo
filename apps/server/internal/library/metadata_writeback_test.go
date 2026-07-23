package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupWritebackTest(t *testing.T) (*Service, *tasksvc.Service, *gorm.DB, string) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开库: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.Task{}); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	base := t.TempDir()
	InitWritebackSnapshotDir(base)
	return NewService(gdb), tasksvc.NewService(gdb), gdb, base
}

func seedImageMedia(t *testing.T, svc *Service, dir string) *models.MediaFile {
	t.Helper()
	lp, err := svc.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "lib")
	if err != nil {
		t.Fatalf("建库: %v", err)
	}
	file := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(file, []byte("fake-jpeg-bytes"), 0o600); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	mf, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 15)
	if err != nil {
		t.Fatalf("创建媒体: %v", err)
	}
	if _, err := svc.UpdateDisplayNameInSpace(models.DefaultSpaceID, mf.ID, "测试标题"); err != nil {
		t.Fatalf("显示名: %v", err)
	}
	if _, err := svc.mediaRepo.UpdateField(models.DefaultSpaceID, mf.ID, "notes", "备注内容"); err != nil {
		t.Fatalf("备注: %v", err)
	}
	if _, err := svc.mediaRepo.UpdateField(models.DefaultSpaceID, mf.ID, "camera", "Canon"); err != nil {
		t.Fatalf("camera: %v", err)
	}
	got, err := svc.GetMediaFileByIDInSpace(models.DefaultSpaceID, mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEnqueueMetadataWriteback_RequiresConfirm(t *testing.T) {
	svc, tasks, _, base := setupWritebackTest(t)
	dir := t.TempDir()
	mf := seedImageMedia(t, svc, dir)

	_, err := EnqueueMetadataWriteback(context.Background(), tasks, svc, base, models.DefaultSpaceID, mf.ID, false)
	if !errors.Is(err, ErrWritebackConfirmRequired) {
		t.Fatalf("期望 CONFIRM, 得到 %v", err)
	}
}

func TestEnqueueMetadataWriteback_RejectsVideo(t *testing.T) {
	svc, tasks, _, base := setupWritebackTest(t)
	dir := t.TempDir()
	lp, _ := svc.CreateLibraryPathInSpace(models.DefaultSpaceID, dir, "local", "lib")
	file := filepath.Join(dir, "clip.mp4")
	_ = os.WriteFile(file, []byte("video"), 0o600)
	mf, err := svc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, file, 5)
	if err != nil {
		t.Fatal(err)
	}
	// 视频无写回字段时也会先被类型拒绝
	_, err = EnqueueMetadataWriteback(context.Background(), tasks, svc, base, models.DefaultSpaceID, mf.ID, true)
	if !errors.Is(err, ErrWritebackVideoUnsupported) {
		t.Fatalf("期望视频拒绝, 得到 %v", err)
	}
}

func TestEnqueueMetadataWriteback_SnapshotAndTask(t *testing.T) {
	svc, tasks, db, base := setupWritebackTest(t)
	dir := t.TempDir()
	mf := seedImageMedia(t, svc, dir)

	task, err := EnqueueMetadataWriteback(context.Background(), tasks, svc, base, models.DefaultSpaceID, mf.ID, true)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if task.Type != TaskTypeMetadataWriteback {
		t.Fatalf("type=%s", task.Type)
	}
	if task.Status != models.TaskStatusPending {
		t.Fatalf("status=%s", task.Status)
	}

	snapRoot := WritebackSnapshotDir(base)
	entries, err := os.ReadDir(filepath.Join(snapRoot, models.DefaultSpaceID, strconv.FormatInt(mf.ID, 10)))
	if err != nil || len(entries) == 0 {
		t.Fatalf("快照目录应有文件: err=%v n=%d", err, len(entries))
	}

	raw, _ := os.ReadFile(filepath.FromSlash(mf.FilePath))
	if string(raw) != "fake-jpeg-bytes" {
		t.Fatalf("入队不应改原文件")
	}

	var count int64
	if err := db.Model(&models.Task{}).Where("type = ?", TaskTypeMetadataWriteback).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("任务数: %d err=%v", count, err)
	}
}

func TestWritebackWorker_ToolFailurePreservesOriginal(t *testing.T) {
	svc, tasks, _, base := setupWritebackTest(t)
	dir := t.TempDir()
	mf := seedImageMedia(t, svc, dir)

	orig := writeImageMetadataFn
	t.Cleanup(func() { writeImageMetadataFn = orig })
	writeImageMetadataFn = func(ctx context.Context, sourcePath string, fields map[string]string) error {
		return errors.New("模拟 magick 失败")
	}

	task, err := EnqueueMetadataWriteback(context.Background(), tasks, svc, base, models.DefaultSpaceID, mf.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewWritebackTaskRunner(base, svc, tasks)
	if err := runner.handleWriteback(context.Background(), *task); err == nil {
		t.Fatal("期望写回失败")
	}

	raw, err := os.ReadFile(filepath.FromSlash(mf.FilePath))
	if err != nil || string(raw) != "fake-jpeg-bytes" {
		t.Fatalf("失败后原文件应不变: %v content=%q", err, string(raw))
	}
	snapDir := filepath.Join(WritebackSnapshotDir(base), models.DefaultSpaceID, strconv.FormatInt(mf.ID, 10))
	ents, _ := os.ReadDir(snapDir)
	if len(ents) == 0 {
		t.Fatal("失败后快照应保留")
	}
}

func TestWritebackWorker_SuccessReplacesWithTool(t *testing.T) {
	svc, tasks, _, base := setupWritebackTest(t)
	dir := t.TempDir()
	mf := seedImageMedia(t, svc, dir)

	orig := writeImageMetadataFn
	t.Cleanup(func() { writeImageMetadataFn = orig })
	writeImageMetadataFn = func(ctx context.Context, sourcePath string, fields map[string]string) error {
		return os.WriteFile(sourcePath, []byte("written-by-tool"), 0o600)
	}

	task, err := EnqueueMetadataWriteback(context.Background(), tasks, svc, base, models.DefaultSpaceID, mf.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	// Enqueue 返回的 task 已含 PayloadJSON，无需再读私有 db。
	runner := NewWritebackTaskRunner(base, svc, tasks)
	if err := runner.handleWriteback(context.Background(), *task); err != nil {
		t.Fatalf("成功路径: %v", err)
	}
	raw, _ := os.ReadFile(filepath.FromSlash(mf.FilePath))
	if string(raw) != "written-by-tool" {
		t.Fatalf("成功后应被工具写入: %q", string(raw))
	}
}
