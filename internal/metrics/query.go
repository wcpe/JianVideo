package metrics

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// GetMetrics 按 range 下采样返回时序与当前快照（FR-119）。
// 窗口与桶大小由 resolveRange 决定，桶内聚合：CPU/内存/goroutine 取 AVG、
// 转码并发与磁盘用量取 MAX；结果按时间升序、点数有界（≤ ~maxPoints）。
// current 为最新一条原始样本；无任何样本时 points 为空切片、current 为 nil（HTTP 仍 200）。
func (s *Sampler) GetMetrics(rangeStr string) (*SystemMetrics, error) {
	rw := resolveRange(rangeStr)
	result := &SystemMetrics{
		Range:  rw.normalized,
		Points: []MetricPoint{},
	}

	points, err := s.queryBuckets(rw)
	if err != nil {
		return nil, err
	}
	if points != nil {
		result.Points = points
	}

	current, err := s.queryCurrent()
	if err != nil {
		return nil, err
	}
	result.Current = current

	return result, nil
}

// bucketRow 下采样聚合查询的扫描目标。
// bucket_start 为桶起始的 unix 秒（整数），由 (unixepoch / 桶秒) * 桶秒 求得。
type bucketRow struct {
	BucketStart     int64   `gorm:"column:bucket_start"`
	CPUPercent      float64 `gorm:"column:cpu_percent"`
	MemUsedBytes    float64 `gorm:"column:mem_used_bytes"`
	MemSysBytes     float64 `gorm:"column:mem_sys_bytes"`
	DiskUsedBytes   int64   `gorm:"column:disk_used_bytes"`
	DiskTotalBytes  int64   `gorm:"column:disk_total_bytes"`
	TranscodeActive int     `gorm:"column:transcode_active"`
	Goroutines      float64 `gorm:"column:goroutines"`
}

// queryBuckets 执行时间桶分组聚合，返回升序时序点。
// 用 unixepoch(sampled_at) 把存储的时间转 unix 秒，整除桶秒分桶；
// 仅扫描窗口内样本（走 sampled_at 索引），保证点数有界。
func (s *Sampler) queryBuckets(rw rangeWindow) ([]MetricPoint, error) {
	bucketSec := int64(rw.bucket / time.Second)
	since := time.Now().UTC().Add(-rw.window)

	var rows []bucketRow
	if err := s.db.Model(&models.MetricSample{}).
		Select(
			"(unixepoch(sampled_at) / ?) * ? AS bucket_start, "+
				"AVG(cpu_percent) AS cpu_percent, "+
				"AVG(mem_used_bytes) AS mem_used_bytes, "+
				"AVG(mem_sys_bytes) AS mem_sys_bytes, "+
				"MAX(disk_used_bytes) AS disk_used_bytes, "+
				"MAX(disk_total_bytes) AS disk_total_bytes, "+
				"MAX(transcode_active) AS transcode_active, "+
				"AVG(goroutines) AS goroutines",
			bucketSec, bucketSec).
		Where("sampled_at >= ?", since).
		Group("bucket_start").
		Order("bucket_start ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	points := make([]MetricPoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, MetricPoint{
			T:               time.Unix(r.BucketStart, 0).UTC(),
			CPUPercent:      r.CPUPercent,
			MemUsedBytes:    int64(r.MemUsedBytes),
			MemSysBytes:     int64(r.MemSysBytes),
			DiskUsedBytes:   r.DiskUsedBytes,
			DiskTotalBytes:  r.DiskTotalBytes,
			TranscodeActive: r.TranscodeActive,
			Goroutines:      int(r.Goroutines),
		})
	}
	return points, nil
}

// queryCurrent 取最新一条原始样本作当前值卡数据源；无样本时返回 nil。
func (s *Sampler) queryCurrent() (*MetricPoint, error) {
	var sample models.MetricSample
	// 以自增 id 倒序取最新一条：id 随采样插入单调递增，最大 id 即最近样本。
	// 不用 sampled_at 排序——其文本序列化为变宽小数秒，字符串比较不可靠（高风险）。
	err := s.db.Model(&models.MetricSample{}).
		Order("id DESC").
		Limit(1).
		Take(&sample).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 无样本（刚启动）不视为错误，返回 nil current
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &MetricPoint{
		T:               sample.SampledAt.UTC(),
		CPUPercent:      sample.CPUPercent,
		MemUsedBytes:    sample.MemUsedBytes,
		MemSysBytes:     sample.MemSysBytes,
		DiskUsedBytes:   sample.DiskUsedBytes,
		DiskTotalBytes:  sample.DiskTotalBytes,
		TranscodeActive: sample.TranscodeActive,
		Goroutines:      sample.Goroutines,
	}, nil
}
