package transcoder

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

// warmCacheTimeout 后台预热实测的整体超时上限。
const warmCacheTimeout = 5 * time.Minute

// ErrFFmpegPathChanged 表示能力请求期间 FFmpeg 路径已切换，调用方可重试。
var ErrFFmpegPathChanged = errors.New("FFmpeg 路径已变更，请重试")

var (
	ffmpegDigestMu      sync.Mutex
	ffmpegDigestPath    string
	ffmpegDigest        string
	ffmpegDigestSize    int64
	ffmpegDigestModTime int64
)

// CapabilityService 硬件加速能力服务：以编码器实测为唯一真源，
// 结果按 FFmpeg 可执行文件身份持久化于 SQLite，副作用（实测 + 读写库）隔离于此层。
type CapabilityService struct {
	db *gorm.DB
	mu sync.Mutex // 串行化实测，防并发重复实测
}

// NewCapabilityService 创建能力服务。
func NewCapabilityService(db *gorm.DB) *CapabilityService {
	return &CapabilityService{db: db}
}

// CodecResults 返回当前 ffmpeg 版本的编码器实测结果。
// force=false 时命中当前可执行文件缓存即返回；未命中或 force 则实测并持久化（按身份摘要 upsert）。
// 每次实测后刷新进程级快照供选码使用。
func (s *CapabilityService) CodecResults(ctx context.Context, force bool) (results []EncoderProbeResult, fromCache bool, version string, testedAt time.Time, err error) {
	return s.CodecResultsWithAudit(ctx, force, nil)
}

// CodecResultsWithAudit 返回实测结果，并在强制重测成功时写入系统级审计。
// 请求期间 FFmpeg 路径切换时返回 ErrFFmpegPathChanged，不返回旧代次结果。
func (s *CapabilityService) CodecResultsWithAudit(ctx context.Context, force bool, rec audit.Recorder) (results []EncoderProbeResult, fromCache bool, version string, testedAt time.Time, err error) {
	version, path, generation := ffmpegVersionWithPathGeneration(ctx)
	cacheKey := ffmpegCacheKey(version, path)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 非强制：先查缓存，命中即返回，不触发实测
	if !force {
		if cached, ok, lookupErr := s.loadCache(cacheKey); lookupErr != nil {
			if !storeProbeSnapshotForGeneration(generation, nil) {
				return nil, false, version, time.Time{}, ErrFFmpegPathChanged
			}
			return nil, false, version, time.Time{}, lookupErr
		} else if ok {
			if !storeProbeSnapshotForGeneration(generation, &cached.results) {
				return nil, false, version, time.Time{}, ErrFFmpegPathChanged
			}
			return cached.results, true, version, cached.testedAt, nil
		}
		if !storeProbeSnapshotForGeneration(generation, nil) {
			return nil, false, version, time.Time{}, ErrFFmpegPathChanged
		}
	}
	if !ffmpegGenerationMatches(generation) {
		return nil, false, version, time.Time{}, ErrFFmpegPathChanged
	}

	// 未命中或强制：使用版本探测时捕获的路径完成实测并持久化
	results = probeEncodersWithPath(ctx, path)
	testedAt = time.Now()
	current, saveErr := s.saveCacheWithAuditForGeneration(ctx, generation, cacheKey, version, results, testedAt, force, rec)
	if !current {
		return nil, false, version, time.Time{}, ErrFFmpegPathChanged
	}
	if saveErr != nil {
		if !ffmpegGenerationMatches(generation) {
			return nil, false, version, time.Time{}, ErrFFmpegPathChanged
		}
		if force && rec != nil {
			return nil, false, version, time.Time{}, saveErr
		}
		// 持久化失败不影响本次结果返回，仅记日志
		log.Printf("[WARN] 编码器实测结果持久化失败: version=%q, err=%v", version, saveErr)
	}
	if !storeProbeSnapshotForGeneration(generation, &results) {
		return nil, false, version, time.Time{}, ErrFFmpegPathChanged
	}
	return results, false, version, testedAt, nil
}

