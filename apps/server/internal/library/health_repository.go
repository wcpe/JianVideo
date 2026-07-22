package library

import (
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// healthRepository 封装媒体健康问题快照与巡检媒体列表的持久化（FR2-070 续7）。
type healthRepository interface {
	// ListIssues 按 issue_type、media_id 升序返回指定 Space 问题清单。
	ListIssues(spaceID string) ([]models.MediaHealthIssue, error)
	// ListActiveMedia 返回指定 Space 未软删媒体，按 id 升序。
	ListActiveMedia(spaceID string) ([]models.MediaFile, error)
	// ReplaceIssues 原子替换指定 Space 的问题快照（先删后插）。
	ReplaceIssues(spaceID string, issues []models.MediaHealthIssue) error
}

type gormHealthRepository struct {
	db *gorm.DB
}

func newGormHealthRepository(db *gorm.DB) healthRepository {
	return &gormHealthRepository{db: db}
}

func (r *gormHealthRepository) ListIssues(spaceID string) ([]models.MediaHealthIssue, error) {
	var issues []models.MediaHealthIssue
	err := r.db.Where("space_id = ?", normalizeSpaceID(spaceID)).
		Order("issue_type ASC, media_id ASC").Find(&issues).Error
	return issues, err
}

func (r *gormHealthRepository) ListActiveMedia(spaceID string) ([]models.MediaFile, error) {
	var media []models.MediaFile
	err := r.db.Where("space_id = ? AND deleted_at IS NULL", normalizeSpaceID(spaceID)).
		Order("id ASC").Find(&media).Error
	return media, err
}

func (r *gormHealthRepository) ReplaceIssues(spaceID string, issues []models.MediaHealthIssue) error {
	spaceID = normalizeSpaceID(spaceID)
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ?", spaceID).Delete(&models.MediaHealthIssue{}).Error; err != nil {
			return err
		}
		if len(issues) == 0 {
			return nil
		}
		return tx.Create(&issues).Error
	})
}
