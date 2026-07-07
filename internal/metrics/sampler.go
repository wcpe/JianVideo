package metrics

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// Sampler 系统指标采样器：后台周期采样写 SQLite，并提供下采样查询。
// 生命周期由主入口装配：Start 起后台 goroutine + ticker，Stop 取消并等待干净退出（不泄漏）。
type Sampler struct {
	db             *gorm.DB
	dataDir        string     // 数据盘采样目标（磁盘用量取该目录所在文件系统）
	transcodeCount func() int // 活跃转码/播放会话数 provider，由播放服务注入

	mu      sync.Mutex // 保护 cancel/done，确保 Start/Stop 幂等且并发安全
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
}

// NewSampler 构造采样器。transcodeCount 为活跃会话计数 provider，不应为 nil；
// 若调用方暂无来源，可传 func() int { return 0 }。
func NewSampler(db *gorm.DB, dataDir string, transcodeCount func() int) *Sampler {
	return &Sampler{
		db:             db,
		dataDir:        dataDir,
		transcodeCount: transcodeCount,
	}
}

// Start 启动后台采样循环。重复调用对已启动的采样器无副作用。
// 传入的 ctx 取消或调用 Stop 均会停止采样。
func (s *Sampler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.started = true
	go s.loop(runCtx, s.done)
}

// Stop 停止采样并等待后台 goroutine 退出，保证无泄漏。重复调用安全。
func (s *Sampler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	done := s.done
	s.started = false
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	cancel()
	<-done
}

// loop 采样主循环：先立即采一次（避免冷启动空窗），随后按 SampleInterval 周期采样。
func (s *Sampler) loop(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(SampleInterval)
	defer ticker.Stop()

	// 首次立即采样，避免页面刚开无任何样本
	s.sampleOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sampleOnce(ctx)
		}
	}
}

// sampleOnce 采集一行指标写库，并裁剪超期样本。任一步失败仅记日志、不中断采样循环。
func (s *Sampler) sampleOnce(ctx context.Context) {
	sample := s.collect()
	if err := s.db.WithContext(ctx).Create(&sample).Error; err != nil {
		log.Printf("[WARN] 系统指标采样写入失败: %v", err)
		return
	}
	s.prune(ctx)
}

// collect 采集当前各项指标，组装为一行样本。
// CPU/磁盘用 gopsutil；内存/goroutine 用标准库 runtime（与 system/info 口径一致）；
// 转码并发取注入的 provider。采集子项失败时退化为 0，不阻断整行写入。
func (s *Sampler) collect() models.MetricSample {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	diskUsed, diskTotal := s.collectDisk()

	return models.MetricSample{
		SampledAt:       time.Now().UTC(),
		CPUPercent:      collectCPUPercent(),
		MemUsedBytes:    uint64ToInt64(mem.Alloc),
		MemSysBytes:     uint64ToInt64(mem.Sys),
		DiskUsedBytes:   diskUsed,
		DiskTotalBytes:  diskTotal,
		TranscodeActive: s.transcodeActive(),
		Goroutines:      runtime.NumGoroutine(),
	}
}

// transcodeActive 经注入 provider 取活跃会话数；provider 未注入时退化为 0。
func (s *Sampler) transcodeActive() int {
	if s.transcodeCount == nil {
		return 0
	}
	return s.transcodeCount()
}

// collectCPUPercent 取系统 CPU 使用率（非阻塞，返回距上次调用的均值）。
// 首次调用或采集失败时退化为 0，符合监控语义（首样本可能为 0）。
func collectCPUPercent() float64 {
	pcts, err := cpu.Percent(0, false)
	if err != nil || len(pcts) == 0 {
		return 0
	}
	return pcts[0]
}

// collectDisk 取数据盘已用/总量（字节）。采集失败时退化为 0，不阻断采样。
func (s *Sampler) collectDisk() (used int64, total int64) {
	u, err := disk.Usage(s.dataDir)
	if err != nil || u == nil {
		return 0, 0
	}
	return uint64ToInt64(u.Used), uint64ToInt64(u.Total)
}

// uint64ToInt64 对系统指标做饱和转换，避免极端平台数值溢出。
func uint64ToInt64(v uint64) int64 {
	if v > 1<<63-1 {
		return 1<<63 - 1
	}
	return int64(v)
}

// prune 裁剪超出保留期的样本，保证表有界。失败仅记日志、不影响本次采样。
func (s *Sampler) prune(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-Retention)
	if err := s.db.WithContext(ctx).
		Where("sampled_at < ?", cutoff).
		Delete(&models.MetricSample{}).Error; err != nil {
		log.Printf("[WARN] 系统指标保留期裁剪失败: %v", err)
	}
}
