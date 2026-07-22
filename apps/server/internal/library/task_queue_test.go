package library

import (
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

// errTest 执行失败桩错误，供错误流转测试使用。
var errTest = errors.New("扫描执行失败（测试桩）")

// newTaskQueueDB 创建带 ScanTask 表的单连接内存库（与生产 WAL「写串行」语义一致）。
func newTaskQueueDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.ScanTask{}, &models.LibraryPath{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return gdb
}

// newConcurrentTaskQueueDB 创建允许并发连接的 WAL 文件库，用于确定性复现领取与取消交错。
func newConcurrentTaskQueueDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "scan-queue.db")
	gdb, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gdb.AutoMigrate(&models.ScanTask{}, &models.LibraryPath{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return gdb
}

// waitFor 轮询断言条件在 timeout 内成立，避免对固定时长 sleep 的脆弱依赖。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
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

// TestTaskQueue_CancelBetweenReadAndClaimKeepsCanceledAndContinues 确定性复现 worker 读到候选后取消先提交的交错。
func TestTaskQueue_CancelBetweenReadAndClaimKeepsCanceledAndContinues(t *testing.T) {
	gdb := newConcurrentTaskQueueDB(t)
	claimReady := make(chan struct{})
	releaseClaim := make(chan struct{})
	var intercepted atomic.Bool
	if err := gdb.Callback().Update().Before("gorm:update").Register("test:block_scan_claim", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "scan_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["status"] != models.ScanTaskStatusRunning || !intercepted.CompareAndSwap(false, true) {
			return
		}
		close(claimReady)
		<-releaseClaim
	}); err != nil {
		t.Fatalf("注册领取拦截器失败: %v", err)
	}

	executed := make(chan int64, 2)
	q := NewTaskQueue(gdb, func(libraryID int64, _, _, _ string) (int, error) {
		executed <- libraryID
		return 1, nil
	}).WithAudit(audit.NewRecorder(gdb))
	firstID, err := q.EnqueueInSpace("space-a", 1, "/first", "local", models.ScanTypeFull)
	if err != nil {
		t.Fatalf("首个任务入队失败: %v", err)
	}
	secondID, err := q.EnqueueInSpace("space-a", 2, "/second", "local", models.ScanTypeFull)
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

	waitFor(t, 2*time.Second, func() bool {
		var first, second models.ScanTask
		return gdb.First(&first, firstID).Error == nil &&
			gdb.First(&second, secondID).Error == nil &&
			first.Status == models.ScanTaskStatusCanceled &&
			second.Status == models.ScanTaskStatusCompleted
	})
	select {
	case libraryID := <-executed:
		if libraryID != 2 {
			t.Fatalf("worker 不得执行已取消任务，实际执行 libraryID=%d", libraryID)
		}
	default:
		t.Fatal("worker 领取冲突后应继续执行下一个 pending 任务")
	}
	select {
	case libraryID := <-executed:
		t.Fatalf("worker 只应执行第二个任务，额外执行 libraryID=%d", libraryID)
	default:
	}

	var canceledCount, firstSucceededCount int64
	gdb.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "task.canceled", fmt.Sprintf("%d", firstID)).Count(&canceledCount)
	gdb.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "task.succeeded", fmt.Sprintf("%d", firstID)).Count(&firstSucceededCount)
	if canceledCount != 1 || firstSucceededCount != 0 {
		t.Fatalf("并发取消审计语义异常: canceled=%d first_succeeded=%d", canceledCount, firstSucceededCount)
	}
}

