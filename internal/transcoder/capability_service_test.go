package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
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

type failingCapabilityAuditRecorder struct{}

func (failingCapabilityAuditRecorder) Record(context.Context, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (failingCapabilityAuditRecorder) RecordTx(context.Context, *gorm.DB, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (failingCapabilityAuditRecorder) List(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, nil
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

func TestCapabilityService_LoadCachedSnapshot(t *testing.T) {
	version := FFmpegVersion(context.Background())
	if version == "" {
		t.Skip("ffmpeg 不可用，跳过同步加载命中测试")
	}
	db := newCapabilityTestDB(t)
	writeCacheForCurrentVersion(t, db, version, []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	})
	clearProbeSnapshot()
	t.Cleanup(clearProbeSnapshot)

	require.NoError(t, NewCapabilityService(db).LoadCachedSnapshot(context.Background()))

	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "h264_amf", enc)
	assert.Equal(t, "d3d11va", dev)
}

func TestCapabilityService_LoadCachedSnapshotCorruptedCacheClearsSnapshot(t *testing.T) {
	version := FFmpegVersion(context.Background())
	if version == "" {
		t.Skip("ffmpeg 不可用，跳过损坏缓存测试")
	}
	db := newCapabilityTestDB(t)
	require.NoError(t, db.Create(&models.CodecProbeCache{
		FFmpegVersion: version,
		Results:       "{损坏的缓存",
		TestedAt:      time.Now(),
	}).Error)
	setProbeSnapshot([]EncoderProbeResult{{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true}})
	t.Cleanup(clearProbeSnapshot)

	require.NoError(t, NewCapabilityService(db).LoadCachedSnapshot(context.Background()))

	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", enc, "损坏缓存不得保留旧快照")
	assert.Empty(t, dev)
}

func TestCapabilityService_LoadCachedSnapshotVersionMismatchClearsSnapshot(t *testing.T) {
	db := newCapabilityTestDB(t)
	writeCacheForCurrentVersion(t, db, "ffmpeg version 0.0.0-nonexistent", []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	})
	setProbeSnapshot([]EncoderProbeResult{{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true}})
	t.Cleanup(clearProbeSnapshot)

	require.NoError(t, NewCapabilityService(db).LoadCachedSnapshot(context.Background()))

	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", enc, "版本不匹配不得保留旧快照")
	assert.Empty(t, dev)
}

func TestCapabilityService_LoadCachedSnapshotReadFailureClearsSnapshot(t *testing.T) {
	if FFmpegVersion(context.Background()) == "" {
		t.Skip("ffmpeg 不可用，跳过缓存读取失败测试")
	}
	db := newCapabilityTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	setProbeSnapshot([]EncoderProbeResult{{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true}})
	t.Cleanup(clearProbeSnapshot)

	require.Error(t, NewCapabilityService(db).LoadCachedSnapshot(context.Background()))

	enc, dev, selectErr := SelectBestEncoder()
	require.NoError(t, selectErr)
	assert.Equal(t, "libx264", enc, "缓存读取失败不得保留旧快照")
	assert.Empty(t, dev)
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

func TestCapabilityService_CapabilitiesMissClearsOldSnapshot(t *testing.T) {
	oldPath := GetFFmpegPath()
	t.Cleanup(func() {
		SetFFmpegPath(oldPath)
		probeSnapshot.Store(nil)
	})
	SetFFmpegPath("jianvideo-nonexistent-ffmpeg-for-cache-miss")
	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true},
	})

	info := NewCapabilityService(nil).Capabilities(context.Background())

	assert.Equal(t, "libx264", info.Preferred)
	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", enc, "缓存未命中后转码选择不得继续使用旧快照")
	assert.Empty(t, dev)
}

