package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

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

// MediaQueryRepository 封装 Space scoped 媒体查询与写路径（FR2-007 / FR2-070 续8/续9）。
type MediaQueryRepository interface {
	ListMediaFiles(filter MediaFilter, page MediaPageRequest) (MediaPageResult, error)
	GetMediaFileByID(spaceID string, id int64) (*models.MediaFile, error)
	// GetMediaFileByIDForViewer 按调用者 max 分级过滤（FR2-051）；不可见 → ErrRecordNotFound。
	GetMediaFileByIDForViewer(spaceID string, id int64, maxContentRating string) (*models.MediaFile, error)
	GetMediaFileByPath(spaceID, path string) (*models.MediaFile, error)
	GetMediaFileByLibraryAndPath(spaceID string, libraryID int64, path string) (*models.MediaFile, error)
	GetMediaFileByLibraryAndPathAnyState(spaceID string, libraryID int64, path string) (*models.MediaFile, error)
	ListMediaByPathPrefix(spaceID, prefix string) ([]models.MediaFile, error)
	ListLibraryPaths(spaceID string) ([]models.LibraryPath, error)
	GetLibraryPathByID(spaceID string, id int64) (*models.LibraryPath, error)
	// CountActiveMediaByLibrary 各库未软删且可用媒体数（path views）。
	CountActiveMediaByLibrary(spaceID string) (map[int64]int64, error)
	WatchStats(spaceID string) (*WatchStats, error)
	LibrarySummary(spaceID string) (*Summary, error)
	// ListSummaryFormatRows 按 library+format 分组聚合（供规则口径 summary）。
	ListSummaryFormatRows(spaceID string) ([]SummaryFormatRow, error)
	MediaTrends(spaceID string) (*MediaTrends, error)

	// Create 插入新媒体记录。
	Create(mf *models.MediaFile) error
	// UpdateField 更新单列；返回 rowsAffected。
	UpdateField(spaceID string, mediaID int64, column string, value any) (int64, error)
	// CountByID 统计指定 Space 下媒体是否存在（含软删，用于幂等写）。
	CountByID(spaceID string, mediaID int64) (int64, error)
	// CountThumbnailCandidates 统计可生成缩略图的活跃媒体数。
	CountThumbnailCandidates(spaceID string) (int64, error)
	// ListThumbnailCandidates 按 ID 游标列出缩略图候选。
	ListThumbnailCandidates(spaceID string, afterID int64, limit int) ([]models.MediaFile, error)

	// GetActiveForSoftDeleteTx 事务内取未软删媒体；不存在返回 gorm.ErrRecordNotFound。
	GetActiveForSoftDeleteTx(tx *gorm.DB, spaceID string, id int64) (*models.MediaFile, error)
	// SoftDeleteTx 事务内置 deleted_at。
	SoftDeleteTx(tx *gorm.DB, spaceID string, id int64, deletedAt time.Time) error
	// ListActiveByIDsTx 事务内按 id 列表取未软删媒体（按 id 升序）。
	ListActiveByIDsTx(tx *gorm.DB, spaceID string, ids []int64) ([]models.MediaFile, error)
	// SoftDeleteByIDsTx 事务内批量置 deleted_at；返回 rowsAffected。
	SoftDeleteByIDsTx(tx *gorm.DB, spaceID string, ids []int64, deletedAt time.Time) (int64, error)
	// ReassignLibraryByIDsTx 事务内批量更新 library_id；返回 rowsAffected。
	ReassignLibraryByIDsTx(tx *gorm.DB, spaceID string, ids []int64, targetLibraryID int64) (int64, error)
	// UpdateFieldsTx 事务内批量更新字段（重命名/移动等）；返回 rowsAffected。
	UpdateFieldsTx(tx *gorm.DB, spaceID string, id int64, updates map[string]any) (int64, error)
	// UpdateFieldsByID 按 id 批量更新字段（扫描 upsert 等不强制 space 条件）。
	UpdateFieldsByID(id int64, updates map[string]any) error
	// GetActiveByLibraryAndPathTx 事务内按 library+path 取未软删媒体。
	GetActiveByLibraryAndPathTx(tx *gorm.DB, spaceID string, libraryID int64, path string) (*models.MediaFile, error)
	// GetByLibraryAndPathIncludingDeleted 含软删记录（扫描 upsert 复活用）。
	GetByLibraryAndPathIncludingDeleted(spaceID string, libraryID int64, path string) (*models.MediaFile, error)
	// DeleteHardByPathTx 按 file_path 硬删媒体及关联 watch/metadata。
	DeleteHardByPathTx(tx *gorm.DB, filePath string) error
	// DeleteHardByLibraryAndPathTx 按 library+path 硬删媒体及关联。
	DeleteHardByLibraryAndPathTx(tx *gorm.DB, libraryID int64, filePath string) error
	// DeleteHardByLibraryIDTx 按 space+library 硬删全部媒体及关联（删库用）。
	DeleteHardByLibraryIDTx(tx *gorm.DB, spaceID string, libraryID int64) error
	// ListByLibraryAndPaths 按 library + 路径列表批量取媒体（扫描 index 去重；含软删/任意 file_state）。
	ListByLibraryAndPaths(spaceID string, libraryID int64, paths []string) ([]models.MediaFile, error)
	// ListActiveForReconcile 全量扫描对账：按 id 游标取未软删且可用媒体的最小字段集。
	ListActiveForReconcile(spaceID string, libraryID int64, afterID int64, limit int) ([]models.MediaFile, error)
	// ListMissingDHash 未软删、active 且 dhash=0 的媒体（按 id 升序，dedup 补算）。
	ListMissingDHash(spaceID string) ([]models.MediaFile, error)
	// SetDHashIfZero 仅当 dhash 仍为 0 时写入（幂等 CAS）；返回 rowsAffected。
	SetDHashIfZero(id int64, dhash int64) (int64, error)
	// ListWithDHash 未软删、active 且 dhash!=0 的媒体（按 id 升序，dedup 聚类）。
	ListWithDHash(spaceID string) ([]models.MediaFile, error)
	// CountContentHashBackfill 统计内容哈希回填候选数（缺失/过期/算法不一致）。
	CountContentHashBackfill(ctx context.Context, spaceID string) (int, error)
	// ListContentHashBackfillBatch 按 id 游标取内容哈希回填批次。
	ListContentHashBackfillBatch(ctx context.Context, spaceID string, afterID int64, limit int) ([]models.MediaFile, error)
	// UpdateContentHash 写入内容哈希及关联字段。
	UpdateContentHash(ctx context.Context, spaceID string, mediaID int64, updates map[string]any) error
	// ListExactDuplicateMedia 查询有效精确重复组内的媒体（按组首 id、媒体 id 升序）。
	ListExactDuplicateMedia(spaceID string) ([]models.MediaFile, error)
	// RefreshContentHashGroups 重建指定 Space 的内容哈希重复组快照。
	RefreshContentHashGroups(ctx context.Context, spaceID string) error
	// ListByIDsActive 批量取未软删媒体（不保证顺序）。
	ListByIDsActive(spaceID string, ids []int64) ([]models.MediaFile, error)
	// ListDeleted 回收站列表，按 deleted_at 倒序。
	ListDeleted(spaceID string) ([]models.MediaFile, error)
	// ListExpiredDeleted 列出 deleted_at 早于 before 的软删项（最旧优先），最多 limit 条（FR2-054）。
	ListExpiredDeleted(spaceID string, before time.Time, limit int) ([]models.MediaFile, error)
	// GetDeletedForRestoreTx 事务内取可还原软删媒体（排除清理中 claim 态）。
	GetDeletedForRestoreTx(tx *gorm.DB, spaceID string, id int64) (*models.MediaFile, error)
	// ClearDeletedAtTx 事务内 CAS 清空 deleted_at；返回 rowsAffected。
	ClearDeletedAtTx(tx *gorm.DB, spaceID string, id int64, expectedDeletedAt *time.Time) (int64, error)
	// UpdateFileStateCAS 非事务 CAS 更新 file_state（匹配 deleted_at + 当前 state）；返回 rowsAffected。
	UpdateFileStateCAS(spaceID string, id int64, deletedAt *time.Time, fromState, toState string) (int64, error)
	// UpdateFileStateCASTx 事务内 CAS 更新 file_state；返回 rowsAffected。
	UpdateFileStateCASTx(tx *gorm.DB, spaceID string, id int64, deletedAt *time.Time, fromState, toState string) (int64, error)
	// GetByIDAndDeletedAtTx 事务内按 space+id+deleted_at 取媒体；不存在返回 found=false。
	GetByIDAndDeletedAtTx(tx *gorm.DB, spaceID string, id int64, deletedAt *time.Time) (*models.MediaFile, bool, error)
	// RunInTx 在事务中执行 fn。
	RunInTx(fn func(tx *gorm.DB) error) error
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
		query = query.Where("(added_at, id) < (?, ?)", cursor.SortTime, cursor.ID)
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
	if err := r.attachMediaInferences(items); err != nil {
		return MediaPageResult{}, err
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
	return r.GetMediaFileByIDForViewer(spaceID, id, "")
}

// GetMediaFileByIDForViewer 按调用者 max 分级取媒体；不可见时返回 gorm.ErrRecordNotFound（对外 404）。
func (r *gormMediaRepository) GetMediaFileByIDForViewer(spaceID string, id int64, maxContentRating string) (*models.MediaFile, error) {
	var mf models.MediaFile
	q := r.db.Where("space_id = ? AND id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), id)
	if max := strings.TrimSpace(maxContentRating); max != "" {
		q = q.Where(
			"(content_rating IS NULL OR content_rating = '' OR UPPER(TRIM(content_rating)) = 'UNRATED' OR UPPER(TRIM(content_rating)) IN ?)",
			contentRatingSQLAllowList(max),
		)
	}
	if err := q.First(&mf).Error; err != nil {
		return nil, err
	}
	return &mf, nil
}

// contentRatingSQLAllowList 生成 SQL IN 列表；含 PG-13 时附带历史别名 PG13。
func contentRatingSQLAllowList(maxRating string) []string {
	allowed := models.ContentRatingsAtMost(maxRating)
	for _, r := range allowed {
		if r == models.ContentRatingPG13 {
			return append(allowed, "PG13")
		}
	}
	return allowed
}

func (r *gormMediaRepository) GetMediaFileByPath(spaceID, path string) (*models.MediaFile, error) {
	var mf models.MediaFile
	err := r.db.Where("space_id = ? AND file_path = ?", normalizeSpaceID(spaceID), filepath.ToSlash(path)).
		Where(activeFileStateCondition()).First(&mf).Error
	if err != nil {
		return nil, err
	}
	return &mf, nil
}

func (r *gormMediaRepository) GetMediaFileByLibraryAndPath(spaceID string, libraryID int64, path string) (*models.MediaFile, error) {
	var mf models.MediaFile
	err := r.db.Where("space_id = ? AND library_id = ? AND file_path = ?", normalizeSpaceID(spaceID), libraryID, filepath.ToSlash(path)).
		Where(activeFileStateCondition()).First(&mf).Error
	if err != nil {
		return nil, err
	}
	return &mf, nil
}

func (r *gormMediaRepository) GetMediaFileByLibraryAndPathAnyState(spaceID string, libraryID int64, path string) (*models.MediaFile, error) {
	var media models.MediaFile
	err := r.db.Where("space_id = ? AND library_id = ? AND file_path = ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), libraryID, filepath.ToSlash(path)).First(&media).Error
	if err != nil {
		return nil, err
	}
	return &media, nil
}

func (r *gormMediaRepository) Create(mf *models.MediaFile) error {
	return r.db.Create(mf).Error
}

func (r *gormMediaRepository) UpdateField(spaceID string, mediaID int64, column string, value any) (int64, error) {
	result := r.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), mediaID).
		Update(column, value)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) CountByID(spaceID string, mediaID int64) (int64, error) {
	var count int64
	err := r.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), mediaID).
		Count(&count).Error
	return count, err
}

