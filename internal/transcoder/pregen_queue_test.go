package transcoder

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
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

// newConcurrentPregenTestDB 创建允许并发连接的 WAL 文件库，用于确定性复现领取与取消交错。
func newConcurrentPregenTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pregen-queue.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.TranscodeTask{}, &models.AuditEvent{}); err != nil {
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

// TestPregenQueue_CancelBetweenReadAndClaimKeepsCanceledAndContinues 确定性复现 worker 读到候选后取消先提交的交错。
func TestPregenQueue_CancelBetweenReadAndClaimKeepsCanceledAndContinues(t *testing.T) {
	db := newConcurrentPregenTestDB(t)
	claimReady := make(chan struct{})
	releaseClaim := make(chan struct{})
	var intercepted atomic.Bool
	if err := db.Callback().Update().Before("gorm:update").Register("test:block_pregen_claim", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "transcode_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["status"] != models.TranscodeTaskStatusRunning || !intercepted.CompareAndSwap(false, true) {
			return
		}
		close(claimReady)
		<-releaseClaim
	}); err != nil {
		t.Fatalf("注册领取拦截器失败: %v", err)
	}

	executed := make(chan int64, 2)
	q := NewPregenQueue(db, func(_ string, mediaID int64, _ string) error {
		executed <- mediaID
		return nil
	}).WithAudit(audit.NewRecorder(db))
	firstID, err := q.EnqueueInSpace("space-a", 1, 1, "h264", 0, 0)
	if err != nil {
		t.Fatalf("首个任务入队失败: %v", err)
	}
	secondID, err := q.EnqueueInSpace("space-a", 2, 1, "h264", 0, 0)
	if err != nil {
		t.Fatalf("第二个任务入队失败: %v", err)
	}
	q.Start()
	defer q.Stop()

	select {
	case <-claimReady:
	case <-time.After(2 * time.Second):
		t.Fatal("worker 未进入首个任务领取窗口")
	}
	if err := q.CancelTaskInSpace("space-a", firstID); err != nil {
		t.Fatalf("并发取消首个任务失败: %v", err)
	}
	close(releaseClaim)

	waitForPregen(t, 2*time.Second, func() bool {
		var first, second models.TranscodeTask
		return db.First(&first, firstID).Error == nil &&
			db.First(&second, secondID).Error == nil &&
			first.Status == models.TranscodeTaskStatusCanceled &&
			second.Status == models.TranscodeTaskStatusCompleted
	})
	select {
	case mediaID := <-executed:
		if mediaID != 2 {
			t.Fatalf("worker 不得执行已取消任务，实际执行 mediaID=%d", mediaID)
		}
	default:
		t.Fatal("worker 领取冲突后应继续执行下一个 pending 任务")
	}
	select {
	case mediaID := <-executed:
		t.Fatalf("worker 只应执行第二个任务，额外执行 mediaID=%d", mediaID)
	default:
	}

	var canceledCount, firstSucceededCount int64
	db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "task.canceled", fmt.Sprintf("%d", firstID)).Count(&canceledCount)
	db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "task.succeeded", fmt.Sprintf("%d", firstID)).Count(&firstSucceededCount)
	if canceledCount != 1 || firstSucceededCount != 0 {
		t.Fatalf("并发取消审计语义异常: canceled=%d first_succeeded=%d", canceledCount, firstSucceededCount)
	}
}

// TestPregenQueue_ClaimBetweenCancelReadAndUpdateRejectsCancel 确定性复现取消读取后由 worker 先领取的交错。
func TestPregenQueue_ClaimBetweenCancelReadAndUpdateRejectsCancel(t *testing.T) {
	db := newConcurrentPregenTestDB(t)
	cancelReady := make(chan struct{})
	releaseCancel := make(chan struct{})
	var intercepted atomic.Bool
	if err := db.Callback().Update().Before("gorm:update").Register("test:block_pregen_cancel", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "transcode_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["status"] != models.TranscodeTaskStatusCanceled || !intercepted.CompareAndSwap(false, true) {
			return
		}
		close(cancelReady)
		<-releaseCancel
	}); err != nil {
		t.Fatalf("注册取消拦截器失败: %v", err)
	}

	q := NewPregenQueue(db, func(string, int64, string) error { return nil }).WithAudit(audit.NewRecorder(db))
	taskID, err := q.EnqueueInSpace("space-a", 1, 1, "h264", 0, 0)
	if err != nil {
		t.Fatalf("任务入队失败: %v", err)
	}
	cancelResult := make(chan error, 1)
	go func() { cancelResult <- q.CancelTaskInSpace("space-a", taskID) }()
	select {
	case <-cancelReady:
	case <-time.After(2 * time.Second):
		t.Fatal("取消操作未进入 CAS 更新窗口")
	}
	claimed, ok := q.nextPending()
	if !ok || claimed.ID != taskID {
		t.Fatal("worker 应在取消 CAS 前成功领取 pending 任务")
	}
	close(releaseCancel)
	if err := <-cancelResult; err == nil || err.Error() != "仅 pending 预生成任务可取消" {
		t.Fatalf("任务已被领取后应返回稳定状态错误，实际 %v", err)
	}

	var task models.TranscodeTask
	if err := db.First(&task, taskID).Error; err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	if task.Status != models.TranscodeTaskStatusRunning || task.CompletedAt != nil {
		t.Fatalf("取消失败不得覆盖 running 状态: %+v", task)
	}
	var canceledCount int64
	db.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "task.canceled", fmt.Sprintf("%d", taskID)).Count(&canceledCount)
	if canceledCount != 0 {
		t.Fatalf("取消 CAS 失败不得写取消审计，实际 %d 条", canceledCount)
	}
}

