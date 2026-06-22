package library

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"jianvideo/internal/db/models"
)

// TestScanEnrich_BoundedConcurrency 验证扫描期对新文件的富化为有界并发：
// 注入阻塞桩观测同时进入富化的 goroutine 峰值，应大于 1（并非串行）且不超过 cap。
// 修复前 indexMediaFiles 串行调用 CreateMediaFile，峰值恒为 1，本测试先红。
func TestScanEnrich_BoundedConcurrency(t *testing.T) {
	svc, _ := newTestService(t)

	// 备份并在测试结束后还原全局富化函数变量
	orig := enrichMediaMetadataFn
	t.Cleanup(func() { enrichMediaMetadataFn = orig })

	const fileCount = 8
	var current int32
	var peak int32
	var mu sync.Mutex
	enrichMediaMetadataFn = func(mf *models.MediaFile) {
		c := atomic.AddInt32(&current, 1)
		mu.Lock()
		if c > peak {
			peak = c
		}
		mu.Unlock()
		// 阻塞一小段，制造并发窗口，让多个富化任务有机会同时在场
		time.Sleep(80 * time.Millisecond)
		atomic.AddInt32(&current, -1)
	}

	dir := t.TempDir()
	for i := 0; i < fileCount; i++ {
		name := filepath.Join(dir, "clip_"+string(rune('a'+i))+".mp4")
		if err := os.WriteFile(name, []byte("fake"), 0o644); err != nil {
			t.Fatalf("写测试文件失败: %v", err)
		}
	}

	count, err := svc.ScanLibraryWithType(1, dir, "local", ScanModeIncremental)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if count != fileCount {
		t.Fatalf("应入库 %d 个文件, 实际 %d", fileCount, count)
	}

	cap := scanEnrichConcurrency()
	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak <= 1 {
		t.Fatalf("扫描期富化应并发执行（峰值 > 1），实际峰值 %d（疑似串行）", gotPeak)
	}
	if int(gotPeak) > cap {
		t.Fatalf("扫描期富化并发峰值 %d 超过上限 cap=%d", gotPeak, cap)
	}
}
