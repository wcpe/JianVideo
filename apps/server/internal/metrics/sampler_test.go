package metrics

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// countSamples 返回 metric_samples 表当前行数。
func countSamples(t *testing.T, s *Sampler) int64 {
	t.Helper()
	var n int64
	if err := s.db.Model(&models.MetricSample{}).Count(&n).Error; err != nil {
		t.Fatalf("统计样本数失败: %v", err)
	}
	return n
}

// TestSampler_StartProducesSampleThenStop 校验生命周期：Start 后立即产生首样本，Stop 干净返回。
func TestSampler_StartProducesSampleThenStop(t *testing.T) {
	s := newTestSampler(t)

	s.Start(context.Background())
	// 首样本为同步立即采集（loop 内 ticker 前先采一次），稍等其落库
	deadline := time.Now().Add(2 * time.Second)
	for countSamples(t, s) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if countSamples(t, s) == 0 {
		t.Fatalf("Start 后应至少产生一条样本")
	}

	// Stop 应阻塞至后台 goroutine 退出，不挂起
	doneCh := make(chan struct{})
	go func() {
		s.Stop()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop 未在预期时间内返回（疑似 goroutine 未退出）")
	}
}

// TestSampler_StopNoGoroutineLeak 校验 Start/Stop 后无 goroutine 泄漏：
// 反复启停若干轮后，goroutine 数应回落到启动前水平附近。
func TestSampler_StopNoGoroutineLeak(t *testing.T) {
	s := newTestSampler(t)

	// 先让运行时稳定，记录基线
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		s.Start(context.Background())
		time.Sleep(10 * time.Millisecond)
		s.Stop()
	}

	// Stop 已等待 goroutine 退出；再给运行时少许结算时间
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()

	// 允许 ±1 的运行时抖动；若泄漏，after 会随启停轮数累积上升
	if after > before+1 {
		t.Fatalf("疑似 goroutine 泄漏：启停前 %d，启停后 %d", before, after)
	}
}

// TestSampler_StartIdempotent 校验重复 Start 不重复起 goroutine（幂等）。
func TestSampler_StartIdempotent(t *testing.T) {
	s := newTestSampler(t)

	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	s.Start(context.Background())
	s.Start(context.Background()) // 第二次应无副作用
	s.Start(context.Background())
	defer s.Stop()

	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// 仅应多出 1 个采样 goroutine（容许 ±1 抖动）
	if after > before+2 {
		t.Fatalf("重复 Start 疑似起了多个 goroutine：前 %d，后 %d", before, after)
	}
}

// TestSampler_RetentionPrune 校验保留期裁剪：超 Retention 的样本在采样后被删，表保持有界。
func TestSampler_RetentionPrune(t *testing.T) {
	s := newTestSampler(t)

	now := time.Now().UTC()
	// 一条远超保留期（8 天前）的样本 + 一条近期样本
	insertSample(t, s, models.MetricSample{SampledAt: now.Add(-8 * 24 * time.Hour), CPUPercent: 1})
	insertSample(t, s, models.MetricSample{SampledAt: now.Add(-1 * time.Hour), CPUPercent: 2})
	if countSamples(t, s) != 2 {
		t.Fatalf("前置应有 2 条样本，实际 %d", countSamples(t, s))
	}

	// 触发一次采样（内部含 prune）：新增 1 条当前样本，删除 1 条超期样本
	s.sampleOnce(context.Background())

	// 期望：超期那条被删；剩近期 1 条 + 新采样 1 条 = 2 条
	if got := countSamples(t, s); got != 2 {
		t.Fatalf("裁剪后期望 2 条（近期+新采样，超期被删），实际 %d", got)
	}

	// 进一步确认：不存在早于保留期下限的样本
	var stale int64
	cutoff := time.Now().UTC().Add(-Retention)
	if err := s.db.Model(&models.MetricSample{}).Where("sampled_at < ?", cutoff).Count(&stale).Error; err != nil {
		t.Fatalf("统计超期样本失败: %v", err)
	}
	if stale != 0 {
		t.Fatalf("裁剪后不应残留超期样本，实际 %d 条", stale)
	}
}

// TestSampler_TranscodeProviderInjected 校验注入的活跃会话 provider 被正确反映进样本。
func TestSampler_TranscodeProviderInjected(t *testing.T) {
	s := newTestSampler(t)
	// 覆盖默认 provider，返回固定活跃数
	active := 7
	s.transcodeCount = func() int { return active }

	s.sampleOnce(context.Background())

	var sample models.MetricSample
	if err := s.db.Order("id DESC").Take(&sample).Error; err != nil {
		t.Fatalf("读取最新样本失败: %v", err)
	}
	if sample.TranscodeActive != 7 {
		t.Fatalf("样本转码并发应反映 provider 返回值 7，实际 %d", sample.TranscodeActive)
	}

	// provider 返回值变化后，下一次采样应跟随变化
	active = 3
	s.sampleOnce(context.Background())
	// 用全新结构体读取，避免 GORM 因复用结构体已带主键而隐式追加 WHERE id=旧值
	var sample2 models.MetricSample
	if err := s.db.Order("id DESC").Take(&sample2).Error; err != nil {
		t.Fatalf("读取最新样本失败: %v", err)
	}
	if sample2.TranscodeActive != 3 {
		t.Fatalf("provider 变更后样本应为 3，实际 %d", sample2.TranscodeActive)
	}
}