// TestPregenQueue_EnqueueExecuteFlow 入队→running→completed 状态流转，exec 按任务快照编码调用。
func TestPregenQueue_EnqueueExecuteFlow(t *testing.T) {
	db := newPregenTestDB(t)

	var gotMedia, gotCalls atomic.Int64
	var gotCodec, gotSpace atomic.Value
	exec := func(spaceID string, mediaID int64, codec string) error {
		gotSpace.Store(spaceID)
		gotMedia.Store(mediaID)
		gotCodec.Store(codec)
		gotCalls.Add(1)
		return nil
	}
	q := NewPregenQueue(db, exec)
	q.Start()
	defer q.Stop()

	id, err := q.EnqueueInSpace("space-other", 42, 1, "h265", 1920, 1080)
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
	if spaceID, _ := gotSpace.Load().(string); spaceID != "space-other" {
		t.Fatalf("exec 应收到任务 Space space-other，实际 %q", spaceID)
	}
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

	exec := func(_ string, _ int64, _ string) error {
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
	exec := func(_ string, _ int64, _ string) error {
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
func TestPregenQueue_ExistingCapabilityCacheLoadedBeforeRecoveredTask(t *testing.T) {
	if !IsFFmpegAvailable() {
		t.Skip("ffmpeg 不可用，跳过启动缓存加载测试")
	}
	db := newPregenTestDB(t)
	if err := db.AutoMigrate(&models.CodecProbeCache{}); err != nil {
		t.Fatalf("迁移能力缓存表失败: %v", err)
	}
	version := FFmpegVersion(context.Background())
	writeCacheForCurrentVersion(t, db, version, []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	})
	setProbeSnapshot(nil)
	t.Cleanup(func() { probeSnapshot.Store(nil) })

	started := time.Now().Add(-time.Minute)
	stale := &models.TranscodeTask{
		MediaID:   6,
		PresetID:  1,
		Codec:     "h264",
		Status:    models.TranscodeTaskStatusRunning,
		StartedAt: &started,
	}
	if err := db.Create(stale).Error; err != nil {
		t.Fatalf("预置残留任务失败: %v", err)
	}

	svc := NewCapabilityService(db)
	if err := svc.LoadCachedSnapshot(context.Background()); err != nil {
		t.Fatalf("同步加载能力缓存失败: %v", err)
	}
	var selected atomic.Value
	q := NewPregenQueue(db, func(_ string, _ int64, codec string) error {
		pipeline, err := NewPipelineForCodecWithPolicy(codec, DefaultHardwarePolicy())
		if err == nil {
			selected.Store(pipeline.encoderName)
		}
		return err
	})
	if err := q.RecoverRunning(); err != nil {
		t.Fatalf("恢复残留任务失败: %v", err)
	}
	q.Start()
	defer q.Stop()

	waitForPregen(t, 2*time.Second, func() bool {
		var task models.TranscodeTask
		return db.First(&task, stale.ID).Error == nil && task.Status == models.TranscodeTaskStatusCompleted
	})
	if got, _ := selected.Load().(string); got != "h264_amf" {
		t.Fatalf("恢复任务启动前应已加载缓存能力，实际编码器 %q", got)
	}
}

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
	exec := func(_ string, _ int64, _ string) error {
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
	exec := func(_ string, _ int64, _ string) error { return nil }
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
	q := NewPregenQueue(db, func(string, int64, string) error { return nil })

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
