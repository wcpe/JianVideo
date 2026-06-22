package transcoder

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newCapabilityTestDB 创建内存库并迁移 CodecProbeCache 表。
func newCapabilityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// :memory: 每条连接是独立内存库，限单连接以保证迁移后的表对后续查询可见。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.CodecProbeCache{}))
	return db
}

// writeCacheForCurrentVersion 为当前 ffmpeg 版本写入一条带哨兵值的缓存。
// 哨兵编码器名真实探测绝不会产生，命中缓存即返回它，可证明未触发实测。
func writeCacheForCurrentVersion(t *testing.T, db *gorm.DB, version string, results []EncoderProbeResult) {
	t.Helper()
	raw, err := json.Marshal(results)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.CodecProbeCache{
		FFmpegVersion: version,
		Results:       string(raw),
		TestedAt:      time.Now(),
	}).Error)
}

// TestCapabilityService_CacheHit 命中当前版本缓存即返回，from_cache=true，且不触发实测。
func TestCapabilityService_CacheHit(t *testing.T) {
	db := newCapabilityTestDB(t)
	svc := NewCapabilityService(db)

	version := FFmpegVersion(context.Background())
	// 哨兵结果：真实探测绝不会产生 "sentinel-encoder"
	sentinel := []EncoderProbeResult{
		{Encoder: "sentinel-encoder", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	}
	writeCacheForCurrentVersion(t, db, version, sentinel)

	results, fromCache, gotVersion, testedAt, err := svc.CodecResults(context.Background(), false)
	require.NoError(t, err)
	assert.True(t, fromCache, "应命中缓存")
	assert.Equal(t, version, gotVersion)
	assert.False(t, testedAt.IsZero(), "命中缓存应有实测时间")
	require.Len(t, results, 1)
	assert.Equal(t, "sentinel-encoder", results[0].Encoder, "应返回缓存中的哨兵值，证明未重跑实测")
}

// TestCapabilityService_VersionMismatch 缓存版本与当前 ffmpeg 版本不一致时不命中。
func TestCapabilityService_VersionMismatch(t *testing.T) {
	db := newCapabilityTestDB(t)
	svc := NewCapabilityService(db)

	// 写入一条版本绝不匹配的缓存
	writeCacheForCurrentVersion(t, db, "ffmpeg version 0.0.0-nonexistent", []EncoderProbeResult{
		{Encoder: "sentinel-encoder", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	})

	results, fromCache, _, _, err := svc.CodecResults(context.Background(), false)
	require.NoError(t, err)
	assert.False(t, fromCache, "版本不一致不应命中缓存")
	// 未命中会触发实测；结果中不应含哨兵编码器
	for _, r := range results {
		assert.NotEqual(t, "sentinel-encoder", r.Encoder, "未命中应实测，不应返回哨兵值")
	}
}

// TestCapabilityService_Capabilities_ColdCache 冷缓存返回未测状态，不阻塞、不触发实测。
func TestCapabilityService_Capabilities_ColdCache(t *testing.T) {
	db := newCapabilityTestDB(t)
	svc := NewCapabilityService(db)

	start := time.Now()
	info := svc.Capabilities(context.Background())
	elapsed := time.Since(start)

	require.NotNil(t, info)
	assert.False(t, info.FromCache, "冷缓存 FromCache 应为 false")
	assert.Empty(t, info.TestedAt, "冷缓存 TestedAt 应为空")
	assert.Equal(t, "libx264", info.Preferred, "冷态应软件兜底")
	assert.True(t, info.SoftwareFallback)
	assert.Less(t, elapsed, 2*time.Second, "冷缓存 Capabilities 不应阻塞实测")
}

// TestCapabilityService_Capabilities_CacheHit 命中缓存时派生 per-codec 能力并标记来自缓存。
func TestCapabilityService_Capabilities_CacheHit(t *testing.T) {
	db := newCapabilityTestDB(t)
	svc := NewCapabilityService(db)

	version := FFmpegVersion(context.Background())
	writeCacheForCurrentVersion(t, db, version, []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	})

	info := svc.Capabilities(context.Background())
	require.NotNil(t, info)
	assert.True(t, info.FromCache, "命中缓存 FromCache 应为 true")
	assert.Equal(t, version, info.FFmpegVersion)
	assert.NotEmpty(t, info.TestedAt, "命中缓存应有 RFC3339 实测时间")
	assert.Equal(t, "h264_amf", info.Preferred, "应从缓存派生硬件 preferred")
}

// TestCapabilityService_ConcurrentNoDoubleWrite 并发调用单航道，不重复写入缓存。
// 预写当前版本缓存后，多协程并发读不应写出第二条记录。
func TestCapabilityService_ConcurrentNoDoubleWrite(t *testing.T) {
	db := newCapabilityTestDB(t)
	svc := NewCapabilityService(db)

	version := FFmpegVersion(context.Background())
	writeCacheForCurrentVersion(t, db, version, []EncoderProbeResult{
		{Encoder: "sentinel-encoder", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, _, err := svc.CodecResults(context.Background(), false)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	var count int64
	require.NoError(t, db.Model(&models.CodecProbeCache{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "并发读不应产生重复缓存记录")
}
