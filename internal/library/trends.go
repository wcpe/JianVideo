package library

import (
	"github.com/wcpe/JianVideo/internal/db/models"
)

// MediaTrends 媒体增长趋势聚合结果（FR-118，增强 FR-75）。
// 仅统计未软删媒体（deleted_at IS NULL），供前端按天累加得累计增长曲线。
type MediaTrends struct {
	MediaAdded []MediaTrendPoint `json:"media_added"` // 按 added_at 本地时区天分桶的新增序列，仅含有新增的天，升序
}

// MediaTrendPoint 趋势序列的一天：日期（本地，YYYY-MM-DD）与当天新增聚合。
type MediaTrendPoint struct {
	Date     string  `json:"date"`     // 本地时区日期 YYYY-MM-DD
	Count    int     `json:"count"`    // 当天新增媒体数
	Size     int64   `json:"size"`     // 当天新增媒体占用合计（字节，SUM(file_size)）
	Duration float64 `json:"duration"` // 当天新增媒体时长合计（秒，SUM(duration)，图片为 0 不影响）
}

// GetMediaTrends 聚合「按天新增媒体」全时段序列（FR-118）：纯查询、全程带 deleted_at IS NULL、无副作用。
// 一次 GROUP BY 按本地时区天分桶，时区口径与 stats.go 的 recent_timeline 一致（strftime(..., 'localtime')）。
// 仅含有新增的天、按日期升序；空库返回非 nil 空切片以契约一致。
func (s *Service) GetMediaTrends() (*MediaTrends, error) {
	trends := &MediaTrends{
		MediaAdded: []MediaTrendPoint{},
	}

	var rows []MediaTrendPoint
	if err := s.db.Model(&models.MediaFile{}).
		Select(
			"strftime('%Y-%m-%d', added_at, 'localtime') AS date, " +
				"COUNT(*) AS count, " +
				"COALESCE(SUM(file_size), 0) AS size, " +
				"COALESCE(SUM(duration), 0) AS duration").
		Where("deleted_at IS NULL").
		Group("date").
		Order("date ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	// 空库时 Scan 留 nil，保持初始化的非 nil 空切片以契约一致
	if rows != nil {
		trends.MediaAdded = rows
	}
	return trends, nil
}