func TestCapabilityService_PathSwitchRejectsStaleSnapshotPublish(t *testing.T) {
	tests := []struct {
		name        string
		expectRetry bool
		call        func(*CapabilityService) ([]EncoderProbeResult, error)
	}{
		{name: "CodecResultsWithAudit", expectRetry: true, call: func(svc *CapabilityService) ([]EncoderProbeResult, error) {
			results, _, _, _, err := svc.CodecResultsWithAudit(context.Background(), false, nil)
			return results, err
		}},
		{name: "Capabilities", call: func(svc *CapabilityService) ([]EncoderProbeResult, error) {
			svc.Capabilities(context.Background())
			return nil, nil
		}},
		{name: "LoadCachedSnapshot", call: func(svc *CapabilityService) ([]EncoderProbeResult, error) {
			return nil, svc.LoadCachedSnapshot(context.Background())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPathSwitchRejectsStaleSnapshotPublish(t, tt.expectRetry, tt.call)
		})
	}
}

type capabilityCacheOutcome struct {
	results []EncoderProbeResult
	err     error
}

func assertPathSwitchRejectsStaleSnapshotPublish(t *testing.T, expectRetry bool, call func(*CapabilityService) ([]EncoderProbeResult, error)) {
	t.Helper()
	oldPath := GetFFmpegPath()
	version := FFmpegVersion(context.Background())
	if version == "" {
		t.Skip("ffmpeg 不可用，跳过路径切换交错测试")
	}
	t.Cleanup(func() {
		SetFFmpegPath(oldPath)
		clearProbeSnapshot()
	})

	db := newCapabilityTestDB(t)
	writeCacheForCurrentVersion(t, db, version, []EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", Compiled: true, TestedOK: true},
	})
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:block_capability_cache_query", func(*gorm.DB) {
		once.Do(func() {
			close(queryStarted)
			<-releaseQuery
		})
	}))

	result := make(chan capabilityCacheOutcome, 1)
	go func() {
		results, err := call(NewCapabilityService(db))
		result <- capabilityCacheOutcome{results: results, err: err}
	}()
	select {
	case <-queryStarted:
	case <-time.After(2 * time.Second):
		close(releaseQuery)
		t.Fatal("能力缓存读取未进入可控阻塞点")
	}
	SetFFmpegPath(oldPath + ".switched")
	close(releaseQuery)
	select {
	case outcome := <-result:
		if expectRetry {
			require.ErrorIs(t, outcome.err, ErrFFmpegPathChanged)
			assert.Empty(t, outcome.results, "路径切换后不得返回旧缓存结果")
		} else {
			require.NoError(t, outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("能力缓存读取未在释放阻塞点后完成")
	}

	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", enc, "旧路径缓存读取不得在路径切换后恢复快照")
	assert.Empty(t, dev)
}

func TestCapabilityService_PathSwitchBeforeProbeReturnsRetryError(t *testing.T) {
	testCapabilityProbePathSwitch(t, "version", false)
}

func TestCapabilityService_PathSwitchDuringProbeDoesNotPolluteCache(t *testing.T) {
	testCapabilityProbePathSwitch(t, "probe", true)
}

func testCapabilityProbePathSwitch(t *testing.T, blockPoint string, expectProbe bool) {
	t.Helper()
	for _, tt := range []struct {
		name  string
		force bool
	}{
		{name: "冷缓存", force: false},
		{name: "强制重测", force: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertCapabilityProbePathSwitch(t, blockPoint, expectProbe, tt.force)
		})
	}
}

func assertCapabilityProbePathSwitch(t *testing.T, blockPoint string, expectProbe, force bool) {
	t.Helper()
	const oldVersion = "ffmpeg version old-capability-test"
	oldGlobalPath := GetFFmpegPath()
	oldBinary, newBinary, stateDir := buildBlockingCapabilityFFmpegPair(t, blockPoint)
	releasePath := filepath.Join(stateDir, blockPoint+"-release")
	SetFFmpegPath(oldBinary)
	t.Cleanup(func() {
		_ = os.WriteFile(releasePath, []byte("release"), 0o600)
		SetFFmpegPath(oldGlobalPath)
		clearProbeSnapshot()
	})

	db := newCapabilityTestDB(t)
	if force {
		writeCacheForCurrentVersion(t, db, oldVersion, []EncoderProbeResult{{Encoder: "sentinel-before-force"}})
	}
	result := make(chan capabilityProbeOutcome, 1)
	go func() {
		results, _, version, _, err := NewCapabilityService(db).CodecResultsWithAudit(context.Background(), force, nil)
		result <- capabilityProbeOutcome{results: results, version: version, err: err}
	}()

	waitForCapabilityProbeFile(t, filepath.Join(stateDir, blockPoint+"-started"))
	SetFFmpegPath(newBinary)
	require.NoError(t, os.WriteFile(releasePath, []byte("release"), 0o600))
	outcome := waitForCapabilityProbeOutcome(t, result)
	require.ErrorIs(t, outcome.err, ErrFFmpegPathChanged)
	assert.Empty(t, outcome.results, "旧代次实测不得返回空成功或旧结果")
	assert.Equal(t, oldVersion, outcome.version)
	if expectProbe {
		assertProbeStayedOnOldPath(t, stateDir)
	} else {
		assertProbeStoppedBeforeEncoderProbe(t, stateDir)
	}
	assertStaleProbeDidNotWriteCache(t, db, oldVersion, force)
}

