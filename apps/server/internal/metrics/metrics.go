// Package metrics 实现系统指标采样与下采样查询（FR-119，见 ADR-0044）。
//
// 职责边界（架构红线）：采样与聚合逻辑全部在本层；db 仅负责 metric_samples 表读写、
// 不含业务逻辑；依赖方向 metrics → db 单向，metrics 不反向依赖任何业务服务，
// 转码并发计数经构造时注入的 provider 取得，避免反向耦合。
package metrics

import "time"

// SampleInterval 采样周期：每 15 秒采样一行（固定常量，YAGNI，不可配置）。
const SampleInterval = 15 * time.Second

// Retention 样本保留期：保留最近 7 天，超期样本每次采样后裁剪，防表无限增长。
const Retention = 7 * 24 * time.Hour

// maxPoints 下采样后返回点数的安全上界（窗口/桶组合天然保证 ≤ ~168，留余量）。
const maxPoints = 400

// MetricPoint 单个时间点的指标值。原始样本（current）与下采样后的桶点共用此结构。
type MetricPoint struct {
	T               time.Time `json:"t"`                // 时间点（桶起始或样本时刻，UTC）
	CPUPercent      float64   `json:"cpu_percent"`      // 系统 CPU 使用率 %（桶内 AVG）
	MemUsedBytes    int64     `json:"mem_used_bytes"`   // 进程已用内存（桶内 AVG）
	MemSysBytes     int64     `json:"mem_sys_bytes"`    // 进程申请内存（桶内 AVG）
	DiskUsedBytes   int64     `json:"disk_used_bytes"`  // 数据盘已用（桶内 MAX）
	DiskTotalBytes  int64     `json:"disk_total_bytes"` // 数据盘总量（桶内 MAX）
	TranscodeActive int       `json:"transcode_active"` // 活跃转码会话数（桶内 MAX）
	Goroutines      int       `json:"goroutines"`       // goroutine 数（桶内 AVG）
}

// SystemMetrics GET /api/system/metrics 的响应。
type SystemMetrics struct {
	Range   string        `json:"range"`   // 归一化后的 range（1h/24h/7d）
	Points  []MetricPoint `json:"points"`  // 下采样时序，按时间升序，无样本时为空切片
	Current *MetricPoint  `json:"current"` // 最新一条原始样本，无样本时为 null
}

// rangeWindow 一个 range 选项对应的查询窗口与下采样桶大小。
type rangeWindow struct {
	normalized string        // 归一化 range 名
	window     time.Duration // 查询时间窗口（now 往前回溯）
	bucket     time.Duration // 下采样时间桶大小
}

// resolveRange 把请求的 range 字符串映射为窗口与桶大小（纯函数，便于穷举测试）。
// 取值：1h→桶 60s、24h→桶 300s、7d→桶 1800s；缺省/未知一律按 24h 处理。
func resolveRange(rangeStr string) rangeWindow {
	switch rangeStr {
	case "1h":
		return rangeWindow{normalized: "1h", window: time.Hour, bucket: 60 * time.Second}
	case "7d":
		return rangeWindow{normalized: "7d", window: 7 * 24 * time.Hour, bucket: 30 * time.Minute}
	case "24h":
		return rangeWindow{normalized: "24h", window: 24 * time.Hour, bucket: 5 * time.Minute}
	default:
		// 缺省与未知值均回退 24h，保证端点对任意输入都返回稳定结果
		return rangeWindow{normalized: "24h", window: 24 * time.Hour, bucket: 5 * time.Minute}
	}
}