// Capabilities 只读缓存（当前 FFmpeg 可执行文件）派生 per-codec 能力，绝不触发实测。
// 命中：BuildCapabilities + 填 FromCache/版本/实测时间，并刷新选码快照。
// 未命中（冷态）：清除旧快照并返回 BuildCapabilities(nil) 的「未测」结果。
func (s *CapabilityService) Capabilities(ctx context.Context) *HWAccelInfo {
	version, path, generation := ffmpegVersionWithPathGeneration(ctx)
	cacheKey := ffmpegCacheKey(version, path)

	s.mu.Lock()
	cached, ok, err := s.loadCache(cacheKey)
	current := false
	if err != nil || !ok {
		current = storeProbeSnapshotForGeneration(generation, nil)
	} else {
		current = storeProbeSnapshotForGeneration(generation, &cached.results)
	}
	s.mu.Unlock()
	if !current {
		version = FFmpegVersion(ctx)
		ok = false
	}
	if err != nil {
		log.Printf("[WARN] 读取编码器实测缓存失败: version=%q, err=%v", version, err)
	}
	if err != nil || !ok {
		info := BuildCapabilities(nil)
		info.FromCache = false
		info.FFmpegVersion = version
		return info
	}

	info := BuildCapabilities(cached.results)
	info.FromCache = true
	info.FFmpegVersion = version
	info.TestedAt = cached.testedAt.Format(time.RFC3339)
	return info
}

// LoadCachedSnapshot 同步只读加载当前 FFmpeg 可执行文件缓存，供启动恢复任务前初始化选码快照。
// 未命中、缓存损坏或读取失败时清除旧快照；本方法绝不触发实测或写库。
func (s *CapabilityService) LoadCachedSnapshot(ctx context.Context) error {
	version, path, generation := ffmpegVersionWithPathGeneration(ctx)
	cacheKey := ffmpegCacheKey(version, path)

	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok, err := s.loadCache(cacheKey)
	if err != nil || !ok {
		storeProbeSnapshotForGeneration(generation, nil)
		return err
	}
	storeProbeSnapshotForGeneration(generation, &cached.results)
	return nil
}

// WarmCacheAsync 启动后台 goroutine 预热缓存（非阻塞），带整体超时。
func (s *CapabilityService) WarmCacheAsync() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), warmCacheTimeout)
		defer cancel()
		if _, fromCache, _, _, err := s.CodecResults(ctx, false); err != nil {
			log.Printf("[WARN] 硬件加速能力预热失败: %v", err)
		} else if !fromCache {
			log.Printf("[INFO] 硬件加速能力预热完成（已实测并写入缓存）")
		} else {
			log.Printf("[INFO] 硬件加速能力预热完成（命中缓存）")
		}
	}()
}

// CleanCache 清理硬件加速能力缓存，并在同一数据库事务内写入系统级审计事件。
// 仅事务成功后清除进程快照；事务失败时数据库与快照均保持原状。
func (s *CapabilityService) CleanCache(ctx context.Context, rec audit.Recorder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.CodecProbeCache{}).Count(&count).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.CodecProbeCache{}).Error; err != nil {
			return err
		}
		if rec == nil {
			return nil
		}
		return rec.RecordTx(ctx, tx, audit.EventInput{
			Scope:        audit.ScopeSystem,
			ActorType:    audit.ActorSystem,
			Action:       "cache.cleaned",
			ResourceType: "cache",
			ResourceID:   "codec_probe",
			Metadata:     map[string]any{"cache": "codec_probe", "deleted": count, "summary": fmt.Sprintf("已清理 %d 条硬件加速缓存", count)},
		})
	})
	if err == nil {
		clearProbeSnapshot()
	}
	return err
}

// cacheEntry 解码后的缓存条目。
type cacheEntry struct {
	results  []EncoderProbeResult
	testedAt time.Time
}

// FFmpegCacheIdentity 返回当前 FFmpeg 可执行文件的持久化缓存身份。
func FFmpegCacheIdentity(ctx context.Context) string {
	version, path, _ := ffmpegVersionWithPathGeneration(ctx)
	return ffmpegCacheKey(version, path)
}

