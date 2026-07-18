package tasks

import (
	"context"
	"errors"
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

func newTaskTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	return NewService(db), db
}

func TestNormalizeStatusMapsLegacyAndRejectsUnknown(t *testing.T) {
	cases := map[string]string{
		models.TaskStatusPending:   models.TaskStatusPending,
		models.TaskStatusRunning:   models.TaskStatusRunning,
		models.TaskStatusSucceeded: models.TaskStatusSucceeded,
		models.TaskStatusFailed:    models.TaskStatusFailed,
		models.TaskStatusCanceled:  models.TaskStatusCanceled,
		"completed":                models.TaskStatusSucceeded,
		"error":                    models.TaskStatusFailed,
	}
	for input, want := range cases {
		got, err := NormalizeStatus(input)
		if err != nil {
			t.Fatalf("状态 %q 不应报错: %v", input, err)
		}
		if got != want {
			t.Fatalf("状态 %q 映射错误: got=%s want=%s", input, got, want)
		}
	}
	if _, err := NormalizeStatus("done"); err == nil {
		t.Fatal("未知状态应被拒绝")
	}
}

func TestEnqueueReturnsExistingUnfinishedTaskForSameIdempotencyKey(t *testing.T) {
	svc, db := newTaskTestService(t)
	ctx := context.Background()
	input := EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        models.DefaultSpaceID,
		Type:           "library.scan",
		Priority:       5,
		IdempotencyKey: "scan:1",
	}

	first, err := svc.Enqueue(ctx, input)
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	second, err := svc.Enqueue(ctx, input)
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("重复幂等键应返回既有任务: first=%d second=%d", first.ID, second.ID)
	}
	if got := countTasks(t, db, "idempotency_key = ?", input.IdempotencyKey); got != 1 {
		t.Fatalf("未完成任务不应重复入库: %d", got)
	}

	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: input.Type})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}
	if err := svc.MarkSucceeded(ctx, claimed.ID); err != nil {
		t.Fatalf("标记成功失败: %v", err)
	}
	third, err := svc.Enqueue(ctx, input)
	if err != nil {
		t.Fatalf("终态后再次入队失败: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("既有任务完成后，同幂等键应允许新任务")
	}
	if got := countTasks(t, db, "idempotency_key = ?", input.IdempotencyKey); got != 2 {
		t.Fatalf("终态后应产生第二条任务: %d", got)
	}
}

func TestConcurrentEnqueueSameIdempotencyKeyCreatesOneUnfinishedTask(t *testing.T) {
	svc, db := newTaskTestService(t)
	ctx := context.Background()
	input := EnqueueInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        models.DefaultSpaceID,
		Type:           "thumbnail.generate",
		IdempotencyKey: "thumb:42",
	}

	const workers = 32
	ids := make(chan int64, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := svc.Enqueue(ctx, input)
			if err != nil {
				errs <- err
				return
			}
			ids <- task.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("并发入队失败: %v", err)
	}
	var first int64
	for id := range ids {
		if first == 0 {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("并发幂等入队应返回同一任务: first=%d got=%d", first, id)
		}
	}
	if got := countTasks(t, db, "idempotency_key = ?", input.IdempotencyKey); got != 1 {
		t.Fatalf("并发同幂等键只应产生一条未完成任务: %d", got)
	}
}

func TestEnqueueRetriesRepeatedWALSnapshotConflicts(t *testing.T) {
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "tasks-snapshot.db")) + "?_busy_timeout=1000&_journal_mode=WAL"
	dbA := openTaskWALDB(t, dsn)
	dbB := openTaskWALDB(t, dsn)
	if err := dbA.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}

	const (
		callbackName     = "test:task-enqueue-busy-snapshot"
		conflictAttempts = 4
	)
	var attempts atomic.Int32
	var injected atomic.Int32
	if err := dbA.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "tasks" {
			return
		}
		if attempts.Add(1) > conflictAttempts {
			return
		}
		injected.Add(1)
		now := time.Now().UTC()
		if err := dbB.Create(&models.Task{
			Scope: models.TaskScopeSystem, Type: "test.concurrent-write", Status: models.TaskStatusSucceeded,
			MaxAttempts: 1, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Errorf("注入 WAL 并发写失败: %v", err)
		}
	}); err != nil {
		t.Fatalf("注册 WAL 快照冲突回调失败: %v", err)
	}
	t.Cleanup(func() { _ = dbA.Callback().Query().Remove(callbackName) })

	service := NewService(dbA)
	task, err := service.Enqueue(context.Background(), EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: models.DefaultSpaceID, Type: "transcode.hls.abr",
		IdempotencyKey: "hls-abr:space-default:42", ResourceType: "media", ResourceID: "42",
	})
	if err != nil {
		t.Fatalf("连续 WAL 快照冲突后入队应自动重试: %v", err)
	}
	if injected.Load() != conflictAttempts || attempts.Load() != conflictAttempts+1 {
		t.Fatalf("连续 WAL 快照冲突应重试至成功: injected=%d attempts=%d", injected.Load(), attempts.Load())
	}
	if task == nil || task.ID == 0 {
		t.Fatalf("重试后应返回已创建任务: %+v", task)
	}
	if got := countTasks(t, dbA, "idempotency_key = ?", "hls-abr:space-default:42"); got != 1 {
		t.Fatalf("重试后只应创建一条目标任务: %d", got)
	}
}

func openTaskWALDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开任务 WAL 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取任务 WAL 底层数据库失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestSyncLegacyUpsertsAndMapsStatus(t *testing.T) {
	svc, db := newTaskTestService(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	input := LegacySyncInput{
		Scope:          models.TaskScopeSpace,
		SpaceID:        models.DefaultSpaceID,
		Type:           "library.scan",
		Status:         "completed",
		Progress:       80,
		IdempotencyKey: "scan:10",
		PayloadJSON:    `{"legacy_id":10}`,
		ResourceType:   "library",
		ResourceID:     "7",
		CreatedAt:      createdAt,
	}

	if err := svc.SyncLegacy(ctx, input); err != nil {
		t.Fatalf("首次同步旧任务失败: %v", err)
	}
	if err := svc.SyncLegacy(ctx, input); err != nil {
		t.Fatalf("重复同步旧任务失败: %v", err)
	}
	if got := countTasks(t, db, "idempotency_key = ?", input.IdempotencyKey); got != 1 {
		t.Fatalf("旧任务同步应按幂等键 upsert: %d", got)
	}
	page, err := svc.List(ctx, Query{SpaceID: models.DefaultSpaceID, Type: "library.scan"})
	if err != nil {
		t.Fatalf("查询同步任务失败: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != models.TaskStatusSucceeded || page.Items[0].Progress != 100 {
		t.Fatalf("旧 completed 应映射为 succeeded 且进度为 100: %+v", page.Items)
	}
}

func TestSpaceAndSystemQueriesAreIsolated(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	spaceTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:    models.TaskScopeSpace,
		SpaceID:  models.DefaultSpaceID,
		Type:     "library.scan",
		Priority: 1,
	})
	otherSpaceTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:    models.TaskScopeSpace,
		SpaceID:  "space-other",
		Type:     "library.scan",
		Priority: 1,
	})
	systemTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:    models.TaskScopeSystem,
		Type:     "tool.download",
		Priority: 1,
	})

	spacePage, err := svc.List(ctx, Query{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("查询 Space 任务失败: %v", err)
	}
	if len(spacePage.Items) != 1 || spacePage.Items[0].ID != spaceTask.ID {
		t.Fatalf("Space 查询应只返回当前 Space 任务: %+v", spacePage.Items)
	}
	systemPage, err := svc.List(ctx, Query{Scope: models.TaskScopeSystem})
	if err != nil {
		t.Fatalf("查询系统任务失败: %v", err)
	}
	if len(systemPage.Items) != 1 || systemPage.Items[0].ID != systemTask.ID {
		t.Fatalf("系统查询应只返回系统任务: %+v", systemPage.Items)
	}
	if _, err := svc.Get(ctx, otherSpaceTask.ID, Query{SpaceID: models.DefaultSpaceID}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("跨 Space 明细应不可见: %v", err)
	}
	if _, err := svc.Get(ctx, systemTask.ID, Query{SpaceID: models.DefaultSpaceID}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Space 明细默认不应返回系统任务: %v", err)
	}

	stats, err := svc.Stats(ctx, Query{SpaceID: models.DefaultSpaceID})
	if err != nil {
		t.Fatalf("统计 Space 任务失败: %v", err)
	}
	if stats.Total != 1 || stats.ByStatus[models.TaskStatusPending] != 1 {
		t.Fatalf("Space 统计应只包含当前 Space pending: %+v", stats)
	}
}

