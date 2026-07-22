// Package migration 提供版本化 SQLite schema 迁移、备份、dry-run 与校验能力。
package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Migration 定义一个可排序、可重入的 schema 迁移步骤。
type Migration struct {
	ID          string
	Description string
	SafeToRetry bool
	Estimate    EstimateFunc
	Up          UpFunc
	Validate    ValidateFunc
}

// EstimateFunc 返回 dry-run 阶段的只读影响预估。
type EstimateFunc func(context.Context, *gorm.DB) (StepPlan, error)

// UpFunc 执行真实迁移，调用方会在事务内传入 tx。
type UpFunc func(context.Context, *gorm.DB) error

// ValidateFunc 执行迁移后的只读校验。
type ValidateFunc func(context.Context, *gorm.DB) (Validation, error)

// Validation 表示单个迁移步骤的校验摘要。
type Validation struct {
	Summary string
}

// Registry 保存按 ID 排序后的迁移步骤。
type Registry struct {
	migrations []Migration
}

// NewRegistry 校验并创建 migration registry。
func NewRegistry(migrations ...Migration) (*Registry, error) {
	seen := make(map[string]struct{}, len(migrations))
	ordered := append([]Migration(nil), migrations...)
	for _, m := range ordered {
		if strings.TrimSpace(m.ID) == "" {
			return nil, fmt.Errorf("migration ID 不能为空")
		}
		if _, ok := seen[m.ID]; ok {
			return nil, fmt.Errorf("重复 migration ID: %s", m.ID)
		}
		seen[m.ID] = struct{}{}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})
	return &Registry{migrations: ordered}, nil
}

// IDs 返回按执行顺序排列的 migration ID。
func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	ids := make([]string, 0, len(r.migrations))
	for _, m := range r.migrations {
		ids = append(ids, m.ID)
	}
	return ids
}

func (r *Registry) all() []Migration {
	if r == nil {
		return nil
	}
	return append([]Migration(nil), r.migrations...)
}
