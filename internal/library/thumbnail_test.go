package library

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGenerateThumbnail_ConcurrencyCapped 验证缩略图生成受固定容量信号量限制，
// 提交远多于上限的任务时，实际并发数不超过 thumbnailConcurrency()。
// 修复前：每文件直接 go 启动 ffmpeg，无并发上限，扫描大目录会瞬间炸出成百上千并发进程。
func TestGenerateThumbnail_ConcurrencyCapped(t *testing.T) {
	capN := thumbnailConcurrency()
	if capN < 1 {
		t.Fatalf("并发上限应 ≥1，实际 %d", capN)
	}

	var current int32 // 当前并发数
	var peak int32    // 并发峰值
	var done int32    // 已完成任务数
	release := make(chan struct{})

	// 注入阻塞桩：记录峰值并发，阻塞驻留以暴露超限；全部任务最终经 release 放行后退出。
	runner := func(_ thumbnailJob) error {
		n := atomic.AddInt32(&current, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		<-release // 阻塞，直到测试放行，保证占满信号量
		atomic.AddInt32(&current, -1)
		atomic.AddInt32(&done, 1)
		return nil
	}

	// 提交远多于上限的任务
	total := capN * 5
	for i := 0; i < total; i++ {
		job, _ := resolveThumbnailJob("/fake/path/video_" + strconv.Itoa(i) + ".mp4")
		submitThumbnailWithRunner(job, runner)
	}

	// 等待信号量被占满（恰好 capN 个进入桩）；若无上限，更多任务会冲进桩抬高 peak。
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&current) < int32(capN) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// 再留一小段时间，给「本不该进来」的任务可乘之机（验证它们确实被挡住）。
	time.Sleep(100 * time.Millisecond)

	gotPeak := atomic.LoadInt32(&peak)

	// 放行全部任务并等待彻底排空，再恢复注入点，避免后台 goroutine 与 Cleanup 写竞争。
	close(release)
	drainDeadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&done) < int32(total) && time.Now().Before(drainDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if d := atomic.LoadInt32(&done); d < int32(total) {
		t.Fatalf("任务未全部排空：完成 %d / %d", d, total)
	}
	if gotPeak > int32(capN) {
		t.Fatalf("并发峰值 %d 超过上限 %d", gotPeak, capN)
	}
}

func TestGenerateThumbnail_SameKeyRunsOnceAndRetriesAfterFailure(t *testing.T) {
	job, ok := resolveThumbnailJob("/fake/path/same.mp4")
	if !ok {
		t.Fatal("测试视频应能解析缩略图任务")
	}
	job.size = 320
	var calls int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	runner := func(_ thumbnailJob) error {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
			return errors.New("首次生成失败")
		}
		return nil
	}

	const duplicates = 20
	for i := 0; i < duplicates; i++ {
		submitThumbnailWithRunner(job, runner)
	}
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("首个缩略图任务未开始")
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("同键并发只应生成一次，实际 %d 次", got)
	}
	close(releaseFirst)

	deadline := time.Now().Add(2 * time.Second)
	for thumbnailFlightInProgress(job) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if thumbnailFlightInProgress(job) {
		t.Fatal("失败任务结束后 singleflight 键未释放")
	}
	submitThumbnailWithRunner(job, runner)
	deadline = time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&calls) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("失败后再次提交应可重试，实际调用 %d 次", got)
	}
}

func TestGenerateThumbnail_DifferentKeysRemainConcurrent(t *testing.T) {
	if thumbnailConcurrency() < 2 {
		t.Skip("当前环境缩略图并发上限小于 2")
	}
	base, ok := resolveThumbnailJob("/fake/path/concurrent.mp4")
	if !ok {
		t.Fatal("测试视频应能解析缩略图任务")
	}
	jobSmall := base
	jobSmall.size = 160
	jobLarge := base
	jobLarge.size = 640
	if thumbnailJobKey(jobSmall) == thumbnailJobKey(jobLarge) {
		t.Fatal("不同尺寸与输出路径必须形成不同 singleflight 键")
	}
	jobStrategy := jobSmall
	jobStrategy.kind = kindImage
	if thumbnailJobKey(jobSmall) == thumbnailJobKey(jobStrategy) {
		t.Fatal("不同生成策略必须形成不同 singleflight 键")
	}

	var current int32
	var peak int32
	var done sync.WaitGroup
	done.Add(2)
	release := make(chan struct{})
	runner := func(_ thumbnailJob) error {
		defer done.Done()
		n := atomic.AddInt32(&current, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&current, -1)
		return nil
	}
	submitThumbnailWithRunner(jobSmall, runner)
	submitThumbnailWithRunner(jobLarge, runner)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&current) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	done.Wait()
	if got := atomic.LoadInt32(&peak); got < 2 {
		t.Fatalf("不同键应保持并发，实际峰值 %d", got)
	}
}
