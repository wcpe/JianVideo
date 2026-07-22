package models

import "time"

// MetricSample 系统指标采样行（FR-119，见 ADR-0044）。
// 由 metrics 采样器按固定周期写入，每行为一次完整采样快照；
// SampledAt 加索引以支撑按时间窗口的下采样查询与保留期裁剪。
type MetricSample struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SampledAt       time.Time `gorm:"column:sampled_at;index" json:"sampled_at"`       // 采样时刻（UTC 存储）
	CPUPercent      float64   `gorm:"column:cpu_percent" json:"cpu_percent"`           // 系统 CPU 使用率 %
	MemUsedBytes    int64     `gorm:"column:mem_used_bytes" json:"mem_used_bytes"`     // 进程已用内存（runtime MemStats Alloc）
	MemSysBytes     int64     `gorm:"column:mem_sys_bytes" json:"mem_sys_bytes"`       // 进程向 OS 申请内存（Sys）
	DiskUsedBytes   int64     `gorm:"column:disk_used_bytes" json:"disk_used_bytes"`   // 数据盘已用
	DiskTotalBytes  int64     `gorm:"column:disk_total_bytes" json:"disk_total_bytes"` // 数据盘总量
	TranscodeActive int       `gorm:"column:transcode_active" json:"transcode_active"` // 活跃转码/播放会话数
	Goroutines      int       `gorm:"column:goroutines" json:"goroutines"`             // goroutine 数
}

// TableName 指定表名，保持与 spec 契约一致。
func (MetricSample) TableName() string {
	return "metric_samples"
}
