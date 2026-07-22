package models

import "time"

const (
	// TaskScopeSpace 表示归属某个 Space 的业务任务。
	TaskScopeSpace = "space"
	// TaskScopeSystem 表示系统级全局任务。
	TaskScopeSystem = "system"
)

const (
	// TaskStatusPending 表示任务等待领取。
	TaskStatusPending = "pending"
	// TaskStatusRunning 表示任务正在执行。
	TaskStatusRunning = "running"
	// TaskStatusSucceeded 表示任务执行成功。
	TaskStatusSucceeded = "succeeded"
	// TaskStatusFailed 表示任务已失败且不可继续执行。
	TaskStatusFailed = "failed"
	// TaskStatusCanceled 表示任务被用户取消。
	TaskStatusCanceled = "canceled"
)

// Task 是通用异步任务队列中心的持久化真源记录。
type Task struct {
	ID             int64      `gorm:"primaryKey;index:idx_tasks_space_status_priority_created,priority:5;index:idx_tasks_type_status_priority_created,priority:5;index:idx_tasks_scope_space_type_status_id,priority:5" json:"id"`
	Scope          string     `gorm:"not null;index;index:idx_tasks_scope_space_type_status_id,priority:1" json:"scope"`
	SpaceID        *string    `gorm:"index:idx_tasks_space_status_priority_created,priority:1;index:idx_tasks_space_type_status_updated,priority:1;index:idx_tasks_scope_space_type_status_id,priority:2" json:"space_id,omitempty"`
	Type           string     `gorm:"not null;index:idx_tasks_type_status_priority_created,priority:1;index:idx_tasks_space_type_status_updated,priority:2;index:idx_tasks_scope_space_type_status_id,priority:3" json:"type"`
	Status         string     `gorm:"not null;index:idx_tasks_space_status_priority_created,priority:2;index:idx_tasks_type_status_priority_created,priority:2;index:idx_tasks_space_type_status_updated,priority:3;index:idx_tasks_status_next_run_priority_created,priority:1;index:idx_tasks_scope_space_type_status_id,priority:4" json:"status"`
	Priority       int        `gorm:"not null;default:0;index:idx_tasks_space_status_priority_created,priority:3;index:idx_tasks_type_status_priority_created,priority:3;index:idx_tasks_status_next_run_priority_created,priority:3" json:"priority"`
	Attempts       int        `gorm:"not null;default:0" json:"attempts"`
	MaxAttempts    int        `gorm:"not null;default:1" json:"max_attempts"`
	Progress       int        `gorm:"not null;default:0" json:"progress"`
	Checkpoint     string     `json:"checkpoint,omitempty"`
	IdempotencyKey string     `gorm:"index" json:"idempotency_key,omitempty"`
	PayloadJSON    string     `json:"payload_json,omitempty"`
	ResourceType   string     `gorm:"index:idx_tasks_resource,priority:1" json:"resource_type,omitempty"`
	ResourceID     string     `gorm:"index:idx_tasks_resource,priority:2" json:"resource_id,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `gorm:"not null;index:idx_tasks_space_status_priority_created,priority:4;index:idx_tasks_type_status_priority_created,priority:4;index:idx_tasks_status_next_run_priority_created,priority:4" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null;index:idx_tasks_space_type_status_updated,priority:4" json:"updated_at"`
	NextRunAt      *time.Time `gorm:"index:idx_tasks_status_next_run_priority_created,priority:2" json:"next_run_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}