// TestTaskQueue_ClaimBetweenCancelReadAndUpdateRejectsCancel 确定性复现取消读取后由 worker 先领取的交错。
func TestTaskQueue_ClaimBetweenCancelReadAndUpdateRejectsCancel(t *testing.T) {
	gdb := newConcurrentTaskQueueDB(t)
	cancelReady := make(chan struct{})
	releaseCancel := make(chan struct{})
	var intercepted atomic.Bool
	if err := gdb.Callback().Update().Before("gorm:update").Register("test:block_scan_cancel", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "scan_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok || updates["status"] != models.ScanTaskStatusCanceled || !intercepted.CompareAndSwap(false, true) {
			return
		}
		close(cancelReady)
		<-releaseCancel
	}); err != nil {
		t.Fatalf("注册取消拦截器失败: %v", err)
	}

	q := NewTaskQueue(gdb, func(int64, string, string, string) (int, error) { return 1, nil }).WithAudit(audit.NewRecorder(gdb))
	taskID, err := q.EnqueueInSpace("space-a", 1, "/first", "local", models.ScanTypeFull)
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
	if err := <-cancelResult; err == nil || err.Error() != "仅 pending 扫描任务可取消" {
		t.Fatalf("任务已被领取后应返回稳定状态错误，实际 %v", err)
	}

	var task models.ScanTask
	if err := gdb.First(&task, taskID).Error; err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	if task.Status != models.ScanTaskStatusRunning || task.CompletedAt != nil {
		t.Fatalf("取消失败不得覆盖 running 状态: %+v", task)
	}
	var canceledCount int64
	gdb.Model(&models.AuditEvent{}).Where("action = ? AND resource_id = ?", "task.canceled", fmt.Sprintf("%d", taskID)).Count(&canceledCount)
	if canceledCount != 0 {
		t.Fatalf("取消 CAS 失败不得写取消审计，实际 %d 条", canceledCount)
	}
}

// TestTaskQueue_EnqueueExecuteFlow 入队→running→completed 状态流转，记录 scanned_files。
func TestTaskQueue_EnqueueExecuteFlow(t *testing.T) {
	gdb := newTaskQueueDB(t)

	var gotMode atomic.Value
	exec := func(_ int64, _, _, mode string) (int, error) {
		gotMode.Store(mode)
		return 7, nil
	}
	q := NewTaskQueue(gdb, exec)
	q.Start()
	defer q.Stop()

	id, err := q.Enqueue(1, "/data", "local", models.ScanTypeFull)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if id == 0 {
		t.Fatal("入队应返回非零任务 ID")
	}

	waitFor(t, 2*time.Second, func() bool {
		var task models.ScanTask
		if err := gdb.First(&task, id).Error; err != nil {
			return false
		}
		return task.Status == models.ScanTaskStatusCompleted
	})

	var task models.ScanTask
	if err := gdb.First(&task, id).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if task.ScannedFiles != 7 {
		t.Fatalf("完成任务应记录 scanned_files=7, 实际 %d", task.ScannedFiles)
	}
	if task.StartedAt == nil || task.CompletedAt == nil {
		t.Fatal("完成任务应记录 started_at 与 completed_at")
	}
	// 锁定 FR-27↔FR-29 对接：worker 须把任务的 scan_type 作为 mode 透传给执行函数
	if got, _ := gotMode.Load().(string); got != models.ScanTypeFull {
		t.Fatalf("worker 应把 scan_type 作为 mode 透传, 期望 %q, 实际 %q", models.ScanTypeFull, got)
	}
}

// TestTaskQueue_ErrorFlow 执行出错置 error 并记录错误信息。
func TestTaskQueue_ErrorFlow(t *testing.T) {
	gdb := newTaskQueueDB(t)

	exec := func(_ int64, _, _, _ string) (int, error) {
		return 0, errTest
	}
	q := NewTaskQueue(gdb, exec)
	q.Start()
	defer q.Stop()

	id, err := q.Enqueue(2, "/x", "local", models.ScanTypeFull)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		var task models.ScanTask
		if err := gdb.First(&task, id).Error; err != nil {
			return false
		}
		return task.Status == models.ScanTaskStatusError
	})

	var task models.ScanTask
	_ = gdb.First(&task, id).Error
	if task.Error == "" {
		t.Fatal("出错任务应记录 error 信息")
	}
}

