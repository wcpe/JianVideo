// Package ai 提供 AI 可替换管线与搜索审核的数据访问与服务实现。
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// GormRepository GORM 实现。
type GormRepository struct {
	db *gorm.DB
}

// NewGormRepository 创建仓库。
func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// ListModels 列出全部模型。
func (r *GormRepository) ListModels(ctx context.Context) ([]models.AIModel, error) {
	var rows []models.AIModel
	err := r.db.WithContext(ctx).Order("id asc").Find(&rows).Error
	return rows, err
}

// GetModel 按 ID 取模型。
func (r *GormRepository) GetModel(ctx context.Context, id string) (*models.AIModel, error) {
	var row models.AIModel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpsertModel 插入或更新模型。
func (r *GormRepository) UpsertModel(ctx context.Context, m *models.AIModel) error {
	if m == nil || strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("模型无效")
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "version", "task_type", "status", "endpoint", "updated_at"}),
	}).Create(m).Error
}

// ListNodes 列出节点。
func (r *GormRepository) ListNodes(ctx context.Context) ([]models.AIInferenceNode, error) {
	var rows []models.AIInferenceNode
	err := r.db.WithContext(ctx).Order("id asc").Find(&rows).Error
	return rows, err
}

// GetNode 按 ID 取节点。
func (r *GormRepository) GetNode(ctx context.Context, id string) (*models.AIInferenceNode, error) {
	var row models.AIInferenceNode
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpsertNode 插入或更新节点。
func (r *GormRepository) UpsertNode(ctx context.Context, n *models.AIInferenceNode) error {
	if n == nil || strings.TrimSpace(n.ID) == "" {
		return fmt.Errorf("节点无效")
	}
	now := time.Now().UTC()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "kind", "endpoint", "enabled", "task_types_json", "updated_at"}),
	}).Create(n).Error
}

// ListResultsByMedia 按 Space+media 列结果。
func (r *GormRepository) ListResultsByMedia(ctx context.Context, spaceID string, mediaID int64) ([]models.AIResult, error) {
	var rows []models.AIResult
	err := r.db.WithContext(ctx).Where("space_id = ? AND media_id = ?", spaceID, mediaID).
		Order("id desc").Find(&rows).Error
	return rows, err
}

// ListResultsBySpace 按 Space 列全部结果，支持可选 task_type 与 manual 过滤。
func (r *GormRepository) ListResultsBySpace(ctx context.Context, spaceID string, taskType string, manual *bool) ([]models.AIResult, error) {
	q := r.db.WithContext(ctx).Where("space_id = ?", spaceID)
	if strings.TrimSpace(taskType) != "" {
		q = q.Where("task_type = ?", taskType)
	}
	if manual != nil {
		q = q.Where("manual = ?", *manual)
	}
	var rows []models.AIResult
	err := q.Order("id desc").Find(&rows).Error
	return rows, err
}

// CreateResult 写入结果。
func (r *GormRepository) CreateResult(ctx context.Context, res *models.AIResult) error {
	if res == nil {
		return fmt.Errorf("结果无效")
	}
	now := time.Now().UTC()
	if res.CreatedAt.IsZero() {
		res.CreatedAt = now
	}
	res.UpdatedAt = now
	return r.db.WithContext(ctx).Create(res).Error
}

// UpsertEmbedding 按 space+media+model 覆盖写入向量。
func (r *GormRepository) UpsertEmbedding(ctx context.Context, e *models.AIEmbedding) error {
	if e == nil || strings.TrimSpace(e.SpaceID) == "" || e.MediaID <= 0 || strings.TrimSpace(e.ModelID) == "" {
		return fmt.Errorf("向量无效")
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "space_id"}, {Name: "media_id"}, {Name: "model_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"model_version", "dim", "batch_id", "vector", "updated_at"}),
	}).Create(e).Error
}

// ListEmbeddingsBySpaceModel 列出某 Space 下指定模型的全部向量。
func (r *GormRepository) ListEmbeddingsBySpaceModel(ctx context.Context, spaceID, modelID string) ([]models.AIEmbedding, error) {
	var rows []models.AIEmbedding
	err := r.db.WithContext(ctx).Where("space_id = ? AND model_id = ?", spaceID, modelID).Find(&rows).Error
	return rows, err
}

// DeleteEmbeddings 按条件删除向量（可重建）。
func (r *GormRepository) DeleteEmbeddings(ctx context.Context, spaceID string, mediaID int64, modelID, batchID string) (int64, error) {
	q := r.db.WithContext(ctx).Where("space_id = ?", spaceID)
	if mediaID > 0 {
		q = q.Where("media_id = ?", mediaID)
	}
	if strings.TrimSpace(modelID) != "" {
		q = q.Where("model_id = ?", modelID)
	}
	if strings.TrimSpace(batchID) != "" {
		q = q.Where("batch_id = ?", batchID)
	}
	res := q.Delete(&models.AIEmbedding{})
	return res.RowsAffected, res.Error
}

// DeleteNonManualResults 删除非人工确认结果。
func (r *GormRepository) DeleteNonManualResults(ctx context.Context, spaceID string, mediaID int64, taskType, batchID string) (int64, error) {
	q := r.db.WithContext(ctx).Where("space_id = ? AND manual = ?", spaceID, false)
	if mediaID > 0 {
		q = q.Where("media_id = ?", mediaID)
	}
	if strings.TrimSpace(taskType) != "" {
		q = q.Where("task_type = ?", taskType)
	}
	if strings.TrimSpace(batchID) != "" {
		q = q.Where("batch_id = ?", batchID)
	}
	res := q.Delete(&models.AIResult{})
	return res.RowsAffected, res.Error
}

// GetResult 按 Space + id 取结果。
func (r *GormRepository) GetResult(ctx context.Context, spaceID string, id int64) (*models.AIResult, error) {
	var row models.AIResult
	err := r.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateResult 更新结果行。
func (r *GormRepository) UpdateResult(ctx context.Context, res *models.AIResult) error {
	if res == nil || res.ID <= 0 {
		return fmt.Errorf("结果无效")
	}
	res.UpdatedAt = time.Now().UTC()
	return r.db.WithContext(ctx).Save(res).Error
}

// DeleteResult 删除单条结果。
func (r *GormRepository) DeleteResult(ctx context.Context, spaceID string, id int64) error {
	res := r.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, id).Delete(&models.AIResult{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