type capabilityProbeOutcome struct {
	results []EncoderProbeResult
	version string
	err     error
}

func waitForCapabilityProbeFile(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "未进入可控能力探测阻塞点")
}

func waitForCapabilityProbeOutcome(t *testing.T, result <-chan capabilityProbeOutcome) capabilityProbeOutcome {
	t.Helper()
	select {
	case outcome := <-result:
		return outcome
	case <-time.After(5 * time.Second):
		t.Fatal("释放能力探测后实测未完成")
		return capabilityProbeOutcome{}
	}
}

func assertProbeStayedOnOldPath(t *testing.T, stateDir string) {
	t.Helper()
	oldRecords := readCapabilityProbeRecords(t, filepath.Join(stateDir, "old.log"))
	assert.Contains(t, oldRecords, "-version")
	assert.Contains(t, oldRecords, "-encoders", "编码器列表必须使用捕获的旧路径")
	assert.Contains(t, oldRecords, "-c:v libx264", "实际试编码必须使用捕获的旧路径")
	assert.Empty(t, readCapabilityProbeRecords(t, filepath.Join(stateDir, "new.log")), "新路径不得参与旧代次实测")
}

func assertProbeStoppedBeforeEncoderProbe(t *testing.T, stateDir string) {
	t.Helper()
	oldRecords := readCapabilityProbeRecords(t, filepath.Join(stateDir, "old.log"))
	assert.Contains(t, oldRecords, "-version")
	assert.NotContains(t, oldRecords, "-encoders", "代次已失效时不得继续枚举编码器")
	assert.NotContains(t, oldRecords, "-c:v libx264", "代次已失效时不得继续实际试编码")
	assert.Empty(t, readCapabilityProbeRecords(t, filepath.Join(stateDir, "new.log")), "新路径不得参与旧代次请求")
}

func assertStaleProbeDidNotWriteCache(t *testing.T, db *gorm.DB, version string, force bool) {
	t.Helper()
	var rows []models.CodecProbeCache
	require.NoError(t, db.Where("ffmpeg_version = ?", version).Find(&rows).Error)
	if !force {
		assert.Empty(t, rows, "冷缓存旧代次实测结果不得落库")
		return
	}
	require.Len(t, rows, 1)
	var results []EncoderProbeResult
	require.NoError(t, json.Unmarshal([]byte(rows[0].Results), &results))
	require.Len(t, results, 1)
	assert.Equal(t, "sentinel-before-force", results[0].Encoder, "强制重测不得覆盖旧版本缓存")
}

func readCapabilityProbeRecords(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(raw)
}

func buildBlockingCapabilityFFmpegPair(t *testing.T, blockPoint string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	t.Setenv("JIANVIDEO_CAPABILITY_PROBE_STATE", stateDir)
	t.Setenv("JIANVIDEO_CAPABILITY_PROBE_BLOCK_POINT", blockPoint)
	sourcePath := filepath.Join(dir, "fake_ffmpeg.go")
	oldBinary := filepath.Join(dir, "old-ffmpeg.exe")
	newBinary := filepath.Join(dir, "new-ffmpeg.exe")
	require.NoError(t, os.WriteFile(sourcePath, []byte(blockingCapabilityFFmpegSource), 0o600))
	cmd := exec.Command("go", "build", "-o", oldBinary, sourcePath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "构建能力探测假 ffmpeg 失败: %s", output)
	binary, err := os.ReadFile(oldBinary)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(newBinary, binary, 0o700))
	return oldBinary, newBinary, stateDir
}

