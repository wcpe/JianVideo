package models

import "time"

// 扫描任务状态常量（FR-29）。
const (
	// ScanTaskStatusPending 已入队、等待 worker 执行。
	ScanTaskStatusPending = "pending"
	// ScanTaskStatusRunning worker 正在执行。
	ScanTaskStatusRunning = "running"
	// ScanTaskStatusCompleted 执行完成。
	ScanTaskStatusCompleted = "completed"
	// ScanTaskStatusError 执行出错。
	ScanTaskStatusError = "error"
)

// 扫描类型常量（FR-29）。full/incremental 字段先落库，
// 两者执行差异留 FR-27 对接，本期 worker 统一按全量行为调用。
const (
	// ScanTypeFull 全量扫描。
	ScanTypeFull = "full"
	// ScanTypeIncremental 增量扫描。
	ScanTypeIncremental = "incremental"
)

// ScanTask 扫描任务队列记录（FR-29）：多次触发的扫描排队、由单 worker 串行执行。
// 队列以本表为持久化真源，服务重启时把残留 running 重置为 pending 重新入队。
type ScanTask struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	SpaceID      string     `gorm:"not null;default:space-default;index:idx_scan_tasks_space_status_created" json:"space_id"`
	LibraryID    int64      `gorm:"index;not null" json:"library_id"`
	ScanType     string     `gorm:"not null;default:'full'" json:"scan_type"` // full / incremental
	Status       string     `gorm:"index;not null;default:'pending';index:idx_scan_tasks_space_status_created" json:"status"`
	ScannedFiles int        `gorm:"default:0" json:"scanned_files"`
	TotalFiles   int        `gorm:"default:0" json:"total_files"`
	Error        string     `json:"error"`
	CreatedAt    time.Time  `gorm:"index:idx_scan_tasks_space_status_created" json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}
