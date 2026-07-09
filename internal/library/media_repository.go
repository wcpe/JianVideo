package library

import (
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// MediaPageRequest 描述媒体列表分页请求。
type MediaPageRequest struct {
	Page     int
	PageSize int
	Cursor   string
}

// MediaPageResult 描述媒体列表分页结果。
type MediaPageResult struct {
	Items      []models.MediaFile
	Total      int64
	Page       int
	PageSize   int
	NextCursor string
}

// MediaQueryRepository 封装 FR2-007 范围内的 Space scoped 媒体查询。
type MediaQueryRepository interface {
	ListMediaFiles(filter MediaFilter, page MediaPageRequest) (MediaPageResult, error)
	GetMediaFileByID(spaceID string, id int64) (*models.MediaFile, error)
	ListMediaByPathPrefix(spaceID, prefix string) ([]models.MediaFile, error)
	ListLibraryPaths(spaceID string) ([]models.LibraryPath, error)
	GetLibraryPathByID(spaceID string, id int64) (*models.LibraryPath, error)
	WatchStats(spaceID string) (*WatchStats, error)
	LibrarySummary(spaceID string) (*Summary, error)
	MediaTrends(spaceID string) (*MediaTrends, error)
}

type gormMediaRepository struct {
	db *gorm.DB
}

func newGormMediaRepository(db *gorm.DB) MediaQueryRepository {
	return &gormMediaRepository{db: db}
}

func normalizeSpaceID(spaceID string) string {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return models.DefaultSpaceID
	}
	return spaceID
}

func normalizeMediaPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func (r *gormMediaRepository) ListMediaFiles(filter MediaFilter, page MediaPageRequest) (MediaPageResult, error) {
	page.Page, page.PageSize = normalizeMediaPage(page.Page, page.PageSize)
	filter.SpaceID = normalizeSpaceID(filter.SpaceID)

	query := r.applyMediaFilter(filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return MediaPageResult{}, err
	}

	query = r.applyMediaFilter(filter)
	if page.Cursor != "" && cursorSortSupported(filter.Sort) {
		cursor, err := DecodeMediaCursor(page.Cursor)
		if err != nil {
			return MediaPageResult{}, err
		}
		query = query.Where("(added_at < ? OR (added_at = ? AND id < ?))", cursor.SortTime, cursor.SortTime, cursor.ID)
	} else {
		query = query.Offset((page.Page - 1) * page.PageSize)
	}
	query = applyMediaOrder(query, filter.Sort)

	var items []models.MediaFile
	if err := query.Limit(page.PageSize + 1).Find(&items).Error; err != nil {
		return MediaPageResult{}, err
	}

	nextCursor := ""
	if len(items) > page.PageSize {
		items = items[:page.PageSize]
		if cursorSortSupported(filter.Sort) {
			last := items[len(items)-1]
			token, err := EncodeMediaCursor(MediaCursor{SortTime: last.AddedAt, ID: last.ID})
			if err != nil {
				return MediaPageResult{}, err
			}
			nextCursor = token
		}
	}
	return MediaPageResult{
		Items:      items,
		Total:      total,
		Page:       page.Page,
		PageSize:   page.PageSize,
		NextCursor: nextCursor,
	}, nil
}