const blockingCapabilityFFmpegSource = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	stateDir := os.Getenv("JIANVIDEO_CAPABILITY_PROBE_STATE")
	role := "new"
	if strings.Contains(strings.ToLower(filepath.Base(os.Args[0])), "old") {
		role = "old"
	}
	record, err := os.OpenFile(filepath.Join(stateDir, role+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(record, strings.Join(os.Args[1:], " "))
	record.Close()

	blockPoint := os.Getenv("JIANVIDEO_CAPABILITY_PROBE_BLOCK_POINT")
	if hasArg("-version") {
		if role == "old" && blockPoint == "version" {
			blockAt(stateDir, "version")
		}
		fmt.Printf("ffmpeg version %s-capability-test\n", role)
		return
	}
	if hasArg("-encoders") {
		if role == "old" && blockPoint == "probe" {
			blockAt(stateDir, "probe")
		}
		fmt.Print("Encoders:\n V..... = Video\n ------\n V....D libx264 fake encoder\n")
	}
}

func blockAt(stateDir, point string) {
	if err := os.WriteFile(filepath.Join(stateDir, point+"-started"), []byte("started"), 0600); err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(stateDir, point+"-release")); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hasArg(target string) bool {
	for _, arg := range os.Args[1:] {
		if arg == target {
			return true
		}
	}
	return false
}
`

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

func TestCapabilityService_CleanCacheRecordsAudit(t *testing.T) {
	db := newCapabilityTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.AuditEvent{}))
	writeCacheForCurrentVersion(t, db, "ffmpeg version audit", []EncoderProbeResult{
		{Encoder: "libx264", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	})
	svc := NewCapabilityService(db)

	setProbeSnapshot([]EncoderProbeResult{
		{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true},
	})
	t.Cleanup(clearProbeSnapshot)
	require.NoError(t, svc.CleanCache(context.Background(), audit.NewRecorder(db)))

	enc, dev, err := SelectBestEncoder()
	require.NoError(t, err)
	assert.Equal(t, "libx264", enc, "清理缓存后不得继续使用旧能力快照")
	assert.Empty(t, dev)

	var cacheCount int64
	require.NoError(t, db.Model(&models.CodecProbeCache{}).Count(&cacheCount).Error)
	assert.Equal(t, int64(0), cacheCount)
	var auditCount int64
	require.NoError(t, db.Model(&models.AuditEvent{}).Where("action = ?", "cache.cleaned").Count(&auditCount).Error)
	assert.Equal(t, int64(1), auditCount)
}

func TestCapabilityService_CleanCacheTransactionFailureKeepsSnapshot(t *testing.T) {
	db := newCapabilityTestDB(t)
	writeCacheForCurrentVersion(t, db, "ffmpeg version transaction", []EncoderProbeResult{
		{Encoder: "libx264", Family: "software", Codec: "h264", Compiled: true, TestedOK: true},
	})
	setProbeSnapshot([]EncoderProbeResult{{Encoder: "h264_amf", Family: "amf", Codec: "h264", TestedOK: true}})
	t.Cleanup(clearProbeSnapshot)

	err := NewCapabilityService(db).CleanCache(context.Background(), failingCapabilityAuditRecorder{})
	require.ErrorContains(t, err, "审计写入失败")

	enc, dev, selectErr := SelectBestEncoder()
	require.NoError(t, selectErr)
	assert.Equal(t, "h264_amf", enc, "事务失败时必须保留仍与数据库一致的旧快照")
	assert.Equal(t, "d3d11va", dev)
	var cacheCount int64
	require.NoError(t, db.Model(&models.CodecProbeCache{}).Count(&cacheCount).Error)
	assert.Equal(t, int64(1), cacheCount, "事务失败应回滚缓存删除")
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
