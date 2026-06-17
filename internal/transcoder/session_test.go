package transcoder

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewTranscodeSession(t *testing.T) {
	sess := NewTranscodeSession("test-session-1")
	if sess == nil {
		t.Fatal("NewTranscodeSession 返回 nil")
	}
	if sess.ID() != "test-session-1" {
		t.Fatalf("期望 ID='test-session-1', 实际 '%s'", sess.ID())
	}
	if sess.Status() != "stopped" {
		t.Fatalf("期望初始状态 'stopped', 实际 '%s'", sess.Status())
	}
}

func TestStartTranscode(t *testing.T) {
	sess := NewTranscodeSession("test-start")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess.Start(ctx, cancel)

	if sess.Status() != "running" {
		t.Fatalf("期望状态 'running', 实际 '%s'", sess.Status())
	}
}

func TestSeekCancelsOldContext(t *testing.T) {
	sess := NewTranscodeSession("test-seek-cancel")

	ctx1, cancel1 := context.WithCancel(context.Background())
	sess.Start(ctx1, cancel1)

	if sess.Status() != "running" {
		t.Fatalf("第一次 Start 后期望 'running', 实际 '%s'", sess.Status())
	}

	// Seek 应该 cancel 旧 context
	ctx2, cancel2 := context.WithCancel(context.Background())
	sess.Seek(ctx2, cancel2, 10.0)

	// 旧 context 应该已被 cancel
	select {
	case <-ctx1.Done():
		// 预期行为
	case <-time.After(500 * time.Millisecond):
		t.Fatal("旧 context 未被 cancel")
	}

	if sess.Status() != "seeking" {
		t.Fatalf("Seek 后期望 'seeking', 实际 '%s'", sess.Status())
	}

	cancel2()
}

func TestSeekConsecutive(t *testing.T) {
	sess := NewTranscodeSession("test-consecutive")

	// 第一次 Seek
	ctx1, cancel1 := context.WithCancel(context.Background())
	sess.Seek(ctx1, cancel1, 5.0)

	// 第二次 Seek 应 cancel 第一次的 context
	ctx2, cancel2 := context.WithCancel(context.Background())
	sess.Seek(ctx2, cancel2, 20.0)

	select {
	case <-ctx1.Done():
		// 预期行为
	case <-time.After(500 * time.Millisecond):
		t.Fatal("第一次 Seek 的 context 未被第二次 Seek cancel")
	}

	// 第三次 Seek
	ctx3, cancel3 := context.WithCancel(context.Background())
	sess.Seek(ctx3, cancel3, 30.0)

	select {
	case <-ctx2.Done():
		// 预期行为
	case <-time.After(500 * time.Millisecond):
		t.Fatal("第二次 Seek 的 context 未被第三次 Seek cancel")
	}

	// ctx3 不应被 cancel
	select {
	case <-ctx3.Done():
		t.Fatal("最新的 context 不应被 cancel")
	default:
		// 预期行为
	}

	cancel3()
}

func TestSeekConcurrentSafety(t *testing.T) {
	sess := NewTranscodeSession("test-concurrent")

	var wg sync.WaitGroup
	const numSeeks = 10

	for i := 0; i < numSeeks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sess.Seek(ctx, cancel, float64(idx))
		}(i)
	}

	wg.Wait()

	// 最终状态应该是 seeking
	if sess.Status() != "seeking" {
		t.Fatalf("并发 Seek 后期望 'seeking', 实际 '%s'", sess.Status())
	}
}

func TestStop(t *testing.T) {
	sess := NewTranscodeSession("test-stop")

	ctx, cancel := context.WithCancel(context.Background())
	sess.Start(ctx, cancel)

	sess.Stop()

	if sess.Status() != "stopped" {
		t.Fatalf("Stop 后期望 'stopped', 实际 '%s'", sess.Status())
	}

	// context 应已被 cancel
	select {
	case <-ctx.Done():
		// 预期行为
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop 后 context 未被 cancel")
	}
}

func TestSeekAfterStop(t *testing.T) {
	sess := NewTranscodeSession("test-seek-after-stop")

	ctx1, cancel1 := context.WithCancel(context.Background())
	sess.Start(ctx1, cancel1)
	sess.Stop()

	// Seek 应能正常工作，即使之前已 Stop
	ctx2, cancel2 := context.WithCancel(context.Background())
	sess.Seek(ctx2, cancel2, 15.0)

	if sess.Status() != "seeking" {
		t.Fatalf("Stop 后 Seek 期望 'seeking', 实际 '%s'", sess.Status())
	}

	cancel2()
}

func TestSeekStoresPosition(t *testing.T) {
	sess := NewTranscodeSession("test-position")

	ctx, cancel := context.WithCancel(context.Background())
	sess.Seek(ctx, cancel, 42.5)

	pos := sess.SeekPosition()
	if pos != 42.5 {
		t.Fatalf("期望 SeekPosition=42.5, 实际 %f", pos)
	}

	cancel()
}
