package metrics

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newWALTestSampler 创建基于临时文件、启用 WAL 与 busy_timeout 的采样器，
// 用于校验采样写入与下采样查询的并发安全（高风险区）。
// 文件型库 + WAL 才能真正并发读写（内存库 journal_mode 恒为 memory）。
func newWALTestSampler(t *testing.T) *Sampler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "metrics_wal.db")
	// _journal_mode=WAL 开启 WAL；_busy_timeout 让并发写遇锁时等待而非立即 SQLITE_BUSY
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开 WAL 测试库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.MetricSample{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// Windows 下文件被进程占用时 TempDir 无法清理，测试结束先关闭底层连接释放文件句柄。
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return NewSampler(gdb, t.TempDir(), func() int { return 0 })
}

// TestSampler_ConcurrentWriteAndQuery 校验 SQLite WAL 下采样写入与 GetMetrics 查询并发不冲突。
// 起多个写 goroutine 持续插入样本，同时多个读 goroutine 持续下采样查询，全程不得报错。
func TestSampler_ConcurrentWriteAndQuery(t *testing.T) {
	s := newWALTestSampler(t)

	const (
		writers       = 4
		readers       = 4
		writesPerLoop = 25
	)

	var wg sync.WaitGroup
	errCh := make(chan error, (writers+readers)*writesPerLoop)
	stop := make(chan struct{})

	// 写侧：并发插入样本
	now := time.Now().UTC()
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < writesPerLoop; i++ {
				sample := models.MetricSample{
					// 散布在最近 1 小时窗口内，确保被 1h 查询覆盖
					SampledAt:       now.Add(-time.Duration(i) * time.Second),
					CPUPercent:      float64(worker*10 + i),
					MemUsedBytes:    int64(i * 1000),
					TranscodeActive: worker,
					Goroutines:      i,
				}
				if err := s.db.Create(&sample).Error; err != nil {
					errCh <- fmt.Errorf("并发写失败: %w", err)
					return
				}
			}
		}(w)
	}

	// 读侧：在写入期间持续下采样查询
	ranges := []string{"1h", "24h", "7d"}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.GetMetrics(ranges[worker%len(ranges)]); err != nil {
					errCh <- fmt.Errorf("并发查询失败: %w", err)
					return
				}
			}
		}(r)
	}

	// 轮询直至写入样本全部落库，再发停读信号让读 goroutine 退出
	deadline := time.Now().Add(10 * time.Second)
	want := int64(writers * writesPerLoop)
	for time.Now().Before(deadline) {
		if countSamples(t, s) >= want {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("并发读写出现错误: %v", err)
		}
	}

	// 写入应全部落库
	if got := countSamples(t, s); got != int64(writers*writesPerLoop) {
		t.Fatalf("并发写入样本数期望 %d，实际 %d", writers*writesPerLoop, got)
	}

	// 并发后仍可正常下采样查询
	result, err := s.GetMetrics("1h")
	if err != nil {
		t.Fatalf("并发后查询失败: %v", err)
	}
	if len(result.Points) == 0 {
		t.Fatalf("并发写入后下采样不应为空")
	}
}

// TestSampler_ConcurrentSampleOnceAndQuery 用真实采样路径（sampleOnce，含 prune）与查询并发。
// 覆盖「采样写 + 保留期裁剪」与「下采样读」同时发生的竞态。
func TestSampler_ConcurrentSampleOnceAndQuery(t *testing.T) {
	s := newWALTestSampler(t)

	var wg sync.WaitGroup
	errCh := make(chan error, 64)
	stop := make(chan struct{})
	ctx := context.Background()

	// 采样侧：走真实 sampleOnce（采集 + 写入 + prune）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			s.sampleOnce(ctx)
		}
	}()

	// 查询侧：持续读
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := s.GetMetrics("24h"); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// 等采样完成
	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("采样与查询并发出错: %v", err)
		}
	}
}
