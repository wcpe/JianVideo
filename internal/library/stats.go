package library

import (
	"github.com/wcpe/JianVideo/internal/db/models"
)

// 观看统计常量（FR-75）
const (
	// positionHeatmapBuckets 续播位置热力的分桶数（10 档，每档 10%）。
	positionHeatmapBuckets = 10
	// recentTimelineDays 最近观看时间线的天数窗口（仅返回最近 N 天有观看的天）。
	recentTimelineDays = 30
	// topViewedLimit 观看次数 Top 榜的条数。
	topViewedLimit = 10
)

// WatchStats 观看统计聚合结果（FR-75），各维度均仅统计未软删媒体。
type WatchStats struct {
	Total           int                 `json:"total"`            // 媒体总数
	Watched         int                 `json:"watched"`          // 已看完数
	Unwatched       int                 `json:"unwatched"`        // 未看完数
	RecentTimeline  []TimelineBucket    `json:"recent_timeline"`  // 最近观看时间线（按天）
	PositionHeatmap []int               `json:"position_heatmap"` // 续播位置分布（10 档，下标 0=0-10%…9=90-100%）
	ByLibrary       []LibraryWatchCount `json:"by_library"`       // 各存储库已看分布
	ByFormat        []FormatWatchCount  `json:"by_format"`        // 各格式已看分布
	TopViewed       []models.MediaFile  `json:"top_viewed"`       // 观看次数 Top N
}

// TimelineBucket 时间线的一天：日期（本地，YYYY-MM-DD）与当天观看媒体数。
type TimelineBucket struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// LibraryWatchCount 某存储库的已看媒体数。
type LibraryWatchCount struct {
	LibraryID int64  `json:"library_id"`
	Label     string `json:"label"`
	Watched   int    `json:"watched"`
}

// FormatWatchCount 某容器格式的已看媒体数。
type FormatWatchCount struct {
	Format  string `json:"format"`
	Watched int    `json:"watched"`
}

// GetWatchStats 聚合观看统计（FR-75）：纯查询现有列、全程带 deleted_at IS NULL，无副作用。
// 复用 FR-44 观看状态列（watched / last_position / last_watched_at）与 FR-75 view_count。
func (s *Service) GetWatchStats() (*WatchStats, error) {
	stats := &WatchStats{
		PositionHeatmap: make([]int, positionHeatmapBuckets),
		RecentTimeline:  []TimelineBucket{},
		ByLibrary:       []LibraryWatchCount{},
		ByFormat:        []FormatWatchCount{},
		TopViewed:       []models.MediaFile{},
	}

	var total int64
	if err := s.db.Model(&models.MediaFile{}).Where("deleted_at IS NULL").Count(&total).Error; err != nil {
		return nil, err
	}
	stats.Total = int(total)

	var watched int64
	if err := s.db.Model(&models.MediaFile{}).Where("deleted_at IS NULL AND watched = ?", true).Count(&watched).Error; err != nil {
		return nil, err
	}
	stats.Watched = int(watched)
	stats.Unwatched = stats.Total - stats.Watched

	if err := s.fillRecentTimeline(stats); err != nil {
		return nil, err
	}
	if err := s.fillPositionHeatmap(stats); err != nil {
		return nil, err
	}
	if err := s.fillByLibrary(stats); err != nil {
		return nil, err
	}
	if err := s.fillByFormat(stats); err != nil {
		return nil, err
	}
	if err := s.fillTopViewed(stats); err != nil {
		return nil, err
	}
	// 兜底：各 fill 用 `var rows []T` 承接查询，零行时 rows 为 nil，会覆盖上面的非空初始化、
	// 进而序列化成 JSON null 让前端 .map 崩溃（如「有媒体但无任何观看记录」的稀疏态）。
	// 这里统一把 nil 收敛为空切片，保证契约「这些字段恒为数组、非 null」（与 empty-states.md 一致）。
	if stats.RecentTimeline == nil {
		stats.RecentTimeline = []TimelineBucket{}
	}
	if stats.ByLibrary == nil {
		stats.ByLibrary = []LibraryWatchCount{}
	}
	if stats.ByFormat == nil {
		stats.ByFormat = []FormatWatchCount{}
	}
	if stats.TopViewed == nil {
		stats.TopViewed = []models.MediaFile{}
	}
	return stats, nil
}

