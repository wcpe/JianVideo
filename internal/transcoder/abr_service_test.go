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

func newABRTestService(t *testing.T, exec ABRExecFunc) (*ABRService, *tasksvc.Service, *tasksvc.WorkerRegistry, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "abr.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 ABR 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取 ABR 测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移 ABR 任务表失败: %v", err)
	}
	tasks := tasksvc.NewService(db)
	workers := tasksvc.NewWorkerRegistry(tasks)
	root := filepath.Join(t.TempDir(), "hls")
	service := NewABRService(tasks, workers, root, exec)
	if err := service.RegisterWorker(1); err != nil {
		t.Fatalf("注册 ABR worker 失败: %v", err)
	}
	return service, tasks, workers, root
}

func TestABRServiceEnqueueSnapshotsPayloadAndIsIdempotent(t *testing.T) {
	service, tasks, _, _ := newABRTestService(t, func(context.Context, int64, ABRPayload) error { return nil })
	request := ABRRequest{
		SpaceID: "space-a", MediaID: 42, SourceWidth: 1280, SourceHeight: 720,
		Ladder: []string{"1080p", "720p", "480p"}, Priority: 8, ForceRebuild: true,
		HWAccelPreference: "auto",
	}
	first, err := service.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatalf("ABR 入队失败: %v", err)
	}
	second, err := service.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatalf("ABR 幂等入队失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("活动 ABR 任务应幂等复用: first=%d second=%d", first.ID, second.ID)
	}
	if first.Type != TaskTypeHLSABR || first.Priority != 8 || first.MaxAttempts != ABRMaxAttempts {
		t.Fatalf("ABR 任务信封异常: %+v", first)
	}
	stored, err := tasks.Get(context.Background(), first.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil {
		t.Fatalf("读取 ABR 任务失败: %v", err)
	}
	var payload ABRPayload
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &payload); err != nil {
		t.Fatalf("解析 ABR payload 失败: %v", err)
	}
	if payload.ProfileID != ABRProfileID || payload.Codec != "h264" || payload.HWAccelPreference != "auto" || !payload.ForceRebuild {
		t.Fatalf("ABR payload 固定字段异常: %+v", payload)
	}
	if got := abrVariantNames(payload.Ladder); len(got) != 2 || got[0] != "720p" || got[1] != "480p" {
		t.Fatalf("ABR payload 应快照裁剪后 ladder: %+v", payload.Ladder)
	}
}

func TestABRServiceProgressRetryCancelAndStatus(t *testing.T) {
	calls := 0
	var root string
	started := make(chan struct{})
	service, tasks, workers, returnedRoot := newABRTestService(t, func(ctx context.Context, _ int64, payload ABRPayload) error {
		calls++
		if calls == 1 {
			return errors.New("首次 ABR 转码失败")
		}
		if calls == 2 {
			close(started)
			<-ctx.Done()
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
	task, err := service.Enqueue(context.Background(), ABRRequest{SpaceID: "space-a", MediaID: 7, SourceWidth: 1920, SourceHeight: 1080})
	if err != nil {
		t.Fatalf("ABR 入队失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行首次 ABR 尝试失败: %v", err)
	}
	pending, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil || pending.Status != models.TaskStatusPending || pending.Attempts != 1 {
		t.Fatalf("ABR 首次失败应进入退避: task=%+v err=%v", pending, err)
	}
	tasks.SetNowForTest(func() time.Time { return pending.NextRunAt.Add(time.Second) })
	done := make(chan error, 1)
	go func() { done <- workers.RunPending(context.Background()) }()
	<-started
	if err := tasks.Cancel(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("取消运行中 ABR 失败: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("取消后的 ABR worker 应正常退出: %v", err)
	}
	if err := tasks.Retry(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"}); err != nil {
		t.Fatalf("重试已取消 ABR 失败: %v", err)
	}
	if err := workers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行 ABR 重试失败: %v", err)
	}
	finished, err := tasks.Get(context.Background(), task.ID, tasksvc.Query{SpaceID: "space-a"})
	if err != nil || finished.Status != models.TaskStatusSucceeded || finished.Progress != 100 {
		t.Fatalf("ABR 重试终态异常: task=%+v err=%v", finished, err)
	}
	status, err := service.Status(context.Background(), "space-a", 7)
	if err != nil {
		t.Fatalf("查询 ABR 状态失败: %v", err)
	}
	if !status.Available || status.ProfileID != ABRProfileID || status.URL != "/api/play/hls/7/profiles/abr-h264/master.m3u8" {
		t.Fatalf("ABR 状态异常: %+v", status)
	}
}
