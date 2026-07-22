package metrics

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newTestSampler 创建带内存库的采样器并迁移 metric_samples，限单连接保证表对所有访问可见。
// transcodeCount 默认返回 0，需校验注入时由调用方覆盖。
func newTestSampler(t *testing.T) *Sampler {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.MetricSample{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewSampler(gdb, t.TempDir(), func() int { return 0 })
}

// insertSample 直接写入一行样本，精确控制采样时刻与各指标，便于穷举下采样断言。
func insertSample(t *testing.T, s *Sampler, sample models.MetricSample) {
	t.Helper()
	if err := s.db.Create(&sample).Error; err != nil {
		t.Fatalf("写入样本失败: %v", err)
	}
}

// TestGetMetrics_EmptySamples 空样本：points 为非 nil 空切片、current 为 nil、range 归一化。
func TestGetMetrics_EmptySamples(t *testing.T) {
	s := newTestSampler(t)

	result, err := s.GetMetrics("1h")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if result.Range != "1h" {
		t.Errorf("range 期望 1h，实际 %q", result.Range)
	}
	if result.Points == nil {
		t.Errorf("points 必须为非 nil 空切片，实际为 nil")
	}
	if len(result.Points) != 0 {
		t.Errorf("空样本期望 0 个点，实际 %d", len(result.Points))
	}
	if result.Current != nil {
		t.Errorf("空样本期望 current 为 nil，实际 %+v", result.Current)
	}
}

// TestGetMetrics_DownsampleAggregation 校验同桶内 AVG（cpu/mem/goroutine）与 MAX（transcode/disk）聚合正确。
// 在 1h（桶 60s）下，同一分钟内放 3 条样本，期望聚合为 1 个点。
func TestGetMetrics_DownsampleAggregation(t *testing.T) {
	s := newTestSampler(t)

	// 取最近、同一分钟内的三个时刻（避免跨桶）
	base := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Minute).Add(5 * time.Second)
	for i, cpu := range []float64{10, 20, 30} {
		insertSample(t, s, models.MetricSample{
			SampledAt:       base.Add(time.Duration(i) * time.Second),
			CPUPercent:      cpu,
			MemUsedBytes:    int64((i + 1) * 100),
			MemSysBytes:     int64((i + 1) * 200),
			DiskUsedBytes:   int64(1000 + i*10), // 递增，MAX 应取最后一个
			DiskTotalBytes:  5000,
			TranscodeActive: i,            // 0,1,2 → MAX=2
			Goroutines:      (i + 1) * 10, // 10,20,30 → AVG=20
		})
	}

	result, err := s.GetMetrics("1h")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("同桶三样本期望聚合为 1 点，实际 %d 点", len(result.Points))
	}
	p := result.Points[0]
	if p.CPUPercent != 20 { // AVG(10,20,30)
		t.Errorf("CPU AVG 期望 20，实际 %v", p.CPUPercent)
	}
	if p.MemUsedBytes != 200 { // AVG(100,200,300)
		t.Errorf("内存 AVG 期望 200，实际 %d", p.MemUsedBytes)
	}
	if p.TranscodeActive != 2 { // MAX(0,1,2)
		t.Errorf("转码并发 MAX 期望 2，实际 %d", p.TranscodeActive)
	}
	if p.DiskUsedBytes != 1020 { // MAX(1000,1010,1020)
		t.Errorf("磁盘已用 MAX 期望 1020，实际 %d", p.DiskUsedBytes)
	}
	if p.Goroutines != 20 { // AVG(10,20,30)
		t.Errorf("goroutine AVG 期望 20，实际 %d", p.Goroutines)
	}
}