// fillRecentTimeline 按本地时区天分桶最近观看记录（last_watched_at 非空），取最近 N 天。
// 用 strftime(..., 'localtime') 与 watch_state.go 的本地时区口径一致。
func (s *Service) fillRecentTimeline(stats *WatchStats) error {
	var rows []TimelineBucket
	if err := s.db.Model(&models.MediaFile{}).
		Select("strftime('%Y-%m-%d', last_watched_at, 'localtime') AS date, COUNT(*) AS count").
		Where("deleted_at IS NULL AND last_watched_at IS NOT NULL").
		Group("date").
		Order("date DESC").
		Limit(recentTimelineDays).
		Scan(&rows).Error; err != nil {
		return err
	}
	stats.RecentTimeline = rows
	return nil
}

// fillPositionHeatmap 按 last_position/duration 比例把媒体分入 10 档（duration>0 且 last_position>0）。
// 比例收敛到 [0,1]，恰为 1（看到结尾）归入最后一档。
func (s *Service) fillPositionHeatmap(stats *WatchStats) error {
	type ratioRow struct {
		Ratio float64
	}
	var rows []ratioRow
	if err := s.db.Model(&models.MediaFile{}).
		Select("last_position / duration AS ratio").
		Where("deleted_at IS NULL AND duration > 0 AND last_position > 0").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		stats.PositionHeatmap[ratioToBucket(r.Ratio)]++
	}
	return nil
}

// ratioToBucket 把 [0,1] 的观看进度比例映射到热力档下标 [0, positionHeatmapBuckets-1]。
// 纯函数，便于穷举测试；超出范围的比例收敛到边界档。
func ratioToBucket(ratio float64) int {
	if ratio < 0 {
		return 0
	}
	bucket := int(ratio * positionHeatmapBuckets)
	if bucket >= positionHeatmapBuckets {
		return positionHeatmapBuckets - 1
	}
	return bucket
}

// fillByLibrary 按 library_id 统计已看媒体数，并带上库 label。
func (s *Service) fillByLibrary(stats *WatchStats) error {
	var rows []LibraryWatchCount
	if err := s.db.Model(&models.MediaFile{}).
		Select("media_files.library_id AS library_id, library_paths.label AS label, COUNT(*) AS watched").
		Joins("LEFT JOIN library_paths ON library_paths.id = media_files.library_id").
		Where("media_files.deleted_at IS NULL AND media_files.watched = ?", true).
		Group("media_files.library_id").
		Order("watched DESC").
		Scan(&rows).Error; err != nil {
		return err
	}
	stats.ByLibrary = rows
	return nil
}

// fillByFormat 按容器格式统计已看媒体数（空格式归为「未知」由前端处理，这里保留原值）。
func (s *Service) fillByFormat(stats *WatchStats) error {
	var rows []FormatWatchCount
	if err := s.db.Model(&models.MediaFile{}).
		Select("format AS format, COUNT(*) AS watched").
		Where("deleted_at IS NULL AND watched = ?", true).
		Group("format").
		Order("watched DESC").
		Scan(&rows).Error; err != nil {
		return err
	}
	stats.ByFormat = rows
	return nil
}

// fillTopViewed 取观看次数 Top N（view_count>0、未软删，按次数倒序）。
func (s *Service) fillTopViewed(stats *WatchStats) error {
	var items []models.MediaFile
	if err := s.db.
		Where("deleted_at IS NULL AND view_count > 0").
		Order("view_count DESC").
		Limit(topViewedLimit).
		Find(&items).Error; err != nil {
		return err
	}
	stats.TopViewed = items
	return nil
}
