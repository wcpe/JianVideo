package library

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// newExportTestDB 构造测试用内存数据库与最小表结构。
func newExportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return gdb
}

func TestEnqueueImageExportIdempotent(t *testing.T) {
	gdb := newExportTestDB(t)
	tasks := tasksvc.NewService(gdb)
	first, err := EnqueueImageExport(context.Background(), tasks, "space-default", 1, ImageExportParams{Format: "jpeg", Exposure: 10})
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	second, err := EnqueueImageExport(context.Background(), tasks, "space-default", 1, ImageExportParams{Format: "jpeg", Exposure: 10})
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("同幂等键应复用任务, got %d vs %d", first.ID, second.ID)
	}
	other, err := EnqueueImageExport(context.Background(), tasks, "space-default", 1, ImageExportParams{Format: "png"})
	if err != nil {
		t.Fatalf("不同格式入队失败: %v", err)
	}
	if other.ID == first.ID {
		t.Fatal("不同参数应入队新任务")
	}
}

func TestEnqueueVideoClipIdempotent(t *testing.T) {
	gdb := newExportTestDB(t)
	tasks := tasksvc.NewService(gdb)
	p := VideoClipParams{StartSec: 1, EndSec: 5, Format: "mp4"}
	first, err := EnqueueVideoClip(context.Background(), tasks, "space-default", 2, p)
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	second, err := EnqueueVideoClip(context.Background(), tasks, "space-default", 2, p)
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("同幂等键应复用任务")
	}
}

func TestParseExportTaskValidatesImage(t *testing.T) {
	gdb := newExportTestDB(t)
	tasks := tasksvc.NewService(gdb)
	task, err := EnqueueImageExport(context.Background(), tasks, "space-default", 1, ImageExportParams{Format: "jpeg"})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if _, err := parseExportTask(*task, true); err != nil {
		t.Fatalf("解析图片任务应通过: %v", err)
	}
	if _, err := parseExportTask(*task, false); err == nil {
		t.Fatal("期望 Image 任务无法被当作视频解析")
	}
}

func TestExportRunnerRegisters(t *testing.T) {
	gdb := newExportTestDB(t)
	tasks := tasksvc.NewService(gdb)
	registry := tasksvc.NewWorkerRegistry(tasks)
	runner := NewExportTaskRunner(t.TempDir(), nil, tasks)
	if err := runner.RegisterExportWorkers(registry); err != nil {
		t.Fatalf("注册导出 worker 失败: %v", err)
	}
	// 通过 DefaultConcurrency 验证已注册（不会报错）。
	if tasksvc.DefaultConcurrency(TaskTypeImageExport) == 0 {
		t.Fatal("ImageExport 任务并发度应 > 0")
	}
	if tasksvc.DefaultConcurrency(TaskTypeVideoClip) == 0 {
		t.Fatal("VideoClip 任务并发度应 > 0")
	}
}

func TestExportDirHelper(t *testing.T) {
	base := t.TempDir()
	got := ExportDir(base)
	want := filepath.Join(base, "exports")
	if got != want {
		t.Fatalf("导出根目录错误: %s vs %s", got, want)
	}
}
