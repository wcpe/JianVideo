package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

// ConfirmResult 将结果标为人工确认（manual=true），不可被 rebuild 删除。
func (s *Service) ConfirmResult(ctx context.Context, spaceID string, resultID int64, actorID string) error {
	if !s.isEnabled() {
		return ErrAIDisabled
	}
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" || resultID <= 0 {
		return ErrInvalidInput
	}
	res, err := s.repo.GetResult(ctx, spaceID, resultID)
	if err != nil {
		return err
	}
	if res == nil {
		return ErrNotFound
	}
	if res.Manual {
		return nil
	}
	res.Manual = true
	res.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateResult(ctx, res); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: spaceID, ActorType: "user", ActorID: actorID,
			Action: "ai.result.confirmed", ResourceType: "ai_result", ResourceID: fmt.Sprintf("%d", resultID),
			Metadata: map[string]any{"media_id": res.MediaID, "task_type": res.TaskType},
		})
	}
	return nil
}

// RejectResult 驳回：删除非 manual 结果；若已 manual 则报错。
func (s *Service) RejectResult(ctx context.Context, spaceID string, resultID int64, actorID string) error {
	if !s.isEnabled() {
		return ErrAIDisabled
	}
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" || resultID <= 0 {
		return ErrInvalidInput
	}
	res, err := s.repo.GetResult(ctx, spaceID, resultID)
	if err != nil {
		return err
	}
	if res == nil {
		return ErrNotFound
	}
	if res.Manual {
		return ErrInvalidInput
	}
	if err := s.repo.DeleteResult(ctx, spaceID, resultID); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: spaceID, ActorType: "user", ActorID: actorID,
			Action: "ai.result.rejected", ResourceType: "ai_result", ResourceID: fmt.Sprintf("%d", resultID),
			Metadata: map[string]any{"media_id": res.MediaID, "task_type": res.TaskType},
		})
	}
	return nil
}

// DuplicateGroup AI 相似候选组（二切最小实现）。
type DuplicateGroup struct {
	MediaIDs []int64 `json:"media_ids"`
	Score    float64 `json:"score"`
	ModelID  string  `json:"model_id"`
}

// FindDuplicateCandidates 同 Space 内 embedding 余弦相似度 ≥ threshold 的无序对分组（简单并查）。
func (s *Service) FindDuplicateCandidates(ctx context.Context, spaceID string, threshold float64) ([]DuplicateGroup, error) {
	if err := s.EnsureReady(ctx); err != nil {
		return nil, err
	}
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return nil, ErrInvalidInput
	}
	if threshold <= 0 {
		threshold = 0.92
	}
	modelsList, err := s.repo.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	var model *models.AIModel
	for i := range modelsList {
		if modelsList[i].TaskType == models.AITaskTypeEmbedding && modelsList[i].Status == models.AIModelStatusAvailable {
			model = &modelsList[i]
			break
		}
	}
	if model == nil {
		return nil, ErrAIDisabled
	}
	rows, err := s.repo.ListEmbeddingsBySpaceModel(ctx, spaceID, model.ID)
	if err != nil {
		return nil, err
	}
	type item struct {
		id  int64
		vec []float32
	}
	items := make([]item, 0, len(rows))
	for _, r := range rows {
		items = append(items, item{id: r.MediaID, vec: DecodeVector(r.Vector)})
	}
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if parent[x] == 0 || parent[x] == x {
			parent[x] = x
			return x
		}
		parent[x] = find(parent[x])
		return parent[x]
	}
	union := func(a, b int64) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	bestScore := map[int64]float64{}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			sc := CosineSimilarity(items[i].vec, items[j].vec)
			if sc >= threshold {
				union(items[i].id, items[j].id)
				root := find(items[i].id)
				if sc > bestScore[root] {
					bestScore[root] = sc
				}
			}
		}
	}
	groups := map[int64][]int64{}
	for _, it := range items {
		if parent[it.id] == 0 {
			continue
		}
		r := find(it.id)
		groups[r] = append(groups[r], it.id)
	}
	out := make([]DuplicateGroup, 0)
	for root, ids := range groups {
		if len(ids) < 2 {
			continue
		}
		out = append(out, DuplicateGroup{MediaIDs: ids, Score: bestScore[root], ModelID: model.ID})
	}
	return out, nil
}

// BatchConfirmResults 批量确认（跳过已确认或失败项，返回成功计数）。
func (s *Service) BatchConfirmResults(ctx context.Context, spaceID string, ids []int64, actorID string) (int, error) {
	if !s.isEnabled() {
		return 0, ErrAIDisabled
	}
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" || len(ids) == 0 {
		return 0, ErrInvalidInput
	}
	count := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if err := s.ConfirmResult(ctx, spaceID, id, actorID); err == nil {
			count++
		}
	}
	return count, nil
}

// BatchRejectResults 批量驳回（跳过 manual 或失败项，返回成功计数）。
func (s *Service) BatchRejectResults(ctx context.Context, spaceID string, ids []int64, actorID string) (int, error) {
	if !s.isEnabled() {
		return 0, ErrAIDisabled
	}
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" || len(ids) == 0 {
		return 0, ErrInvalidInput
	}
	count := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if err := s.RejectResult(ctx, spaceID, id, actorID); err == nil {
			count++
		}
	}
	return count, nil
}