func TestTaskQueue_SuccessCallbackRunsOnlyAfterSuccessfulScan(t *testing.T) {
	gdb := newTaskQueueDB(t)
	scanFinished := make(chan struct{})
	callbackCalled := make(chan models.ScanTask, 1)
	release := make(chan struct{})
	q := NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		<-release
		close(scanFinished)
		return 1, nil
	}).WithSuccessCallback(func(task models.ScanTask) {
		select {
		case <-scanFinished:
			callbackCalled <- task
		default:
			t.Error("扫描成功回调不得早于扫描完成")
		}
	})
	q.Start()
	defer q.Stop()

	if _, err := q.EnqueueInSpace("space-a", 3, "/data", "local", models.ScanTypeFull); err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	select {
	case <-callbackCalled:
		t.Fatal("扫描尚未完成时不应触发成功回调")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	select {
	case task := <-callbackCalled:
		if task.SpaceID != "space-a" || task.LibraryID != 3 || task.Status != models.ScanTaskStatusCompleted {
			t.Fatalf("成功回调任务上下文不正确: %+v", task)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("扫描成功后未触发回调")
	}
}

func TestTaskQueue_SuccessCallbackSkipsFailedScan(t *testing.T) {
	gdb := newTaskQueueDB(t)
	var callbacks int32
	q := NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		return 0, errTest
	}).WithSuccessCallback(func(models.ScanTask) {
		atomic.AddInt32(&callbacks, 1)
	})
	q.Start()
	defer q.Stop()

	id, err := q.Enqueue(4, "/failed", "local", models.ScanTypeFull)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool {
		var task models.ScanTask
		return gdb.First(&task, id).Error == nil && task.Status == models.ScanTaskStatusError
	})
	if got := atomic.LoadInt32(&callbacks); got != 0 {
		t.Fatalf("失败扫描不应触发成功回调，实际 %d 次", got)
	}
}

// TestTaskQueue_SerialExecution 多任务排队时 worker 串行执行，任意时刻至多一个 running。
func TestTaskQueue_SerialExecution(t *testing.T) {
	gdb := newTaskQueueDB(t)

	var current int32
	var peak int32
	var mu sync.Mutex
	exec := func(_ int64, _, _, _ string) (int, error) {
		c := atomic.AddInt32(&current, 1)
		mu.Lock()
		if c > peak {
			peak = c
		}
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return 1, nil
	}
	q := NewTaskQueue(gdb, exec)
	q.Start()
	defer q.Stop()

	const n = 5
	for i := 0; i < n; i++ {
		if _, err := q.Enqueue(int64(i+1), "/p", "local", models.ScanTypeFull); err != nil {
			t.Fatalf("入队失败: %v", err)
		}
	}

	waitFor(t, 5*time.Second, func() bool {
		var cnt int64
		gdb.Model(&models.ScanTask{}).Where("status = ?", models.ScanTaskStatusCompleted).Count(&cnt)
		return cnt == int64(n)
	})

	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak != 1 {
		t.Fatalf("worker 应串行执行（并发峰值应为 1），实际峰值 %d", gotPeak)
	}
}

// TestTaskQueue_RecoverRunning 重启恢复：残留 running 任务被重置为 pending 并重新执行。
func TestTaskQueue_RecoverRunning(t *testing.T) {
	gdb := newTaskQueueDB(t)

	// 重启恢复需按 library_id 反查目录路径，故预置一条媒体库目录记录
	lp := &models.LibraryPath{Path: "/data/lib9", Type: "local", Enabled: 1}
	if err := gdb.Create(lp).Error; err != nil {
		t.Fatalf("预置媒体库目录失败: %v", err)
	}

	// 预置一条「残留 running」任务（模拟上次进程崩溃时正在执行）
	started := time.Now().Add(-time.Minute)
	stale := &models.ScanTask{
		LibraryID: lp.ID,
		ScanType:  models.ScanTypeFull,
		Status:    models.ScanTaskStatusRunning,
		StartedAt: &started,
	}
	if err := gdb.Create(stale).Error; err != nil {
		t.Fatalf("预置残留任务失败: %v", err)
	}

	var executed int32
	exec := func(_ int64, _, _, _ string) (int, error) {
		atomic.AddInt32(&executed, 1)
		return 3, nil
	}
	q := NewTaskQueue(gdb, exec)
	// 先恢复残留任务，再启动 worker
	if err := q.RecoverRunning(); err != nil {
		t.Fatalf("恢复残留任务失败: %v", err)
	}
	q.Start()
	defer q.Stop()

	waitFor(t, 2*time.Second, func() bool {
		var task models.ScanTask
		if err := gdb.First(&task, stale.ID).Error; err != nil {
			return false
		}
		return task.Status == models.ScanTaskStatusCompleted
	})

	if atomic.LoadInt32(&executed) != 1 {
		t.Fatalf("残留 running 任务应被重新执行一次，实际执行 %d 次", executed)
	}
}

