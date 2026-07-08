package transcoder

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

// warmCacheTimeout 后台预热实测的整体超时上限。
const warmCacheTimeout = 5 * time.Minute

// CapabilityService 硬件加速能力服务：以编码器实测为唯一真源，
// 结果按 ffmpeg 版本持久化于 SQLite，副作用（实测 + 读写库）隔离于此层。
type CapabilityService struct {
	db *gorm.DB
	mu sync.Mutex // 串行化实测，防并发重复实测
}

// NewCapabilityService 创建能力服务。
func NewCapabilityService(db *gorm.DB) *CapabilityService {
	return &CapabilityService{db: db}
}

// CodecResults 返回当前 ffmpeg 版本的编码器实测结果。
// force=false 时命中当前版本缓存即返回；未命中或 force 则实测并持久化（按版本 upsert）。
// 每次实测后刷新进程级快照供选码使用。
func (s *CapabilityService) CodecResults(ctx context.Context, force bool) (results []EncoderProbeResult, fromCache bool, version string, testedAt time.Time, err error) {
	version = FFmpegVersion(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 非强制：先查缓存，命中即返回，不触发实测
	if !force {
		if cached, ok, lookupErr := s.loadCache(version); lookupErr != nil {
			return nil, false, version, time.Time{}, lookupErr
		} else if ok {
			setProbeSnapshot(cached.results)
			return cached.results, true, version, cached.testedAt, nil
		}
	}

	// 未命中或强制：实测并持久化
	results = ProbeEncoders(ctx)
	testedAt = time.Now()
	setProbeSnapshot(results)
	if err = s.saveCache(version, results, testedAt); err != nil {
		// 持久化失败不影响本次结果返回，仅记日志
		log.Printf("[WARN] 编码器实测结果持久化失败: version=%q, err=%v", version, err)
	}
	return results, false, version, testedAt, nil
}

// Capabilities 只读缓存（当前 ffmpeg 版本）派生 per-codec 能力，绝不触发实测。
// 命中：BuildCapabilities + 填 FromCache/版本/实测时间，并刷新选码快照。
// 未命中（冷态）：返回 BuildCapabilities(nil) 的「未测」结果，不阻塞。
func (s *CapabilityService) Capabilities(ctx context.Context) *HWAccelInfo {
	version := FFmpegVersion(ctx)

	cached, ok, err := s.loadCache(version)
	if err != nil {
		log.Printf("[WARN] 读取编码器实测缓存失败: version=%q, err=%v", version, err)
	}
	if err != nil || !ok {
		// 冷态：未测，返回软件兜底默认，不阻塞
		info := BuildCapabilities(nil)
		info.FromCache = false
		info.FFmpegVersion = version
		info.TestedAt = ""
		return info
	}

	setProbeSnapshot(cached.results)
	info := BuildCapabilities(cached.results)
	info.FromCache = true
	info.FFmpegVersion = version
	info.TestedAt = cached.testedAt.Format(time.RFC3339)
	return info
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
func (s *CapabilityService) CleanCache(ctx context.Context, rec audit.Recorder) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
}

// cacheEntry 解码后的缓存条目。
type cacheEntry struct {
	results  []EncoderProbeResult
	testedAt time.Time
}

// loadCache 按 ffmpeg 版本读取缓存；版本为空或无记录返回 ok=false。
func (s *CapabilityService) loadCache(version string) (cacheEntry, bool, error) {
	if version == "" {
		return cacheEntry{}, false, nil
	}
	var row models.CodecProbeCache
	err := s.db.Where("ffmpeg_version = ?", version).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return cacheEntry{}, false, nil
		}
		return cacheEntry{}, false, err
	}
	var results []EncoderProbeResult
	if err := json.Unmarshal([]byte(row.Results), &results); err != nil {
		// 缓存损坏视为未命中，下次将重测覆盖
		log.Printf("[WARN] 编码器实测缓存解码失败，将重测: version=%q, err=%v", version, err)
		return cacheEntry{}, false, nil
	}
	return cacheEntry{results: results, testedAt: row.TestedAt}, true, nil
}

// saveCache 按 ffmpeg 版本 upsert 实测结果；版本为空则跳过（无可靠键）。
func (s *CapabilityService) saveCache(version string, results []EncoderProbeResult, testedAt time.Time) error {
	if version == "" {
		return nil
	}
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	row := models.CodecProbeCache{FFmpegVersion: version, Results: string(raw), TestedAt: testedAt}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ffmpeg_version"}},
		DoUpdates: clause.AssignmentColumns([]string{"results", "tested_at"}),
	}).Create(&row).Error
}
