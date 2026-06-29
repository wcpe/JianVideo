package library

import (
	"github.com/wcpe/JianVideo/internal/db/models"
)

// Summary 媒体库总量聚合结果（FR-117）。
// 所有计数/求和均仅统计未软删媒体（deleted_at IS NULL）。
type Summary struct {
	Total         int          `json:"total"`          // 未软删媒体总数
	VideoCount    int          `json:"video_count"`    // 视频数（format 不在内置图片后缀集合内）
	ImageCount    int          `json:"image_count"`    // 图片数（format 在内置图片后缀集合内）
	TotalSize     int64        `json:"total_size"`     // 占用空间合计（字节，SUM(file_size)）
	TotalDuration float64      `json:"total_duration"` // 时长合计（秒，SUM(duration)，图片为 0 不影响）
	LibraryCount  int          `json:"library_count"`  // 启用的媒体库数（library_paths.enabled=1）
	ByLibrary     []SummaryRow `json:"by_library"`     // 各库聚合（按 library_id 分组）
}

// SummaryRow 单个媒体库的聚合行（FR-117）。
type SummaryRow struct {
	LibraryID     int64   `json:"library_id"`
	Label         string  `json:"label"`          // 库名（取自 library_paths，LEFT JOIN）
	MediaCount    int     `json:"media_count"`    // 该库未软删媒体数
	VideoCount    int     `json:"video_count"`    // 该库视频数
	ImageCount    int     `json:"image_count"`    // 该库图片数
	TotalSize     int64   `json:"total_size"`     // 该库占用空间合计（字节）
	TotalDuration float64 `json:"total_duration"` // 该库时长合计（秒）
}

// GetLibrarySummary 聚合媒体库总量（FR-117）：纯查询、全程带 deleted_at IS NULL、无副作用。
// 视频/图片拆分复用全站一致谓词（builtInImageExtensionList）：图片=LOWER(format) IN 集合，视频=NOT IN。
// by_library 用单次 GROUP BY library_id（LEFT JOIN library_paths 取 label）一次取齐，避免逐库查询（N+1）。
func (s *Service) GetLibrarySummary() (*Summary, error) {
	summary := &Summary{
		ByLibrary: []SummaryRow{},
	}

	if err := s.fillSummaryTotals(summary); err != nil {
		return nil, err
	}
	if err := s.fillSummaryLibraryCount(summary); err != nil {
		return nil, err
	}
	if err := s.fillSummaryByLibrary(summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// fillSummaryTotals 单次扫描 media_files 取总数、视频/图片拆分、SUM(file_size)、SUM(duration)。
// 视频/图片用 SUM(CASE WHEN ...) 在同一聚合内完成，避免多趟查询。
func (s *Service) fillSummaryTotals(summary *Summary) error {
	imageExts := builtInImageExtensionList()
	type totalsRow struct {
		Total         int
		VideoCount    int
		ImageCount    int
		TotalSize     int64
		TotalDuration float64
	}
	var row totalsRow
	if err := s.db.Model(&models.MediaFile{}).
		Select(
			"COUNT(*) AS total, "+
				"SUM(CASE WHEN LOWER(format) IN (?) THEN 0 ELSE 1 END) AS video_count, "+
				"SUM(CASE WHEN LOWER(format) IN (?) THEN 1 ELSE 0 END) AS image_count, "+
				"COALESCE(SUM(file_size), 0) AS total_size, "+
				"COALESCE(SUM(duration), 0) AS total_duration",
			imageExts, imageExts).
		Where("deleted_at IS NULL").
		Scan(&row).Error; err != nil {
		return err
	}
	summary.Total = row.Total
	summary.VideoCount = row.VideoCount
	summary.ImageCount = row.ImageCount
	summary.TotalSize = row.TotalSize
	summary.TotalDuration = row.TotalDuration
	return nil
}

// fillSummaryLibraryCount 统计启用的媒体库数（enabled=1），与 /paths 口径一致。
func (s *Service) fillSummaryLibraryCount(summary *Summary) error {
	var count int64
	if err := s.db.Model(&models.LibraryPath{}).Where("enabled = ?", 1).Count(&count).Error; err != nil {
		return err
	}
	summary.LibraryCount = int(count)
	return nil
}

// fillSummaryByLibrary 单次 GROUP BY library_id 取各库聚合，LEFT JOIN library_paths 取 label。
// 视频/图片拆分同样用 SUM(CASE WHEN ...) 在同一聚合内完成，避免逐库查询（N+1）。
func (s *Service) fillSummaryByLibrary(summary *Summary) error {
	imageExts := builtInImageExtensionList()
	var rows []SummaryRow
	if err := s.db.Model(&models.MediaFile{}).
		Select(
			"media_files.library_id AS library_id, "+
				"library_paths.label AS label, "+
				"COUNT(*) AS media_count, "+
				"SUM(CASE WHEN LOWER(media_files.format) IN (?) THEN 0 ELSE 1 END) AS video_count, "+
				"SUM(CASE WHEN LOWER(media_files.format) IN (?) THEN 1 ELSE 0 END) AS image_count, "+
				"COALESCE(SUM(media_files.file_size), 0) AS total_size, "+
				"COALESCE(SUM(media_files.duration), 0) AS total_duration",
			imageExts, imageExts).
		Joins("LEFT JOIN library_paths ON library_paths.id = media_files.library_id").
		Where("media_files.deleted_at IS NULL").
		Group("media_files.library_id").
		Order("media_count DESC").
		Scan(&rows).Error; err != nil {
		return err
	}
	// 空库时 Scan 留 nil，保持初始化的非 nil 空切片以契约一致
	if rows != nil {
		summary.ByLibrary = rows
	}
	return nil
}
