package player

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHLSSegmentWriter_ConcurrentWrite 验证 10 个 goroutine 同时写入不会出错。
func TestHLSSegmentWriter_ConcurrentWrite(t *testing.T) {
	tDir := t.TempDir()

	w, err := NewHLSSegmentWriter(tDir, 1, "1080p")
	require.NoError(t, err)

	var wg sync.WaitGroup
	var errCount atomic.Int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data := bytes.Repeat([]byte{byte(idx + 1)}, 128)
			if err := w.WriteSegment(data); err != nil {
				errCount.Add(1)
				t.Errorf("goroutine %d WriteSegment 失败: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int32(0), errCount.Load(), "所有并发写入都应成功")
	assert.Equal(t, 10, w.seq, "写入 10 个切片后 seq 应为 10")

	// 验证 m3u8 文件包含 10 个 #EXTINF 记录
	mediaDir := filepath.Join(tDir, "1")
	content, err := os.ReadFile(filepath.Join(mediaDir, "1080p.m3u8"))
	require.NoError(t, err)
	assert.Equal(t, 10, strings.Count(string(content), "#EXTINF"),
		"m3u8 应包含 10 个 EXTINF 记录")

	_ = w.Close()
}

// TestHLSSegmentWriter_CloseIdempotent 验证重复调用 Close 不会报错。
func TestHLSSegmentWriter_CloseIdempotent(t *testing.T) {
	tDir := t.TempDir()

	w, err := NewHLSSegmentWriter(tDir, 1, "1080p")
	require.NoError(t, err)

	require.NoError(t, w.Close(), "第一次 Close 应成功")
	require.NoError(t, w.Close(), "第二次 Close 也应成功（幂等）")
}

// TestHLSSegmentWriter_WriteAfterClose 验证 Close 后写入应返回错误。
func TestHLSSegmentWriter_WriteAfterClose(t *testing.T) {
	tDir := t.TempDir()

	w, err := NewHLSSegmentWriter(tDir, 1, "1080p")
	require.NoError(t, err)

	require.NoError(t, w.Close())

	err = w.WriteSegment([]byte("should fail"))
	assert.Error(t, err, "Close 后 WriteSegment 应返回错误")
}

// TestHLSManager_ConcurrentGetOrCreate 验证并发获取同一 writer 返回同一实例。
func TestHLSManager_ConcurrentGetOrCreate(t *testing.T) {
	tDir := t.TempDir()
	mgr := NewHLSManager(tDir)

	const concurrency = 20
	var wg sync.WaitGroup
	writers := make([]*HLSSegmentWriter, concurrency)
	var errCount atomic.Int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w, err := mgr.GetOrCreateWriter(1, "1080p")
			if err != nil {
				errCount.Add(1)
				t.Errorf("goroutine %d GetOrCreateWriter 失败: %v", idx, err)
				return
			}
			writers[idx] = w
		}(i)
	}
	wg.Wait()

	require.Equal(t, int32(0), errCount.Load(), "所有并发获取都应成功")

	// 验证所有 goroutine 获取到的是同一个实例
	first := writers[0]
	require.NotNil(t, first, "第一个 writer 不应为 nil")
	for i := 1; i < concurrency; i++ {
		assert.True(t, first == writers[i],
			"goroutine %d 的 writer 应与 goroutine 0 相同", i)
	}

	_ = first.Close()
}