// TestGetMetrics_BucketsSeparatedAndOrdered 校验不同时间桶分开成多点且按时间升序。
func TestGetMetrics_BucketsSeparatedAndOrdered(t *testing.T) {
	s := newTestSampler(t)

	now := time.Now().UTC()
	// 三个相隔数分钟的样本（1h 桶 60s，必落不同桶），逆序写入以验证查询侧排序
	times := []time.Time{
		now.Add(-2 * time.Minute),
		now.Add(-10 * time.Minute),
		now.Add(-5 * time.Minute),
	}
	for i, ts := range times {
		insertSample(t, s, models.MetricSample{
			SampledAt:  ts,
			CPUPercent: float64(i + 1),
		})
	}

	result, err := s.GetMetrics("1h")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(result.Points) != 3 {
		t.Fatalf("期望 3 个独立桶，实际 %d", len(result.Points))
	}
	for i := 1; i < len(result.Points); i++ {
		if !result.Points[i].T.After(result.Points[i-1].T) {
			t.Errorf("点未按时间升序：%v 不晚于 %v", result.Points[i].T, result.Points[i-1].T)
		}
	}
}

// TestGetMetrics_WindowExcludesOld 校验超出 range 窗口的样本不计入。
// 1h 窗口下，放一条 2 小时前的样本（应被排除）与一条 10 分钟前的样本（应保留）。
func TestGetMetrics_WindowExcludesOld(t *testing.T) {
	s := newTestSampler(t)

	now := time.Now().UTC()
	insertSample(t, s, models.MetricSample{SampledAt: now.Add(-2 * time.Hour), CPUPercent: 99})
	insertSample(t, s, models.MetricSample{SampledAt: now.Add(-10 * time.Minute), CPUPercent: 50})

	result, err := s.GetMetrics("1h")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("1h 窗口期望仅 1 点（排除 2 小时前样本），实际 %d", len(result.Points))
	}
	if result.Points[0].CPUPercent != 50 {
		t.Errorf("保留点 CPU 期望 50，实际 %v", result.Points[0].CPUPercent)
	}
}

// TestGetMetrics_CurrentIsLatestRawSample 校验 current 取最新一条原始样本（非下采样、不受桶影响）。
func TestGetMetrics_CurrentIsLatestRawSample(t *testing.T) {
	s := newTestSampler(t)

	now := time.Now().UTC()
	insertSample(t, s, models.MetricSample{SampledAt: now.Add(-5 * time.Minute), CPUPercent: 11, TranscodeActive: 1})
	insertSample(t, s, models.MetricSample{SampledAt: now.Add(-1 * time.Minute), CPUPercent: 77, TranscodeActive: 3, MemUsedBytes: 999})

	result, err := s.GetMetrics("24h")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if result.Current == nil {
		t.Fatalf("current 不应为 nil")
	}
	if result.Current.CPUPercent != 77 {
		t.Errorf("current CPU 期望取最新样本 77，实际 %v", result.Current.CPUPercent)
	}
	if result.Current.TranscodeActive != 3 {
		t.Errorf("current 转码并发期望 3，实际 %d", result.Current.TranscodeActive)
	}
	if result.Current.MemUsedBytes != 999 {
		t.Errorf("current 内存期望 999，实际 %d", result.Current.MemUsedBytes)
	}
}

// TestGetMetrics_BucketSizeByRange 校验不同 range 用不同桶大小：
// 两条相隔 ~4 分钟的样本，在 1h（桶 60s）下应分 2 桶，在 7d（桶 1800s=30min）下应并入 1 桶。
func TestGetMetrics_BucketSizeByRange(t *testing.T) {
	s := newTestSampler(t)

	// 对齐到 30 分钟桶内部，确保两点在 7d 桶下同桶（避开 30min 边界）
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(30 * time.Minute).Add(2 * time.Minute)
	insertSample(t, s, models.MetricSample{SampledAt: base, CPUPercent: 10})
	insertSample(t, s, models.MetricSample{SampledAt: base.Add(4 * time.Minute), CPUPercent: 20})

	r1h, err := s.GetMetrics("1h")
	if err != nil {
		t.Fatalf("查询 1h 失败: %v", err)
	}
	if len(r1h.Points) != 2 {
		t.Errorf("1h（桶 60s）相隔 4 分钟应分 2 桶，实际 %d", len(r1h.Points))
	}

	r7d, err := s.GetMetrics("7d")
	if err != nil {
		t.Fatalf("查询 7d 失败: %v", err)
	}
	if len(r7d.Points) != 1 {
		t.Errorf("7d（桶 30min）相隔 4 分钟应并入 1 桶，实际 %d", len(r7d.Points))
	}
}
