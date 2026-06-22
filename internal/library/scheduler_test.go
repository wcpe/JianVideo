package library

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// TestScanScheduler_FiresOnInterval 周期 >0 时按周期多次触发。
func TestScanScheduler_FiresOnInterval(t *testing.T) {
	var fires int32
	sch := NewScanScheduler(
		func() time.Duration { return 15 * time.Millisecond },
		func() { atomic.AddInt32(&fires, 1) },
	)
	sch.Start()
	defer sch.Stop()

	// 150ms 内 15ms 周期应至少触发数次
	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&fires) >= 3 })
}

// TestScanScheduler_DisabledWhenIntervalZero 周期 <=0 时不触发。
func TestScanScheduler_DisabledWhenIntervalZero(t *testing.T) {
	var fires int32
	sch := NewScanScheduler(
		func() time.Duration { return 0 },
		func() { atomic.AddInt32(&fires, 1) },
	)
	sch.Start()
	defer sch.Stop()

	time.Sleep(80 * time.Millisecond)
	if got := atomic.LoadInt32(&fires); got != 0 {
		t.Fatalf("周期为 0 应不触发, 实际触发 %d 次", got)
	}
}

// TestScanScheduler_ReloadAppliesNewInterval 初始关闭，改小周期 + Reload 后开始触发。
func TestScanScheduler_ReloadAppliesNewInterval(t *testing.T) {
	var fires int32
	var intervalNs atomic.Int64 // 当前周期（纳秒），初始 0 表示关闭
	sch := NewScanScheduler(
		func() time.Duration { return time.Duration(intervalNs.Load()) },
		func() { atomic.AddInt32(&fires, 1) },
	)
	sch.Start()
	defer sch.Stop()

	// 关闭态：稍等不应触发
	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&fires); got != 0 {
		t.Fatalf("关闭态不应触发, 实际 %d 次", got)
	}

	// 改为小周期并热重载
	intervalNs.Store(int64(15 * time.Millisecond))
	sch.Reload()

	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&fires) >= 2 })
}

// TestScanScheduler_StopHalts 停止后不再触发。
func TestScanScheduler_StopHalts(t *testing.T) {
	var fires int32
	sch := NewScanScheduler(
		func() time.Duration { return 15 * time.Millisecond },
		func() { atomic.AddInt32(&fires, 1) },
	)
	sch.Start()
	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&fires) >= 1 })
	sch.Stop()

	settled := atomic.LoadInt32(&fires)
	time.Sleep(80 * time.Millisecond)
	if got := atomic.LoadInt32(&fires); got > settled+1 {
		// 允许停止瞬间至多一次在途触发，之后不应再增长
		t.Fatalf("停止后不应继续触发, 停止时 %d, 之后 %d", settled, got)
	}
}

// TestScanScheduler_StartIdempotent 重复 Start 安全且仍只有一个循环在跑。
func TestScanScheduler_StartIdempotent(t *testing.T) {
	var fires int32
	sch := NewScanScheduler(
		func() time.Duration { return 30 * time.Millisecond },
		func() { atomic.AddInt32(&fires, 1) },
	)
	sch.Start()
	sch.Start() // 第二次应为空操作，不应再起一个循环
	defer sch.Stop()

	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&fires) >= 1 })
}

// TestTaskQueue_EnqueueScheduled 全库入队：跳过禁用库与已有活动任务的库，返回入队数量。
func TestTaskQueue_EnqueueScheduled(t *testing.T) {
	gdb := newTaskQueueDB(t)
	exec := func(libraryID int64, path, dirType, mode string) (int, error) { return 0, nil }
	q := NewTaskQueue(gdb, exec)
	// 不启动 worker，纯观察入队结果

	// 预置一条已有 pending 任务的库（应被跳过）
	if err := gdb.Create(&models.ScanTask{LibraryID: 1, ScanType: models.ScanTypeIncremental, Status: models.ScanTaskStatusPending}).Error; err != nil {
		t.Fatalf("预置活动任务失败: %v", err)
	}

	libs := []models.LibraryPath{
		{ID: 1, Path: "/a", Type: "local", Enabled: 1}, // 已有活动任务 → 跳过
		{ID: 2, Path: "/b", Type: "local", Enabled: 1}, // 入队
		{ID: 3, Path: "/c", Type: "local", Enabled: 0}, // 禁用 → 跳过
		{ID: 4, Path: "/d", Type: "smb", Enabled: 1},   // 入队（类型不限）
	}

	n := q.EnqueueScheduled(libs, models.ScanTypeIncremental)
	if n != 2 {
		t.Fatalf("应入队 2 个库（跳过禁用与活动任务）, 实际 %d", n)
	}

	// 确认入队的是库 2 与库 4，且为增量
	var tasks []models.ScanTask
	if err := gdb.Where("status = ?", models.ScanTaskStatusPending).Order("library_id ASC").Find(&tasks).Error; err != nil {
		t.Fatalf("查询入队任务失败: %v", err)
	}
	// 含预置的库 1 共 3 条 pending
	if len(tasks) != 3 {
		t.Fatalf("应有 3 条 pending（含预置）, 实际 %d", len(tasks))
	}
	ids := map[int64]bool{}
	for _, tk := range tasks {
		ids[tk.LibraryID] = true
		if tk.LibraryID != 1 && tk.ScanType != models.ScanTypeIncremental {
			t.Fatalf("定时入队应为增量, 库 %d 实际 %s", tk.LibraryID, tk.ScanType)
		}
	}
	if !ids[2] || !ids[4] {
		t.Fatalf("库 2 与库 4 应被入队, 实际入队集合 %v", ids)
	}
}
