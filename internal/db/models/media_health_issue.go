package models

import "time"

// 媒体健康问题类型常量（FR-73）。
const (
	// HealthIssueBroken 视频损坏：ffprobe 无法解析容器/流。
	HealthIssueBroken = "broken"
	// HealthIssueZeroByte 0 字节文件。
	HealthIssueZeroByte = "zero_byte"
	// HealthIssueMissing 源文件不存在（本地路径 os.Stat 失败，排除 SMB）。
	HealthIssueMissing = "missing"
	// HealthIssueNoThumbnail 缩略图缺失且无法生成。
	HealthIssueNoThumbnail = "no_thumbnail"
)

// MediaHealthIssue 媒体健康巡检发现的问题记录（FR-73）。
// 独立于 media_files（不在其上加列），是巡检的只读报告快照：
// 每轮巡检先清空全表再写入，绝不改写 media_files.deleted_at（软删真源归 FR-25/27）。
type MediaHealthIssue struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	SpaceID   string    `gorm:"not null;default:space-default;index:idx_health_space_type_media,priority:1" json:"space_id"`
	MediaID   int64     `gorm:"not null;index:idx_health_space_type_media,priority:3" json:"media_id"`
	IssueType string    `gorm:"not null;index:idx_health_space_type_media,priority:2" json:"issue_type"` // broken / zero_byte / missing / no_thumbnail
	Detail    string    `json:"detail"`                                                                  // 问题细节（如 ffprobe 错误尾部）
	CheckedAt time.Time `json:"checked_at"`                                                              // 本轮巡检判定时刻
}