func TestCancelAndRetryTransitions(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	task := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: models.DefaultSpaceID,
		Type:    "library.scan",
	})

	if err := svc.Cancel(ctx, task.ID, Query{SpaceID: models.DefaultSpaceID}); err != nil {
		t.Fatalf("取消任务失败: %v", err)
	}
	canceled := mustGetTask(t, svc, task.ID, Query{SpaceID: models.DefaultSpaceID})
	if canceled.Status != models.TaskStatusCanceled || canceled.FinishedAt == nil {
		t.Fatalf("取消后状态异常: %+v", canceled)
	}

	if err := svc.Retry(ctx, task.ID, Query{SpaceID: models.DefaultSpaceID}); err != nil {
		t.Fatalf("重试任务失败: %v", err)
	}
	retried := mustGetTask(t, svc, task.ID, Query{SpaceID: models.DefaultSpaceID})
	if retried.Status != models.TaskStatusPending || retried.FinishedAt != nil || retried.Error != "" {
		t.Fatalf("重试后应回到 pending 并清理终态字段: %+v", retried)
	}
}

func TestProgressSucceededAndFailedTransitions(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	successTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "transcode.hls",
		MaxAttempts: 1,
	})
	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: "transcode.hls"})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}
	if claimed.ID != successTask.ID || claimed.Status != models.TaskStatusRunning || claimed.StartedAt == nil {
		t.Fatalf("领取后状态异常: %+v", claimed)
	}
	if err := svc.UpdateProgress(ctx, claimed.ID, ProgressInput{Progress: 35, Checkpoint: "seg-3"}); err != nil {
		t.Fatalf("更新进度失败: %v", err)
	}
	progressed := mustGetTask(t, svc, claimed.ID, Query{SpaceID: models.DefaultSpaceID})
	if progressed.Progress != 35 || progressed.Checkpoint != "seg-3" {
		t.Fatalf("进度未持久化: %+v", progressed)
	}
	if err := svc.MarkSucceeded(ctx, claimed.ID); err != nil {
		t.Fatalf("标记成功失败: %v", err)
	}
	succeeded := mustGetTask(t, svc, claimed.ID, Query{SpaceID: models.DefaultSpaceID})
	if succeeded.Status != models.TaskStatusSucceeded || succeeded.Progress != 100 || succeeded.FinishedAt == nil {
		t.Fatalf("成功终态异常: %+v", succeeded)
	}

	failedTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "transcode.hls",
		MaxAttempts: 1,
	})
	claimedFailed, err := svc.ClaimNext(ctx, ClaimQuery{Type: "transcode.hls"})
	if err != nil {
		t.Fatalf("领取失败路径任务失败: %v", err)
	}
	if claimedFailed.ID != failedTask.ID {
		t.Fatalf("领取到错误任务: got=%d want=%d", claimedFailed.ID, failedTask.ID)
	}
	if err := svc.MarkFailed(ctx, claimedFailed.ID, "转码失败"); err != nil {
		t.Fatalf("标记失败失败: %v", err)
	}
	failed := mustGetTask(t, svc, claimedFailed.ID, Query{SpaceID: models.DefaultSpaceID})
	if failed.Status != models.TaskStatusFailed || failed.Attempts != 1 || failed.Error != "转码失败" || failed.FinishedAt == nil {
		t.Fatalf("失败终态异常: %+v", failed)
	}
}

func TestMarkSucceededRecordsAuditEvent(t *testing.T) {
	svc, db := newTaskTestService(t)
	if err := db.AutoMigrate(&models.AuditEvent{}); err != nil {
		t.Fatalf("迁移审计表失败: %v", err)
	}
	svc.WithAudit(audit.NewRecorder(db))
	ctx := context.Background()
	task := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: models.DefaultSpaceID,
		Type:    "tool.download",
	})
	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: task.Type})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}

	if err := svc.MarkSucceeded(ctx, claimed.ID); err != nil {
		t.Fatalf("标记成功失败: %v", err)
	}

	var event models.AuditEvent
	if err := db.First(&event, "action = ?", "task.succeeded").Error; err != nil {
		t.Fatalf("应写入 task.succeeded 审计事件: %v", err)
	}
	if event.Scope != audit.ScopeSpace || event.SpaceID == nil || *event.SpaceID != models.DefaultSpaceID {
		t.Fatalf("成功事件应保留任务作用域: %+v", event)
	}
	if event.ResourceType != "task" || event.ResourceID == "" {
		t.Fatalf("成功事件资源字段不正确: %+v", event)
	}
}