// TestTaskQueue_ListTasks 列出任务（最近在前）。
func TestTaskQueue_ListTasks(t *testing.T) {
	gdb := newTaskQueueDB(t)
	exec := func(_ int64, _, _, _ string) (int, error) { return 0, nil }
	q := NewTaskQueue(gdb, exec)
	// 不启动 worker，直接观察入队后的 pending 列表
	_, _ = q.Enqueue(1, "/a", "local", models.ScanTypeFull)
	_, _ = q.Enqueue(2, "/b", "local", models.ScanTypeIncremental)

	tasks, err := q.ListTasks()
	if err != nil {
		t.Fatalf("列出任务失败: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("应有 2 条任务, 实际 %d", len(tasks))
	}
	// 最近入队的在最前
	if tasks[0].LibraryID != 2 {
		t.Fatalf("列表应按入队时间倒序, 首条 LibraryID 期望 2, 实际 %d", tasks[0].LibraryID)
	}
}

// TestTaskQueue_EnqueueChangeExecutesChangeTarget 验证 watcher 事件进入扫描队列后，
// worker 执行的是单路径变更而不是整库扫描。
func TestTaskQueue_EnqueueChangeExecutesChangeTarget(t *testing.T) {
	gdb := newTaskQueueDB(t)

	execCalled := int32(0)
	var gotChange ScanChange
	q := NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		atomic.AddInt32(&execCalled, 1)
		return 0, nil
	}).WithChangeExec(func(change ScanChange) (int, error) {
		gotChange = change
		return 1, nil
	})
	q.Start()
	defer q.Stop()

	id, err := q.EnqueueChange(ScanChange{
		SpaceID:   models.DefaultSpaceID,
		LibraryID: 9,
		Path:      "D:/media/a.mp4",
		Op:        ScanChangeModified,
	})
	if err != nil {
		t.Fatalf("变更入队失败: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		var task models.ScanTask
		if err := gdb.First(&task, id).Error; err != nil {
			return false
		}
		return task.Status == models.ScanTaskStatusCompleted
	})
	if atomic.LoadInt32(&execCalled) != 0 {
		t.Fatal("变更任务不应执行整库扫描函数")
	}
	if gotChange.Op != ScanChangeModified || gotChange.LibraryID != 9 || gotChange.Path != "D:/media/a.mp4" {
		t.Fatalf("变更任务参数不正确: %+v", gotChange)
	}
}

// TestTaskQueue_RestartedChangeTaskExecutesFromPayload 验证变更任务重启后
// 仍能从持久化 payload 还原执行目标，而不是依赖进程内内存映射。
func TestTaskQueue_RestartedChangeTaskExecutesFromPayload(t *testing.T) {
	gdb := newTaskQueueDB(t)
	change := ScanChange{
		SpaceID:   models.DefaultSpaceID,
		LibraryID: 7,
		Path:      filepath.ToSlash("D:/Videos/restart.mp4"),
		Op:        ScanChangeModified,
	}
	q1 := NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		t.Fatal("变更任务不应走整库扫描执行器")
		return 0, nil
	})
	if _, err := q1.EnqueueChange(change); err != nil {
		t.Fatalf("变更入队失败: %v", err)
	}

	var got atomic.Value
	q2 := NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		t.Fatal("变更任务不应走整库扫描执行器")
		return 0, nil
	}).WithChangeExec(func(change ScanChange) (int, error) {
		got.Store(change)
		return 1, nil
	})
	q2.Start()
	defer q2.Stop()

	waitFor(t, 2*time.Second, func() bool {
		var task models.ScanTask
		if err := gdb.First(&task).Error; err != nil {
			return false
		}
		return task.Status == models.ScanTaskStatusCompleted
	})
	restored, ok := got.Load().(ScanChange)
	if !ok {
		t.Fatal("未执行变更任务")
	}
	if restored.LibraryID != change.LibraryID || restored.Path != change.Path || restored.Op != change.Op {
		t.Fatalf("恢复的变更不正确: %+v", restored)
	}
}
