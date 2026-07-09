package library

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

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
