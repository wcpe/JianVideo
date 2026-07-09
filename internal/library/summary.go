package library

import (
	"sort"

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
// 视频/图片拆分复用媒体类型规则服务，避免扫描、筛选和统计口径分叉。
// by_library 用单次 GROUP BY library_id + format 一次取齐，避免逐库逐格式查询（N+1）。
func (s *Service) GetLibrarySummary() (*Summary, error) {
	return s.GetLibrarySummaryInSpace(models.DefaultSpaceID)
}

// GetLibrarySummaryInSpace 聚合指定 Space 的媒体库总量。
func (s *Service) GetLibrarySummaryInSpace(spaceID string) (*Summary, error) {
	spaceID = normalizeSpaceID(spaceID)
	summary := &Summary{ByLibrary: []SummaryRow{}}
	if err := s.fillSummaryLibraryCount(spaceID, summary); err != nil {
		return nil, err
	}
	if err := s.fillRuleBasedSummary(spaceID, summary); err != nil {
		return nil, err
	}
	return summary, nil
}

type summaryFormatRow struct {
	LibraryID     int64
	Label         string
	Format        string
	MediaCount    int
	TotalSize     int64
	TotalDuration float64
}

func (s *Service) fillSummaryLibraryCount(spaceID string, summary *Summary) error {
	var count int64
	if err := s.db.Model(&models.LibraryPath{}).Where("space_id = ? AND enabled = ?", spaceID, 1).Count(&count).Error; err != nil {
		return err
	}
	summary.LibraryCount = int(count)
	return nil
}

func (s *Service) fillRuleBasedSummary(spaceID string, summary *Summary) error {
	rows, err := s.loadSummaryFormatRows(spaceID)
	if err != nil {
		return err
	}
	byLibrary := map[int64]*SummaryRow{}
	policies := map[int64]mediaExtensionPolicy{}
	for _, row := range rows {
		mediaType := s.summaryMediaType(spaceID, row.LibraryID, row.Format, policies)
		applySummaryFormatRow(summary, byLibrary, row, mediaType)
	}
	summary.ByLibrary = sortedSummaryRows(byLibrary)
	return nil
}

func (s *Service) loadSummaryFormatRows(spaceID string) ([]summaryFormatRow, error) {
	var rows []summaryFormatRow
	err := s.db.Model(&models.MediaFile{}).
		Select(
			"media_files.library_id AS library_id, "+
				"library_paths.label AS label, "+
				"LOWER(media_files.format) AS format, "+
				"COUNT(*) AS media_count, "+
				"COALESCE(SUM(media_files.file_size), 0) AS total_size, "+
				"COALESCE(SUM(media_files.duration), 0) AS total_duration").
		Joins("LEFT JOIN library_paths ON library_paths.id = media_files.library_id").
		Where("media_files.space_id = ? AND media_files.deleted_at IS NULL AND "+activeFileStateCondition(), spaceID).
		Group("media_files.library_id, LOWER(media_files.format)").
		Scan(&rows).Error
	return rows, err
}

func (s *Service) summaryMediaType(spaceID string, libraryID int64, ext string, policies map[int64]mediaExtensionPolicy) string {
	policy, ok := policies[libraryID]
	if !ok {
		var err error
		policy, err = s.mediaExtensionPolicyInSpace(spaceID, libraryID)
		if err != nil {
			policy = mediaExtensionPolicy{}
		}
		policies[libraryID] = policy
	}
	mediaType, _ := policy.MediaTypeByExtension(ext)
	return mediaType
}

func applySummaryFormatRow(summary *Summary, byLibrary map[int64]*SummaryRow, row summaryFormatRow, mediaType string) {
	summary.Total += row.MediaCount
	summary.TotalSize += row.TotalSize
	summary.TotalDuration += row.TotalDuration
	item := summaryRowForLibrary(byLibrary, row)
	item.MediaCount += row.MediaCount
	item.TotalSize += row.TotalSize
	item.TotalDuration += row.TotalDuration
	switch mediaType {
	case MediaTypeImage:
		summary.ImageCount += row.MediaCount
		item.ImageCount += row.MediaCount
	case MediaTypeVideo:
		summary.VideoCount += row.MediaCount
		item.VideoCount += row.MediaCount
	}
}

func summaryRowForLibrary(byLibrary map[int64]*SummaryRow, row summaryFormatRow) *SummaryRow {
	if item, ok := byLibrary[row.LibraryID]; ok {
		return item
	}
	item := &SummaryRow{LibraryID: row.LibraryID, Label: row.Label}
	byLibrary[row.LibraryID] = item
	return item
}

func sortedSummaryRows(byLibrary map[int64]*SummaryRow) []SummaryRow {
	rows := make([]SummaryRow, 0, len(byLibrary))
	for _, item := range byLibrary {
		rows = append(rows, *item)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].MediaCount == rows[j].MediaCount {
			return rows[i].LibraryID < rows[j].LibraryID
		}
		return rows[i].MediaCount > rows[j].MediaCount
	})
	return rows
}

func (r *gormMediaRepository) LibrarySummary(spaceID string) (*Summary, error) {
	summary := &Summary{
		ByLibrary: []SummaryRow{},
	}

	if err := r.fillSummaryTotals(spaceID, summary); err != nil {
		return nil, err
	}
	if err := r.fillSummaryLibraryCount(spaceID, summary); err != nil {
		return nil, err
	}
	if err := r.fillSummaryByLibrary(spaceID, summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// fillSummaryTotals 单次扫描 media_files 取总数、视频/图片拆分、SUM(file_size)、SUM(duration)。
// 视频/图片用 SUM(CASE WHEN ...) 在同一聚合内完成，避免多趟查询。
func (r *gormMediaRepository) fillSummaryTotals(spaceID string, summary *Summary) error {
	imageExts := builtInImageExtensionList()
	type totalsRow struct {
		Total         int
		VideoCount    int
		ImageCount    int
		TotalSize     int64
		TotalDuration float64
	}
	var row totalsRow
	if err := r.db.Model(&models.MediaFile{}).
		Select(
			"COUNT(*) AS total, "+
				"SUM(CASE WHEN LOWER(format) IN (?) THEN 0 ELSE 1 END) AS video_count, "+
				"SUM(CASE WHEN LOWER(format) IN (?) THEN 1 ELSE 0 END) AS image_count, "+
				"COALESCE(SUM(file_size), 0) AS total_size, "+
				"COALESCE(SUM(duration), 0) AS total_duration",
			imageExts, imageExts).
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
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
func (r *gormMediaRepository) fillSummaryLibraryCount(spaceID string, summary *Summary) error {
	var count int64
	if err := r.db.Model(&models.LibraryPath{}).Where("space_id = ? AND enabled = ?", normalizeSpaceID(spaceID), 1).Count(&count).Error; err != nil {
		return err
	}
	summary.LibraryCount = int(count)
	return nil
}

// fillSummaryByLibrary 单次 GROUP BY library_id 取各库聚合，LEFT JOIN library_paths 取 label。
// 视频/图片拆分同样用 SUM(CASE WHEN ...) 在同一聚合内完成，避免逐库查询（N+1）。
func (r *gormMediaRepository) fillSummaryByLibrary(spaceID string, summary *Summary) error {
	imageExts := builtInImageExtensionList()
	var rows []SummaryRow
	if err := r.db.Model(&models.MediaFile{}).
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
		Where("media_files.space_id = ? AND media_files.deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
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
