package library

import "time"

// 健康巡检状态常量（FR-73）。
const (
	// healthStatusIdle 未开始 / 空闲。
	healthStatusIdle = "idle"
	// healthStatusScanning 巡检进行中。
	healthStatusScanning = "scanning"
	// healthStatusCompleted 巡检完成。
	healthStatusCompleted = "completed"
	// healthStatusError 巡检出错。
	healthStatusError = "error"
)

// HealthScanStatus 描述当前健康巡检进度（FR-73），结构参照 ScanStatus。
type HealthScanStatus struct {
	Status      string    `json:"status"`       // idle / scanning / completed / error
	Total       int       `json:"total"`        // 待巡检的未软删媒体总数
	Checked     int       `json:"checked"`      // 已巡检的媒体数
	IssueCount  int       `json:"issue_count"`  // 本轮发现的问题数
	Error       string    `json:"error"`        // 错误信息（status=error 时）
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}
