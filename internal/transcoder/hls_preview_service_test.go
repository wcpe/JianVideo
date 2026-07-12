package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func newHLSPreviewTestService(t *testing.T, exec HLSPreviewExecFunc) (*HLSPreviewService, *tasksvc.Service, *tasksvc.WorkerRegistry, *gorm.DB, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	root := filepath.Join(t.TempDir(), "hls")
	svc := NewHLSPreviewService(tasks, workers, root, exec)
	if err := svc.RegisterWorker(); err != nil {
		t.Fatalf("注册 HLS preview worker 失败: %v", err)
	}
	return svc, tasks, workers, db, root
}

func TestHLSPreviewServiceEnqueueUsesUnifiedTaskTypeAndPayload(t *testing.T) {
	svc, tasks, _, _, _ := newHLSPreviewTestService(t, func(context.Context, int64, HLSPreviewPayload) error { return nil })
	task, err := svc.Enqueue(context.Background(), HLSPreviewRequest{
		SpaceID:      "space-a",
		MediaID:      42,
		PresetID:     7,
		ProfileID:    "h264",
		Codec:        "h264",
		Width:        1280,
		Height:       720,
		Priority:     9,
		ForceRebuild: true,
	})
	if err != nil {
		t.Fatalf("HLS preview 入队失败: %v", err)
	}
	if task.Type != TaskTypeHLSPreview || task.Priority != 9 || task.MaxAttempts != HLSPreviewMaxAttempts {
		t.Fatalf("统一任务字段异常: %+v", task)
	}
	stored, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("读取统一任务失败: %v", err)
	}
	var payload HLSPreviewPayload
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &payload); err != nil {
		t.Fatalf("解析 payload 失败: %v", err)
	}
	if payload.MediaID != 42 || payload.PresetID != 7 || payload.ProfileID != "h264" || !payload.ForceRebuild {
		t.Fatalf("payload 不完整: %+v", payload)
	}
	if payload.Codec != "h264" || payload.Width != 1280 || payload.Height != 720 {
		t.Fatalf("payload 预设快照异常: %+v", payload)
	}
}

func TestHLSPreviewServiceWorkerSupportsRetryAndProgress(t *testing.T) {
	calls := 0
	var root string
	svc, tasks, workers, _, returnedRoot := newHLSPreviewTestService(t, func(_ context.Context, _ int64, payload HLSPreviewPayload) error {
		calls++
		if calls == 1 {
			return errors.New("首次转码失败")
		}
		dir, err := HLSProfileDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U\n"), 0o640)
	})
	root = returnedRoot
	task, err := svc.Enqueue(context.Background(), HLSPreviewRequest{SpaceID: "space-a", MediaID: 8, ProfileID: "h264", Codec: "h264"})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("首次 worker 执行失败: %v", err)
	}
	failed, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("读取失败任务失败: %v", err)
	}
	if failed.Status != models.TaskStatusPending || failed.Attempts != 1 {
		t.Fatalf("首次失败应退避重试: %+v", failed)
	}
	tasks.SetNowForTest(func() time.Time { return failed.NextRunAt.Add(time.Second) })
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("重试 worker 执行失败: %v", err)
	}
	done, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("读取完成任务失败: %v", err)
	}
	if done.Status != models.TaskStatusSucceeded || done.Progress != 100 || calls != 2 {
		t.Fatalf("重试终态异常: task=%+v calls=%d", done, calls)
	}
}

func TestHLSPreviewResolutionPrefersPresetSnapshot(t *testing.T) {
	width, height := HLSPreviewResolution(HLSPreviewPayload{Width: 1280, Height: 720}, 1920, 1080)
	if width != 1280 || height != 720 {
		t.Fatalf("HLS preview 应优先使用任务中的预设尺寸快照: got=%dx%d", width, height)
	}
	width, height = HLSPreviewResolution(HLSPreviewPayload{}, 1920, 1080)
	if width != 1920 || height != 1080 {
		t.Fatalf("预设尺寸为空时应回退源尺寸: got=%dx%d", width, height)
	}
}

func TestHLSProfileDirRejectsTraversalAndIsolatesSpaceMediaProfile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hls")
	dir, err := HLSProfileDir(root, "space-a", 42, "h264")
	if err != nil {
		t.Fatalf("构造 profile 目录失败: %v", err)
	}
	want := filepath.Join(root, "space-a", "42", "h264")
	if dir != want {
		t.Fatalf("profile 目录不符: got=%s want=%s", dir, want)
	}
	if _, err := HLSProfileDir(root, "../space-b", 42, "h264"); err == nil {
		t.Fatal("Space 路径穿越应被拒绝")
	}
	if _, err := HLSProfileDir(root, "space-a", 42, "../h265"); err == nil {
		t.Fatal("profile 路径穿越应被拒绝")
	}
}

func TestHLSPreviewStatusUsesCodecManifestForCustomProfile(t *testing.T) {
	var root string
	svc, _, workers, _, returnedRoot := newHLSPreviewTestService(t, func(_ context.Context, _ int64, payload HLSPreviewPayload) error {
		dir, err := HLSProfileDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U\n"), 0o640)
	})
	root = returnedRoot
	if _, err := svc.Enqueue(context.Background(), HLSPreviewRequest{SpaceID: "space-a", MediaID: 9, ProfileID: "mobile", Codec: "h264"}); err != nil {
		t.Fatalf("入队自定义 H.264 profile 失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行自定义 H.264 profile 失败: %v", err)
	}
	status, err := svc.Status(context.Background(), "space-a", 9, "mobile")
	if err != nil {
		t.Fatalf("查询自定义 H.264 profile 失败: %v", err)
	}
	if !status.Available || status.URL != "/api/play/hls/9/profiles/mobile/master.m3u8" {
		t.Fatalf("自定义 H.264 profile 状态异常: %+v", status)
	}
}

func TestHLSPreviewCancelThenRetryRunsNewAttempt(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	calls := 0
	var root string
	svc, tasks, workers, _, returnedRoot := newHLSPreviewTestService(t, func(ctx context.Context, _ int64, payload HLSPreviewPayload) error {
		calls++
		if calls == 1 {
			close(started)
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		}
		dir, err := HLSProfileDir(root, payload.SpaceID, payload.MediaID, payload.ProfileID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U\n"), 0o640)
	})
	root = returnedRoot
	task, err := svc.Enqueue(context.Background(), HLSPreviewRequest{SpaceID: "space-a", MediaID: 10, Codec: "h264"})
	if err != nil {
		t.Fatalf("入队取消测试任务失败: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- workers.RunPending(context.Background()) }()
	<-started
	if err := tasks.Cancel(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("取消运行中 HLS preview 失败: %v", err)
	}
	<-canceled
	if err := <-done; err != nil {
		t.Fatalf("取消后的 worker 应正常退出: %v", err)
	}
	if err := tasks.Retry(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("重试已取消 HLS preview 失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行重试 HLS preview 失败: %v", err)
	}
	got, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("读取重试任务失败: %v", err)
	}
	if got.Status != models.TaskStatusSucceeded || calls != 2 {
		t.Fatalf("取消重试终态异常: task=%+v calls=%d", got, calls)
	}
}