func (r *gormMediaRepository) CountThumbnailCandidates(spaceID string) (int64, error) {
	var count int64
	err := r.db.Model(&models.MediaFile{}).
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Count(&count).Error
	return count, err
}

func (r *gormMediaRepository) ListThumbnailCandidates(spaceID string, afterID int64, limit int) ([]models.MediaFile, error) {
	var items []models.MediaFile
	err := r.db.Where("space_id = ? AND id > ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID), afterID).
		Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

func (r *gormMediaRepository) RunInTx(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func (r *gormMediaRepository) GetActiveForSoftDeleteTx(tx *gorm.DB, spaceID string, id int64) (*models.MediaFile, error) {
	var before models.MediaFile
	err := tx.Where("space_id = ? AND id = ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), id).First(&before).Error
	if err != nil {
		return nil, err
	}
	return &before, nil
}

func (r *gormMediaRepository) SoftDeleteTx(tx *gorm.DB, spaceID string, id int64, deletedAt time.Time) error {
	return tx.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).
		Update("deleted_at", deletedAt).Error
}

func (r *gormMediaRepository) ListActiveByIDsTx(tx *gorm.DB, spaceID string, ids []int64) ([]models.MediaFile, error) {
	if len(ids) == 0 {
		return []models.MediaFile{}, nil
	}
	var before []models.MediaFile
	err := tx.Where("space_id = ? AND id IN ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), ids).
		Order("id ASC").Find(&before).Error
	return before, err
}

func (r *gormMediaRepository) SoftDeleteByIDsTx(tx *gorm.DB, spaceID string, ids []int64, deletedAt time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := tx.Model(&models.MediaFile{}).
		Where("space_id = ? AND id IN ?", normalizeSpaceID(spaceID), ids).
		Update("deleted_at", deletedAt)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) ReassignLibraryByIDsTx(tx *gorm.DB, spaceID string, ids []int64, targetLibraryID int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := tx.Model(&models.MediaFile{}).
		Where("space_id = ? AND id IN ?", normalizeSpaceID(spaceID), ids).
		Update("library_id", targetLibraryID)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) UpdateFieldsTx(tx *gorm.DB, spaceID string, id int64, updates map[string]any) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	result := tx.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ?", normalizeSpaceID(spaceID), id).
		Updates(updates)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) UpdateFieldsByID(id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.Model(&models.MediaFile{}).Where("id = ?", id).Updates(updates).Error
}

func (r *gormMediaRepository) GetActiveByLibraryAndPathTx(tx *gorm.DB, spaceID string, libraryID int64, path string) (*models.MediaFile, error) {
	var before models.MediaFile
	err := tx.Where("space_id = ? AND library_id = ? AND file_path = ? AND deleted_at IS NULL",
		normalizeSpaceID(spaceID), libraryID, filepath.ToSlash(path)).First(&before).Error
	if err != nil {
		return nil, err
	}
	return &before, nil
}

func (r *gormMediaRepository) GetByLibraryAndPathIncludingDeleted(spaceID string, libraryID int64, path string) (*models.MediaFile, error) {
	var mf models.MediaFile
	err := r.db.Where("space_id = ? AND library_id = ? AND file_path = ?",
		normalizeSpaceID(spaceID), libraryID, filepath.ToSlash(path)).First(&mf).Error
	if err != nil {
		return nil, err
	}
	return &mf, nil
}

func (r *gormMediaRepository) DeleteHardByPathTx(tx *gorm.DB, filePath string) error {
	return deleteMediaRecordsTx(tx, tx.Where("file_path = ?", filepath.ToSlash(filePath)))
}

func (r *gormMediaRepository) DeleteHardByLibraryAndPathTx(tx *gorm.DB, libraryID int64, filePath string) error {
	return deleteMediaRecordsTx(tx, tx.Where("library_id = ? AND file_path = ?", libraryID, filepath.ToSlash(filePath)))
}

func (r *gormMediaRepository) DeleteHardByLibraryIDTx(tx *gorm.DB, spaceID string, libraryID int64) error {
	return deleteMediaRecordsTx(tx, tx.Where("space_id = ? AND library_id = ?", normalizeSpaceID(spaceID), libraryID))
}

func (r *gormMediaRepository) ListByLibraryAndPaths(spaceID string, libraryID int64, paths []string) ([]models.MediaFile, error) {
	if len(paths) == 0 {
		return []models.MediaFile{}, nil
	}
	var items []models.MediaFile
	err := r.db.Where("space_id = ? AND library_id = ? AND file_path IN ?", normalizeSpaceID(spaceID), libraryID, paths).
		Find(&items).Error
	return items, err
}

func (r *gormMediaRepository) ListActiveForReconcile(spaceID string, libraryID int64, afterID int64, limit int) ([]models.MediaFile, error) {
	if limit <= 0 {
		return []models.MediaFile{}, nil
	}
	var rows []models.MediaFile
	err := r.db.Select("id", "space_id", "library_id", "file_path", "file_name", "file_state").
		Where("space_id = ? AND library_id = ? AND deleted_at IS NULL AND id > ? AND "+activeFileStateCondition(),
			normalizeSpaceID(spaceID), libraryID, afterID).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *gormMediaRepository) ListMissingDHash(spaceID string) ([]models.MediaFile, error) {
	var pending []models.MediaFile
	err := r.db.
		Where("space_id = ? AND deleted_at IS NULL AND dhash = 0 AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Order("id ASC").
		Find(&pending).Error
	return pending, err
}

func (r *gormMediaRepository) SetDHashIfZero(id int64, dhash int64) (int64, error) {
	result := r.db.Model(&models.MediaFile{}).
		Where("id = ? AND dhash = 0", id).
		Update("dhash", dhash)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) ListWithDHash(spaceID string) ([]models.MediaFile, error) {
	var media []models.MediaFile
	err := r.db.
		Where("space_id = ? AND deleted_at IS NULL AND dhash != 0 AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Order("id ASC").
		Find(&media).Error
	return media, err
}

func (r *gormMediaRepository) contentHashBackfillQuery(ctx context.Context, spaceID string) *gorm.DB {
	return r.db.WithContext(ctx).
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Where("(content_hash = '' OR content_hash IS NULL OR content_hash_stale = ? OR content_hash_algo <> ? OR content_hash_algo IS NULL)", true, ContentHashAlgoSHA256)
}

func (r *gormMediaRepository) CountContentHashBackfill(ctx context.Context, spaceID string) (int, error) {
	var count int64
	if err := r.contentHashBackfillQuery(ctx, spaceID).
		Model(&models.MediaFile{}).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *gormMediaRepository) ListContentHashBackfillBatch(ctx context.Context, spaceID string, afterID int64, limit int) ([]models.MediaFile, error) {
	if limit <= 0 {
		return []models.MediaFile{}, nil
	}
	var rows []models.MediaFile
	err := r.contentHashBackfillQuery(ctx, spaceID).
		Where("id > ?", afterID).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *gormMediaRepository) UpdateContentHash(ctx context.Context, spaceID string, mediaID int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&models.MediaFile{}).
		Where("id = ? AND space_id = ?", mediaID, normalizeSpaceID(spaceID)).
		Updates(updates).Error
}

func (r *gormMediaRepository) ListExactDuplicateMedia(spaceID string) ([]models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	duplicateKeys := r.validExactDuplicateKeys(spaceID)
	var items []models.MediaFile
	err := r.db.Table("(?) AS duplicate_keys", duplicateKeys).
		Select("media_files.*").
		Joins("JOIN media_files ON media_files.space_id = duplicate_keys.space_id AND media_files.file_size = duplicate_keys.file_size AND media_files.content_hash = duplicate_keys.content_hash").
		Where("media_files.space_id = ? AND media_files.deleted_at IS NULL AND "+activeMediaFileStateCondition(), spaceID).
		Where("media_files.content_hash_algo = ? AND media_files.content_hash_stale = ?", ContentHashAlgoSHA256, false).
		Order("duplicate_keys.first_media_id ASC, media_files.id ASC").
		Find(&items).Error
	return items, err
}

func (r *gormMediaRepository) validExactDuplicateKeys(spaceID string) *gorm.DB {
	candidates := r.db.Model(&models.MediaHashGroup{}).
		Select("space_id, file_size, content_hash").
		Where("space_id = ? AND content_hash_algo = ? AND item_count >= 2", spaceID, ContentHashAlgoSHA256)
	return r.db.Table("(?) AS duplicate_keys", candidates).
		Select("duplicate_keys.space_id, duplicate_keys.file_size, duplicate_keys.content_hash, MIN(media_files.id) AS first_media_id").
		Joins("JOIN media_files ON media_files.space_id = duplicate_keys.space_id AND media_files.file_size = duplicate_keys.file_size AND media_files.content_hash = duplicate_keys.content_hash").
		Where("media_files.space_id = ? AND media_files.deleted_at IS NULL AND "+activeMediaFileStateCondition(), spaceID).
		Where("media_files.content_hash_algo = ? AND media_files.content_hash_stale = ?", ContentHashAlgoSHA256, false).
		Group("duplicate_keys.space_id, duplicate_keys.file_size, duplicate_keys.content_hash").
		Having("COUNT(media_files.id) >= 2").
		Order("first_media_id ASC").
		Limit(exactDuplicateGroupLimit)
}

func (r *gormMediaRepository) RefreshContentHashGroups(ctx context.Context, spaceID string) error {
	spaceID = normalizeSpaceID(spaceID)
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ?", spaceID).Delete(&models.MediaHashGroup{}).Error; err != nil {
			return err
		}
		insert := `
			INSERT INTO media_hash_groups(
				space_id, file_size, content_hash, content_hash_algo, item_count, first_media_id, updated_at
			)
			SELECT
				space_id, file_size, content_hash, ?, COUNT(*), MIN(id), ?
			FROM media_files
			WHERE space_id = ?
				AND deleted_at IS NULL
				AND (file_state IS NULL OR file_state = '' OR file_state = ?)
				AND content_hash <> ''
				AND content_hash_algo = ?
				AND content_hash_stale = 0
			GROUP BY space_id, file_size, content_hash
			HAVING COUNT(*) >= 2`
		return tx.Exec(insert, ContentHashAlgoSHA256, now, spaceID, models.MediaFileStateAvailable, ContentHashAlgoSHA256).Error
	})
}

func (r *gormMediaRepository) ListByIDsActive(spaceID string, ids []int64) ([]models.MediaFile, error) {
	if len(ids) == 0 {
		return []models.MediaFile{}, nil
	}
	var items []models.MediaFile
	err := r.db.Where("space_id = ? AND id IN ? AND deleted_at IS NULL", normalizeSpaceID(spaceID), ids).Find(&items).Error
	return items, err
}

func (r *gormMediaRepository) ListDeleted(spaceID string) ([]models.MediaFile, error) {
	var items []models.MediaFile
	err := r.db.Where("space_id = ? AND deleted_at IS NOT NULL", normalizeSpaceID(spaceID)).
		Order("deleted_at DESC").Find(&items).Error
	return items, err
}

func (r *gormMediaRepository) ListExpiredDeleted(spaceID string, before time.Time, limit int) ([]models.MediaFile, error) {
	if limit <= 0 {
		limit = 50
	}
	var items []models.MediaFile
	err := r.db.Where("space_id = ? AND deleted_at IS NOT NULL AND deleted_at < ?", normalizeSpaceID(spaceID), before).
		Order("deleted_at ASC").Limit(limit).Find(&items).Error
	return items, err
}

func (r *gormMediaRepository) GetDeletedForRestoreTx(tx *gorm.DB, spaceID string, id int64) (*models.MediaFile, error) {
	var before models.MediaFile
	err := tx.Where("space_id = ? AND id = ? AND deleted_at IS NOT NULL", normalizeSpaceID(spaceID), id).
		Where("file_state IS NULL OR file_state NOT LIKE ?", recycleClaimStatePrefix+"%").
		First(&before).Error
	if err != nil {
		return nil, err
	}
	return &before, nil
}

func (r *gormMediaRepository) ClearDeletedAtTx(tx *gorm.DB, spaceID string, id int64, expectedDeletedAt *time.Time) (int64, error) {
	result := tx.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ? AND deleted_at = ?", normalizeSpaceID(spaceID), id, expectedDeletedAt).
		UpdateColumn("deleted_at", nil)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) UpdateFileStateCAS(spaceID string, id int64, deletedAt *time.Time, fromState, toState string) (int64, error) {
	return r.UpdateFileStateCASTx(r.db, spaceID, id, deletedAt, fromState, toState)
}

func (r *gormMediaRepository) UpdateFileStateCASTx(tx *gorm.DB, spaceID string, id int64, deletedAt *time.Time, fromState, toState string) (int64, error) {
	result := tx.Model(&models.MediaFile{}).
		Where("space_id = ? AND id = ? AND deleted_at = ? AND file_state = ?", normalizeSpaceID(spaceID), id, deletedAt, fromState).
		UpdateColumn("file_state", toState)
	return result.RowsAffected, result.Error
}

func (r *gormMediaRepository) GetByIDAndDeletedAtTx(tx *gorm.DB, spaceID string, id int64, deletedAt *time.Time) (*models.MediaFile, bool, error) {
	var current models.MediaFile
	err := tx.Where("space_id = ? AND id = ? AND deleted_at = ?", normalizeSpaceID(spaceID), id, deletedAt).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &current, true, nil
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

func (r *gormMediaRepository) CountActiveMediaByLibrary(spaceID string) (map[int64]int64, error) {
	type countRow struct {
		LibraryID int64
		Count     int64
	}
	var rows []countRow
	if err := r.db.Model(&models.MediaFile{}).
		Select("library_id, COUNT(*) AS count").
		Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(spaceID)).
		Group("library_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, row := range rows {
		out[row.LibraryID] = row.Count
	}
	return out, nil
}

func (r *gormMediaRepository) applyMediaFilter(filter MediaFilter) *gorm.DB {
	query := r.db.Model(&models.MediaFile{}).Where("space_id = ? AND deleted_at IS NULL AND "+activeFileStateCondition(), normalizeSpaceID(filter.SpaceID))
	if filter.LibraryID > 0 {
		query = query.Where("library_id = ?", filter.LibraryID)
	}
	// 家长控制（FR2-051）：有 max 时只返回可见分级（UNRATED/空始终可见）。
	if max := strings.TrimSpace(filter.MaxContentRating); max != "" {
		// 空串与 UNRATED 始终放行；其余须在 allowed 列表（max=G 时 allowed=[G]）。
		query = query.Where(
			"(content_rating IS NULL OR content_rating = '' OR UPPER(TRIM(content_rating)) = 'UNRATED' OR UPPER(TRIM(content_rating)) IN ?)",
			contentRatingSQLAllowList(max),
		)
	}
	if filter.Search != "" {
		query = r.applyTermMatch(query, filter.SpaceID, filter.Search)
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
	query = r.applyInferenceFilter(query, filter)

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
		// FR2-046：裸词同时命中文件字段与推断片名
		query = r.applyTermMatch(query, filter.SpaceID, term)
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
	// FR2-046：时长与分辨率
	if filter.DurationMin > 0 {
		query = query.Where("duration >= ?", filter.DurationMin)
	}
	if filter.DurationMax > 0 {
		query = query.Where("duration <= ?", filter.DurationMax)
	}
	if filter.WidthMin > 0 {
		query = query.Where("width >= ?", filter.WidthMin)
	}
	if filter.WidthMax > 0 {
		query = query.Where("width <= ?", filter.WidthMax)
	}
	if filter.HeightMin > 0 {
		query = query.Where("height >= ?", filter.HeightMin)
	}
	if filter.HeightMax > 0 {
		query = query.Where("height <= ?", filter.HeightMax)
	}
	return query
}

// applyTermMatch 将单个关键词约束为：可搜列任一命中，或同 Space 推断标题命中（FR2-046）。
func (r *gormMediaRepository) applyTermMatch(query *gorm.DB, spaceID, term string) *gorm.DB {
	pattern := "%" + escapeLike(term) + "%"
	clauses := make([]string, 0, len(searchableColumns)+1)
	args := make([]any, 0, len(searchableColumns)+1)
	for _, col := range searchableColumns {
		clauses = append(clauses, col+" LIKE ?")
		args = append(args, pattern)
	}
	if r.db.Migrator().HasTable(&models.MediaInference{}) {
		inferred := r.db.Model(&models.MediaInference{}).
			Select("media_id").
			Where("space_id = ? AND title LIKE ?", normalizeSpaceID(spaceID), pattern)
		clauses = append(clauses, "id IN (?)")
		args = append(args, inferred)
	}
	return query.Where(strings.Join(clauses, " OR "), args...)
}

func (r *gormMediaRepository) applyInferenceFilter(query *gorm.DB, filter MediaFilter) *gorm.DB {
	if filter.InferenceStatus == "" {
		return query
	}
	if !r.db.Migrator().HasTable(&models.MediaInference{}) {
		if filter.InferenceStatus == InferenceStatusMissing {
			return query
		}
		return query.Where("1 = 0")
	}
	subquery := r.db.Model(&models.MediaInference{}).
		Select("media_id").
		Where("space_id = ?", normalizeSpaceID(filter.SpaceID))
	switch filter.InferenceStatus {
	case InferenceStatusInferred:
		return query.Where("id IN (?)", subquery)
	case InferenceStatusAuto:
		return query.Where("id IN (?)", subquery.Where("manual = ?", false))
	case InferenceStatusManual:
		return query.Where("id IN (?)", subquery.Where("manual = ?", true))
	case InferenceStatusMissing:
		return query.Where("id NOT IN (?)", subquery)
	default:
		return query
	}
}

func (r *gormMediaRepository) attachMediaInferences(items []models.MediaFile) error {
	if len(items) == 0 || !r.db.Migrator().HasTable(&models.MediaInference{}) {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	var inferences []models.MediaInference
	if err := r.db.Where("media_id IN ?", ids).Find(&inferences).Error; err != nil {
		return err
	}
	byMediaID := make(map[int64]*models.MediaInference, len(inferences))
	for i := range inferences {
		byMediaID[inferences[i].MediaID] = &inferences[i]
	}
	for i := range items {
		items[i].Inference = byMediaID[items[i].ID]
	}
	return nil
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
	case "duration":
		return query.Order("duration DESC, id DESC")
	case "duration_asc":
		return query.Order("duration ASC, id ASC")
	case "resolution":
		// 以高度为主键近似清晰度档位，同高再比宽度
		return query.Order("height DESC, width DESC, id DESC")
	case "resolution_asc":
		return query.Order("height ASC, width ASC, id ASC")
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