func (r *gormMediaRepository) GetMediaFileByID(spaceID string, id int64) (*models.MediaFile, error) {
	var mf models.MediaFile
	if err := r.db.Where("space_id = ? AND id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), id).
		First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

func (r *gormMediaRepository) ListMediaByPathPrefix(spaceID, prefix string) ([]models.MediaFile, error) {
	var allFiles []models.MediaFile
	err := r.db.Where("space_id = ? AND file_path LIKE ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), prefix+"%").
		Order("file_path ASC").Find(&allFiles).Error
	return allFiles, err
}

func (r *gormMediaRepository) ListLibraryPaths(spaceID string) ([]models.LibraryPath, error) {
	var paths []models.LibraryPath
	err := r.db.Where("space_id = ?", normalizeSpaceID(spaceID)).Order("id").Find(&paths).Error
	return paths, err
}

func (r *gormMediaRepository) GetLibraryPathByID(spaceID string, id int64) (*models.LibraryPath, error) {
	var lp models.LibraryPath
	if err := r.db.Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).First(&lp).Error; err != nil {
		return nil, err
	}
	return &lp, nil
}

func (r *gormMediaRepository) applyMediaFilter(filter MediaFilter) *gorm.DB {
	query := r.db.Model(&models.MediaFile{}).Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(filter.SpaceID))
	if filter.LibraryID > 0 {
		query = query.Where("library_id = ?", filter.LibraryID)
	}
	if filter.Search != "" {
		query = applyMultiColumnLike(query, searchableColumns, filter.Search)
	}
	if filter.Favorite != nil {
		query = query.Where("favorite = ?", *filter.Favorite)
	}
	if filter.TagID > 0 {
		taggedMedia := r.db.Table("tag_mappings").
			Select("tag_mappings.media_id").
			Joins("JOIN tags ON tags.id = tag_mappings.tag_id").
			Where("tag_mappings.tag_id = ? AND tags.space_id = ?", filter.TagID, filter.SpaceID)
		query = query.Where("id IN (?)",
			taggedMedia)
	}

	if len(filter.MediaTypeExtensions) > 0 {
		query = query.Where("LOWER(format) IN ?", lowerAll(filter.MediaTypeExtensions))
	} else {
		switch filter.MediaType {
		case MediaTypeImage:
			query = query.Where("LOWER(format) IN ?", builtInImageExtensionList())
		case MediaTypeVideo:
			query = query.Where("LOWER(format) NOT IN ?", builtInImageExtensionList())
		}
	}
	if len(filter.Formats) > 0 {
		query = query.Where("LOWER(format) IN ?", lowerAll(filter.Formats))
	}
	if filter.SizeMin > 0 {
		query = query.Where("file_size >= ?", filter.SizeMin)
	}
	if filter.SizeMax > 0 {
		query = query.Where("file_size <= ?", filter.SizeMax)
	}
	if filter.TimeFrom != nil {
		query = query.Where("COALESCE(media_time, added_at) >= ?", *filter.TimeFrom)
	}
	if filter.TimeTo != nil {
		query = query.Where("COALESCE(media_time, added_at) <= ?", *filter.TimeTo)
	}
	if filter.PathPrefix != "" {
		query = query.Where("file_path LIKE ?", filepath.ToSlash(filter.PathPrefix)+"%")
	}
	if filter.HasGPS {
		query = query.Where("gps_lat != 0 OR gps_lon != 0")
	}
	for _, term := range filter.Terms {
		if term == "" {
			continue
		}
		query = applyMultiColumnLike(query, searchableColumns, term)
	}
	for _, term := range filter.CameraTerms {
		if term == "" {
			continue
		}
		query = query.Where("camera LIKE ?", "%"+escapeLike(term)+"%")
	}
	for _, term := range filter.LensTerms {
		if term == "" {
			continue
		}
		query = query.Where("lens LIKE ?", "%"+escapeLike(term)+"%")
	}
	return query
}

func applyMediaOrder(query *gorm.DB, sortKey string) *gorm.DB {
	switch sortKey {
	case "time_asc":
		return query.Order("added_at ASC, id ASC")
	case "name":
		return query.Order("file_name ASC, id ASC")
	case "media_time":
		return query.Order("COALESCE(media_time, added_at) DESC, id DESC")
	case "media_time_asc":
		return query.Order("COALESCE(media_time, added_at) ASC, id ASC")
	default:
		return query.Order("added_at DESC, id DESC")
	}
}

func cursorSortSupported(sortKey string) bool {
	return sortKey == "" || sortKey == "time_desc"
}

func (r *gormMediaRepository) spaceMediaQuery(spaceID string) *gorm.DB {
	return r.db.Model(&models.MediaFile{}).Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID))
}
