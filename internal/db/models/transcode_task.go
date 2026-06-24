package models

import "time"

// 预生成任务状态常量（FR-77）。与扫描任务状态语义一致，便于前端复用展示范式。
const (
	// TranscodeTaskStatusPending 已入队、等待 worker 执行。
	TranscodeTaskStatusPending = "pending"
	// TranscodeTaskStatusRunning worker 正在执行预转码。
	TranscodeTaskStatusRunning = "running"
	// TranscodeTaskStatusCompleted 预转码完成、切片已缓存。
	TranscodeTaskStatusCompleted = "completed"
	// TranscodeTaskStatusError 预转码出错。
	TranscodeTaskStatusError = "error"
)

// TranscodeTask 转码预生成任务（FR-77）：把「某媒体 + 某预设」入队，单 worker 串行预转码。
// 队列以本表为持久化真源，服务重启时把残留 running 重置为 pending 重新入队。
//
// 入队时把预设的 codec/width/height 快照进任务，使任务执行不强依赖预设此后是否被改/删
// （与 ScanTask 把 scan_type 落到任务一致）。
type TranscodeTask struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	MediaID     int64      `gorm:"index;not null" json:"media_id"`
	PresetID    int64      `gorm:"index;not null" json:"preset_id"`
	Codec       string     `gorm:"not null" json:"codec"` // 入队时快照的目标编码
	Width       int        `gorm:"default:0" json:"width"`
	Height      int        `gorm:"default:0" json:"height"`
	Status      string     `gorm:"index;not null;default:'pending'" json:"status"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
