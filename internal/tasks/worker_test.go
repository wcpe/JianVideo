package tasks

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestWorkerRegistryValidatesRegistration(t *testing.T) {
	svc, _ := newTaskTestService(t)
	registry := NewWorkerRegistry(svc)
	handler := func(context.Context, models.Task) error { return nil }

	if err := registry.Register("", 1, handler); err == nil {
		t.Fatal("空任务类型应被拒绝")
	}
	if err := registry.Register("library.scan", 0, handler); err == nil {
		t.Fatal("非正并发上限应被拒绝")
	}
	if err := registry.Register("library.scan", 1, nil); err == nil {
		t.Fatal("空处理器应被拒绝")
	}
}

func TestWorkerRegistryRunsTasksWithTypeConcurrencyLimit(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		mustEnqueueTask(t, svc, EnqueueInput{
			Scope:       models.TaskScopeSpace,
			SpaceID:     models.DefaultSpaceID,
			Type:        "thumbnail.generate",
			MaxAttempts: 1,
		})
	}

	var active int64
	var peak int64
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	handler := func(context.Context, models.Task) error {
		current := atomic.AddInt64(&active, 1)
		updatePeak(&peak, current)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		atomic.AddInt64(&active, -1)
		return nil
	}
	registry := NewWorkerRegistry(svc)
	if err := registry.Register("thumbnail.generate", 2, handler); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- registry.RunPending(ctx)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("worker 未在超时时间内达到并发上限，峰值=%d", atomic.LoadInt64(&peak))
		}
	}
	if got := atomic.LoadInt64(&peak); got != 2 {
		t.Fatalf("并发峰值应等于上限 2，实际 %d", got)
	}
	close(release)
	released = true
	if err := <-done; err != nil {
		t.Fatalf("执行 pending 任务失败: %v", err)
	}
	page, err := svc.List(ctx, Query{SpaceID: models.DefaultSpaceID, Type: "thumbnail.generate"})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	for _, task := range page.Items {
		if task.Status != models.TaskStatusSucceeded {
			t.Fatalf("任务应全部成功: %+v", page.Items)
		}
	}
}

func TestWorkerRegistryRunPendingIsExclusive(t *testing.T) {
	svc, _ := newTaskTestService(t)
	mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "metadata.backfill",
		MaxAttempts: 1,
	})
	entered := make(chan struct{})
	release := make(chan struct{})
	registry := NewWorkerRegistry(svc)
	if err := registry.Register("metadata.backfill", 1, func(context.Context, models.Task) error {
		close(entered)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- registry.RunPending(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("首个 RunPending 未开始执行任务")
	}
	go func() { secondDone <- registry.RunPending(context.Background()) }()
	select {
	case err := <-secondDone:
		t.Fatalf("第二个 RunPending 不应在首个执行完成前返回: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("首个 RunPending 执行失败: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("第二个 RunPending 执行失败: %v", err)
	}
}

func TestWorkerRegistryWakeCoalescesConcurrentSignals(t *testing.T) {
	svc, _ := newTaskTestService(t)
	mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "metadata.backfill",
		MaxAttempts: 1,
	})
	entered := make(chan struct{})
	release := make(chan struct{})
	registry := NewWorkerRegistry(svc)
	if err := registry.Register("metadata.backfill", 1, func(context.Context, models.Task) error {
		close(entered)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	registry.Wake()
	<-entered
	for i := 0; i < 1000; i++ {
		registry.Wake()
	}
	registry.wakeMu.Lock()
	running, pending := registry.wakeRunning, registry.wakePending
	registry.wakeMu.Unlock()
	if !running || !pending {
		t.Fatalf("并发唤醒应合并为一个补跑信号: running=%v pending=%v", running, pending)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		registry.wakeMu.Lock()
		idle := !registry.wakeRunning && !registry.wakePending
		registry.wakeMu.Unlock()
		if idle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("合并唤醒执行完成后未回到空闲状态")
}

func TestWorkerRegistryMarksFailedAndRetries(t *testing.T) {
	svc, _ := newTaskTestService(t)
	ctx := context.Background()
	retryTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "metadata.backfill",
		MaxAttempts: 2,
	})
	registry := NewWorkerRegistry(svc)
	if err := registry.Register(retryTask.Type, 1, func(context.Context, models.Task) error {
		return errors.New("处理失败")
	}); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}

	if err := registry.RunPending(ctx); err != nil {
		t.Fatalf("执行失败重试任务不应中断 worker: %v", err)
	}
	got := mustGetTask(t, svc, retryTask.ID, Query{SpaceID: models.DefaultSpaceID})
	if got.Status != models.TaskStatusPending || got.Attempts != 1 || got.NextRunAt == nil {
		t.Fatalf("仍有重试次数时应回到 pending 并延后领取: %+v", got)
	}

	finalTask := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "export.video",
		MaxAttempts: 1,
	})
	finalRegistry := NewWorkerRegistry(svc)
	if err := finalRegistry.Register(finalTask.Type, 1, func(context.Context, models.Task) error {
		return errors.New("最终失败")
	}); err != nil {
		t.Fatalf("注册最终失败 worker 失败: %v", err)
	}
	if err := finalRegistry.RunPending(ctx); err != nil {
		t.Fatalf("执行最终失败任务不应中断 worker: %v", err)
	}
	failed := mustGetTask(t, svc, finalTask.ID, Query{SpaceID: models.DefaultSpaceID})
	if failed.Status != models.TaskStatusFailed || failed.Attempts != 1 || failed.Error != "最终失败" {
		t.Fatalf("重试耗尽后应失败: %+v", failed)
	}
}

func TestWorkerRegistryKeepsCanceledTerminalState(t *testing.T) {
	svc, _ := newTaskTestService(t)
	task := mustEnqueueTask(t, svc, EnqueueInput{
		Scope:       models.TaskScopeSpace,
		SpaceID:     models.DefaultSpaceID,
		Type:        "metadata.backfill",
		MaxAttempts: 1,
	})
	entered := make(chan struct{})
	registry := NewWorkerRegistry(svc)
	if err := registry.Register(task.Type, 1, func(ctx context.Context, _ models.Task) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- registry.RunPending(context.Background()) }()
	<-entered
	if err := svc.Cancel(context.Background(), task.ID, Query{SpaceID: models.DefaultSpaceID}); err != nil {
		t.Fatalf("取消运行中任务失败: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("取消后的 worker 不应失败: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("取消 running 任务后处理器未收到 context 取消信号")
	}
	got := mustGetTask(t, svc, task.ID, Query{SpaceID: models.DefaultSpaceID})
	if got.Status != models.TaskStatusCanceled {
		t.Fatalf("worker 不得覆盖取消终态: %+v", got)
	}
}

func TestDefaultConcurrencyByTaskType(t *testing.T) {
	cases := map[string]int{
		"library.scan":       1,
		"transcode.hls":      1,
		"thumbnail.generate": 4,
		"metadata.backfill":  2,
	}
	for taskType, want := range cases {
		if got := DefaultConcurrency(taskType); got != want {
			t.Fatalf("%s 默认并发上限 got=%d want=%d", taskType, got, want)
		}
	}
}

func updatePeak(peak *int64, current int64) {
	for {
		previous := atomic.LoadInt64(peak)
		if current <= previous || atomic.CompareAndSwapInt64(peak, previous, current) {
			return
		}
	}
}
