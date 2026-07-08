package transcoder

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newPregenTestDB 创建带 TranscodeTask 表的单连接内存库。
func newPregenTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.TranscodeTask{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

// waitForPregen 轮询断言条件在 timeout 内成立。
func waitForPregen(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待条件超时（%s）", timeout)
}

// TestPregenQueue_EnqueueExecuteFlow 入队→running→completed 状态流转，exec 按任务快照编码调用。
func TestPregenQueue_EnqueueExecuteFlow(t *testing.T) {
	db := newPregenTestDB(t)

	var gotMedia, gotCalls atomic.Int64
	var gotCodec atomic.Value
	exec := func(mediaID int64, codec string) error {
		gotMedia.Store(mediaID)
		gotCodec.Store(codec)
		gotCalls.Add(1)
		return nil
	}
	q := NewPregenQueue(db, exec)
	q.Start()
	defer q.Stop()

	id, err := q.Enqueue(42, 1, "h265", 1920, 1080)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if id == 0 {
		t.Fatal("入队应返回非零任务 ID")
	}

	waitForPregen(t, 2*time.Second, func() bool {
		var task models.TranscodeTask
		if err := db.First(&task, id).Error; err != nil {
			return false
		}
		return task.Status == models.TranscodeTaskStatusCompleted
	})

	// 断言 exec 按任务快照的 media_id/codec 被调用（预生成按 preset codec 调 PreSliceWithCodec 的契约）
	if gotMedia.Load() != 42 {
		t.Fatalf("exec 应按任务 media_id=42 调用, 实际 %d", gotMedia.Load())
	}
	if c, _ := gotCodec.Load().(string); c != "h265" {
		t.Fatalf("exec 应按任务快照编码 h265 调用, 实际 %q", c)
	}
	if gotCalls.Load() != 1 {
		t.Fatalf("exec 应只调用一次, 实际 %d", gotCalls.Load())
	}

	var task models.TranscodeTask
	_ = db.First(&task, id).Error
	if task.StartedAt == nil || task.CompletedAt == nil {
		t.Fatal("完成任务应记录 started_at 与 completed_at")
	}
}

// TestPregenQueue_ErrorFlow 执行出错置 error 并记录错误信息。
func TestPregenQueue_ErrorFlow(t *testing.T) {
	db := newPregenTestDB(t)

	exec := func(_ int64, _ string) error {
		return errors.New("预转码失败（测试桩）")
	}
	q := NewPregenQueue(db, exec)
	q.Start()
	defer q.Stop()

	id, _ := q.Enqueue(7, 1, "av1", 0, 0)

	waitForPregen(t, 2*time.Second, func() bool {
		var task models.TranscodeTask
		if err := db.First(&task, id).Error; err != nil {
			return false
		}
		return task.Status == models.TranscodeTaskStatusError
	})

	var task models.TranscodeTask
	_ = db.First(&task, id).Error
	if task.Error == "" {
		t.Fatal("出错任务应记录 error 信息")
	}
}

// TestPregenQueue_SerialExecution 多任务排队时 worker 串行执行（并发峰值为 1）。
func TestPregenQueue_SerialExecution(t *testing.T) {
	db := newPregenTestDB(t)

	var current, peak int32
	var mu sync.Mutex
	exec := func(_ int64, _ string) error {
		c := atomic.AddInt32(&current, 1)
		mu.Lock()
		if c > peak {
			peak = c
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return nil
	}
	q := NewPregenQueue(db, exec)
	q.Start()
	defer q.Stop()

	const n = 5
	for i := 0; i < n; i++ {
		if _, err := q.Enqueue(int64(i+1), 1, "h264", 0, 0); err != nil {
			t.Fatalf("入队失败: %v", err)
		}
	}

	waitForPregen(t, 5*time.Second, func() bool {
		var cnt int64
		db.Model(&models.TranscodeTask{}).Where("status = ?", models.TranscodeTaskStatusCompleted).Count(&cnt)
		return cnt == int64(n)
	})

	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak != 1 {
		t.Fatalf("worker 应串行执行（并发峰值应为 1），实际峰值 %d", gotPeak)
	}
}

// TestPregenQueue_RecoverRunning 重启恢复：残留 running 被重置为 pending 并重新执行。
func TestPregenQueue_RecoverRunning(t *testing.T) {
	db := newPregenTestDB(t)

	started := time.Now().Add(-time.Minute)
	stale := &models.TranscodeTask{
		MediaID:   5,
		PresetID:  1,
		Codec:     "h264",
		Status:    models.TranscodeTaskStatusRunning,
		StartedAt: &started,
	}
	if err := db.Create(stale).Error; err != nil {
		t.Fatalf("预置残留任务失败: %v", err)
	}

	var executed int32
	exec := func(_ int64, _ string) error {
		atomic.AddInt32(&executed, 1)
		return nil
	}
	q := NewPregenQueue(db, exec)
	if err := q.RecoverRunning(); err != nil {
		t.Fatalf("恢复残留任务失败: %v", err)
	}
	q.Start()
	defer q.Stop()

	waitForPregen(t, 2*time.Second, func() bool {
		var task models.TranscodeTask
		if err := db.First(&task, stale.ID).Error; err != nil {
			return false
		}
		return task.Status == models.TranscodeTaskStatusCompleted
	})

	if atomic.LoadInt32(&executed) != 1 {
		t.Fatalf("残留 running 任务应被重新执行一次，实际执行 %d 次", executed)
	}
}

// TestPregenQueue_ListTasksFilter 列任务支持按状态过滤、按入队倒序。
func TestPregenQueue_ListTasksFilter(t *testing.T) {
	db := newPregenTestDB(t)
	exec := func(_ int64, _ string) error { return nil }
	q := NewPregenQueue(db, exec)
	// 不启动 worker，观察 pending 列表
	_, _ = q.Enqueue(1, 1, "h264", 0, 0)
	_, _ = q.Enqueue(2, 1, "h265", 0, 0)

	all, err := q.ListTasks("")
	if err != nil {
		t.Fatalf("列任务失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("应有 2 条任务, 实际 %d", len(all))
	}
	if all[0].MediaID != 2 {
		t.Fatalf("列表应按入队倒序, 首条 MediaID 期望 2, 实际 %d", all[0].MediaID)
	}

	pending, _ := q.ListTasks(models.TranscodeTaskStatusPending)
	if len(pending) != 2 {
		t.Fatalf("pending 过滤应有 2 条, 实际 %d", len(pending))
	}
	completed, _ := q.ListTasks(models.TranscodeTaskStatusCompleted)
	if len(completed) != 0 {
		t.Fatalf("completed 过滤应为 0 条, 实际 %d", len(completed))
	}
}

func TestPregenQueue_CancelAndRetry(t *testing.T) {
	db := newPregenTestDB(t)
	q := NewPregenQueue(db, func(int64, string) error { return nil })

	id, err := q.EnqueueInSpace(models.DefaultSpaceID, 1, 2, "h264", 0, 0)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if err := q.CancelTaskInSpace(models.DefaultSpaceID, id); err != nil {
		t.Fatalf("取消任务失败: %v", err)
	}
	var canceled models.TranscodeTask
	if err := db.First(&canceled, id).Error; err != nil {
		t.Fatalf("读取取消任务失败: %v", err)
	}
	if canceled.Status != models.TranscodeTaskStatusCanceled || canceled.CompletedAt == nil {
		t.Fatalf("取消后状态异常: %+v", canceled)
	}

	if err := q.RetryTaskInSpace(models.DefaultSpaceID, id); err != nil {
		t.Fatalf("重试任务失败: %v", err)
	}
	var retried models.TranscodeTask
	if err := db.First(&retried, id).Error; err != nil {
		t.Fatalf("读取重试任务失败: %v", err)
	}
	if retried.Status != models.TranscodeTaskStatusPending || retried.CompletedAt != nil || retried.Error != "" {
		t.Fatalf("重试后状态异常: %+v", retried)
	}
}