// ffmpegCacheKey 以版本、解析后的路径和文件元数据生成稳定缓存键。
// 同版本的不同构建或同路径替换不得复用旧硬件能力结果。
func ffmpegCacheKey(version, path string) string {
	if version == "" || path == "" {
		return ""
	}
	resolved := path
	if found, err := exec.LookPath(path); err == nil {
		resolved = found
	}
	if absolute, err := filepath.Abs(resolved); err == nil {
		resolved = absolute
	}
	identity := version + "\n" + filepath.Clean(resolved)
	if info, err := os.Stat(resolved); err == nil {
		identity += fmt.Sprintf("\n%d\n%d", info.Size(), info.ModTime().UnixNano())
	}
	identity += "\n" + ffmpegContentDigest(resolved)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func ffmpegContentDigest(path string) string {
	ffmpegDigestMu.Lock()
	defer ffmpegDigestMu.Unlock()
	info, statErr := os.Stat(path)
	if statErr == nil && path == ffmpegDigestPath && ffmpegDigest != "" && info.Size() == ffmpegDigestSize && info.ModTime().UnixNano() == ffmpegDigestModTime {
		return ffmpegDigest
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	ffmpegDigestPath = path
	ffmpegDigest = fmt.Sprintf("%x", hash.Sum(nil))
	if statErr == nil {
		ffmpegDigestSize = info.Size()
		ffmpegDigestModTime = info.ModTime().UnixNano()
	} else {
		ffmpegDigestSize = 0
		ffmpegDigestModTime = 0
	}
	return ffmpegDigest
}

func invalidateFFmpegContentDigest() {
	ffmpegDigestMu.Lock()
	ffmpegDigestPath = ""
	ffmpegDigest = ""
	ffmpegDigestSize = 0
	ffmpegDigestModTime = 0
	ffmpegDigestMu.Unlock()
}

// loadCache 按 ffmpeg 可执行文件身份读取缓存；缓存键为空或无记录返回 ok=false。
func (s *CapabilityService) loadCache(cacheKey string) (cacheEntry, bool, error) {
	if cacheKey == "" || s == nil || s.db == nil {
		return cacheEntry{}, false, nil
	}
	var row models.CodecProbeCache
	err := s.db.Where("ffmpeg_version = ?", cacheKey).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return cacheEntry{}, false, nil
		}
		return cacheEntry{}, false, err
	}
	var results []EncoderProbeResult
	if err := json.Unmarshal([]byte(row.Results), &results); err != nil {
		// 缓存损坏视为未命中，下次将重测覆盖
		log.Printf("[WARN] 编码器实测缓存解码失败，将重测: cache_key=%q, err=%v", cacheKey, err)
		return cacheEntry{}, false, nil
	}
	return cacheEntry{results: results, testedAt: row.TestedAt}, true, nil
}

// saveCacheWithAuditForGeneration 仅在路径代次仍匹配时提交缓存事务。
// 路径切换可在写库与审计期间继续进行，提交前核对代次并在过期时回滚。
func (s *CapabilityService) saveCacheWithAuditForGeneration(ctx context.Context, generation uint64, cacheKey, version string, results []EncoderProbeResult, testedAt time.Time, force bool, rec audit.Recorder) (bool, error) {
	if cacheKey == "" {
		return ffmpegGenerationMatches(generation), nil
	}
	tx, err := s.prepareCacheWrite(ctx, cacheKey, version, results, testedAt, force, rec)
	if err != nil {
		return ffmpegGenerationMatches(generation), err
	}
	return commitCacheForGeneration(tx, generation)
}

func ffmpegGenerationMatches(generation uint64) bool {
	ffmpegPathMu.RLock()
	defer ffmpegPathMu.RUnlock()
	return generation == ffmpegPathGeneration
}

func (s *CapabilityService) prepareCacheWrite(ctx context.Context, cacheKey, version string, results []EncoderProbeResult, testedAt time.Time, force bool, rec audit.Recorder) (*gorm.DB, error) {
	raw, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	row := models.CodecProbeCache{FFmpegVersion: cacheKey, Results: string(raw), TestedAt: testedAt}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ffmpeg_version"}},
		DoUpdates: clause.AssignmentColumns([]string{"results", "tested_at"}),
	}).Create(&row).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if force && rec != nil {
		if err := recordProbeRetest(ctx, tx, rec, version, len(results)); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return tx, nil
}

func recordProbeRetest(ctx context.Context, tx *gorm.DB, rec audit.Recorder, version string, resultCount int) error {
	return rec.RecordTx(ctx, tx, audit.EventInput{
		Scope:        audit.ScopeSystem,
		ActorType:    audit.ActorSystem,
		Action:       "codec_probe.retested",
		ResourceType: "codec_probe",
		ResourceID:   version,
		Metadata:     map[string]any{"ffmpeg_version": version, "result_count": resultCount, "summary": "已强制重测硬件加速能力"},
	})
}

func commitCacheForGeneration(tx *gorm.DB, generation uint64) (bool, error) {
	ffmpegPathMu.RLock()
	defer ffmpegPathMu.RUnlock()
	if generation != ffmpegPathGeneration {
		return false, tx.Rollback().Error
	}
	return true, tx.Commit().Error
}
