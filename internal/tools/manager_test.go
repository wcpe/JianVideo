package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func TestManagerEnqueuesSystemTaskAndAppliesSettings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}

	archive := MakeTestToolZip(t, ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	taskSvc := tasksvc.NewService(db)
	manager := NewManager(ManagerOptions{
		Installer: NewInstaller(t.TempDir(), nil),
		Settings:  settings.NewService(db),
		Tasks:     taskSvc,
	})
	task, err := manager.EnqueueDownload(context.Background(), DownloadRequest{
		Tool:              ToolFFmpeg,
		CustomURL:         server.URL,
		SHA256:            hex.EncodeToString(sum[:]),
		Version:           "test",
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("入队应成功: %v", err)
	}
	if task.Scope != models.TaskScopeSystem || task.SpaceID != nil || task.Type != TaskTypeDownload {
		t.Fatalf("工具下载必须是系统级任务: %+v", task)
	}

	registry := tasksvc.NewWorkerRegistry(taskSvc)
	if err := manager.RegisterWorker(registry); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行 worker 失败: %v", err)
	}

	value, err := settings.NewService(db).Get(settings.KeyFFmpegPath)
	if err != nil {
		t.Fatalf("读取 ffmpeg_path 失败: %v", err)
	}
	if value == "" || !strings.Contains(value, "ffmpeg") {
		t.Fatalf("下载成功后应写入 ffmpeg_path，实际 %q", value)
	}

	page, err := taskSvc.List(context.Background(), tasksvc.Query{Scope: models.TaskScopeSystem})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != models.TaskStatusSucceeded || page.Items[0].Progress != 100 {
		t.Fatalf("任务终态不正确: %+v", page.Items)
	}
}

func TestManagerMarksFailedOnInstallErrorAndRetryWorks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}

	taskSvc := tasksvc.NewService(db)
	manager := NewManager(ManagerOptions{
		Installer: NewInstaller(t.TempDir(), nil),
		Settings:  settings.NewService(db),
		Tasks:     taskSvc,
	})
	task, err := manager.EnqueueDownload(context.Background(), DownloadRequest{
		Tool:              ToolFFmpeg,
		CustomURL:         "http://127.0.0.1:1/missing.zip",
		SHA256:            strings.Repeat("a", 64),
		Version:           "test",
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("入队应成功: %v", err)
	}

	registry := tasksvc.NewWorkerRegistry(taskSvc)
	if err := manager.RegisterWorker(registry); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("执行失败任务不应让 worker 返回错误: %v", err)
	}
	got, err := taskSvc.Get(context.Background(), task.ID, tasksvc.Query{Scope: models.TaskScopeSystem})
	if err != nil {
		t.Fatalf("查询失败任务失败: %v", err)
	}
	if got.Status != models.TaskStatusFailed || got.Error == "" {
		t.Fatalf("下载失败应进入 failed 并记录错误: %+v", got)
	}
	if err := taskSvc.Retry(context.Background(), task.ID, tasksvc.Query{Scope: models.TaskScopeSystem}); err != nil {
		t.Fatalf("失败任务应可重试: %v", err)
	}
}

func TestManagerDoesNotPersistSettingWhenApplyFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}

	archive := MakeTestToolZip(t, ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	taskSvc := tasksvc.NewService(db)
	manager := NewManager(ManagerOptions{
		Installer: NewInstaller(t.TempDir(), nil),
		Settings:  settings.NewService(db),
		Tasks:     taskSvc,
		Apply: func(InstallResult) error {
			return errors.New("模拟热应用失败")
		},
	})
	task, err := manager.EnqueueDownload(context.Background(), DownloadRequest{
		Tool:              ToolFFmpeg,
		CustomURL:         server.URL,
		SHA256:            hex.EncodeToString(sum[:]),
		Version:           "test",
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("入队应成功: %v", err)
	}

	registry := tasksvc.NewWorkerRegistry(taskSvc)
	if err := manager.RegisterWorker(registry); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	if err := registry.RunPending(context.Background()); err != nil {
		t.Fatalf("热应用失败应只让任务失败，不应让 worker 返回错误: %v", err)
	}

	got, err := taskSvc.Get(context.Background(), task.ID, tasksvc.Query{Scope: models.TaskScopeSystem})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if got.Status != models.TaskStatusFailed || !strings.Contains(got.Error, "热应用") {
		t.Fatalf("热应用失败应标记任务失败，实际 %+v", got)
	}
	value, err := settings.NewService(db).Get(settings.KeyFFmpegPath)
	if err != nil {
		t.Fatalf("读取 ffmpeg_path 失败: %v", err)
	}
	if value != "" {
		t.Fatalf("热应用失败时不应持久化工具路径，实际 %q", value)
	}
}

func TestManagerStopsCanceledTaskBeforePersistingSetting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}

	archive := MakeTestToolZip(t, ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	taskSvc := tasksvc.NewService(db)
	manager := NewManager(ManagerOptions{
		Installer: NewInstaller(t.TempDir(), nil),
		Settings:  settings.NewService(db),
		Tasks:     taskSvc,
	})
	_, err = manager.EnqueueDownload(context.Background(), DownloadRequest{
		Tool:              ToolFFmpeg,
		CustomURL:         server.URL,
		SHA256:            hex.EncodeToString(sum[:]),
		Version:           "test",
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("入队应成功: %v", err)
	}
	claimed, err := taskSvc.ClaimNext(context.Background(), tasksvc.ClaimQuery{Type: TaskTypeDownload})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}
	if err := taskSvc.Cancel(context.Background(), claimed.ID, tasksvc.Query{Scope: models.TaskScopeSystem}); err != nil {
		t.Fatalf("取消任务失败: %v", err)
	}

	err = manager.handleTask(context.Background(), *claimed)
	if err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("已取消任务应停止处理，实际 %v", err)
	}
	value, err := settings.NewService(db).Get(settings.KeyFFmpegPath)
	if err != nil {
		t.Fatalf("读取 ffmpeg_path 失败: %v", err)
	}
	if value != "" {
		t.Fatalf("已取消任务不应写入工具路径，实际 %q", value)
	}
}