func TestMarkFailedRequeuesWhenAttemptsRemain(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	svc.SetNowForTest(func() time.Time { return base })
	task := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "thumbnail.generate",
		MaxAttempts: 2,
	})
	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: task.Type})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}
	if err := svc.MarkFailed(ctx, claimed.ID, "临时失败"); err != nil {
		t.Fatalf("标记失败失败: %v", err)
	}
	got := mustGetTask(t, svc, claimed.ID, Query{SpaceID: models.DefaultSpaceID})
	if got.Status != models.TaskStatusPending || got.Attempts != 1 || got.FinishedAt != nil || got.NextRunAt == nil {
		t.Fatalf("仍有重试次数时应回到 pending 并设置退避时间: %+v", got)
	}
	if _, err := svc.ClaimNext(ctx, ClaimQuery{Type: task.Type}); !errors.Is(err, ErrNoPendingTask) {
		t.Fatalf("退避未到期时不应领取任务: %v", err)
	}
	svc.SetNowForTest(func() time.Time { return base.Add(2 * retryBackoffBase) })
	reclaimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: task.Type})
	if err != nil {
		t.Fatalf("退避到期后应可领取: %v", err)
	}
	if reclaimed.ID != task.ID || reclaimed.NextRunAt != nil {
		t.Fatalf("退避到期领取应清理 next_run_at: %+v", reclaimed)
	}
}

func TestWorkerTerminalWriteDoesNotOverrideCanceledTask(t *testing.T) {
	svc, db := newTaskTestService(t)
	ctx := context.Background()
	task := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: models.DefaultSpaceID,
		Type:    "export.video",
	})
	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: task.Type})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	if err := db.Model(&models.Task{}).Where("id = ?", claimed.ID).Updates(map[string]any{
		"status":      models.TaskStatusCanceled,
		"finished_at": now,
	}).Error; err != nil {
		t.Fatalf("模拟并发取消失败: %v", err)
	}
	if err := svc.MarkSucceeded(ctx, claimed.ID); err == nil {
		t.Fatal("已取消任务不应被 worker 标记成功覆盖")
	}
	got := mustGetTask(t, svc, claimed.ID, Query{SpaceID: models.DefaultSpaceID})
	if got.Status != models.TaskStatusCanceled {
		t.Fatalf("取消状态应保留: %+v", got)
	}
}

func TestRecoverRunningRequeuesTasks(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	mustEnqueueTask(t, svc, EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: models.DefaultSpaceID,
		Type:    "library.scan",
	})
	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: "library.scan"})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}

	if err := svc.RecoverRunning(ctx); err != nil {
		t.Fatalf("恢复 running 任务失败: %v", err)
	}
	recovered := mustGetTask(t, svc, claimed.ID, Query{SpaceID: models.DefaultSpaceID})
	if recovered.Status != models.TaskStatusPending || recovered.StartedAt != nil {
		t.Fatalf("running 恢复后应回到 pending: %+v", recovered)
	}
}

func TestClaimNextPreventsLowPriorityStarvation(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	tick := 0
	svc.SetNowForTest(func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	})

	low := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:    models.TaskScopeSpace,
		SpaceID:  models.DefaultSpaceID,
		Type:     "export.video",
		Priority: 1,
	})
	for i := 0; i < 4; i++ {
		mustEnqueueTask(t, svc, EnqueueInput{
			Scope:    models.TaskScopeSpace,
			SpaceID:  models.DefaultSpaceID,
			Type:     "export.video",
			Priority: 10,
		})
	}

	for i := 0; i < 3; i++ {
		claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: "export.video"})
		if err != nil {
			t.Fatalf("第 %d 次领取失败: %v", i+1, err)
		}
		if claimed.ID == low.ID {
			t.Fatalf("前 3 次仍应优先领取高优先级任务，第 %d 次领到了低优先级", i+1)
		}
		if err := svc.MarkSucceeded(ctx, claimed.ID); err != nil {
			t.Fatalf("标记高优先级任务成功失败: %v", err)
		}
	}
	claimed, err := svc.ClaimNext(ctx, ClaimQuery{Type: "export.video"})
	if err != nil {
		t.Fatalf("公平领取低优先级任务失败: %v", err)
	}
	if claimed.ID != low.ID {
		t.Fatalf("连续领取高优先级后应让低优先级任务前进: got=%d want=%d", claimed.ID, low.ID)
	}
}

func mustEnqueueTask(t *testing.T, svc *Service, input EnqueueInput) *models.Task {
	t.Helper()
	task, err := svc.Enqueue(context.Background(), input)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	return task
}

func mustGetTask(t *testing.T, svc *Service, id int64, query Query) *models.Task {
	t.Helper()
	task, err := svc.Get(context.Background(), id, query)
	if err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	return task
}

func countTasks(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&models.Task{}).Where(where, args...).Count(&count).Error; err != nil {
		t.Fatalf("统计任务失败: %v", err)
	}
	return count
}
