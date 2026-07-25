package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// 任务与错误。
const (
	// TaskTypeAIInfer 统一 AI 推理任务类型（payload 带 task_type）。
	TaskTypeAIInfer = "ai.infer"
)

var (
	// ErrAIDisabled AI 总开关关闭或无可用模型/节点。
	ErrAIDisabled = errors.New("AI 能力未启用")
	// ErrInvalidInput 请求参数非法。
	ErrInvalidInput = errors.New("AI 请求参数非法")
	// ErrNotFound 资源不存在。
	ErrNotFound = errors.New("AI 资源不存在")
)

// InferRequest 节点推理请求。
type InferRequest struct {
	SpaceID  string
	MediaID  int64
	TaskType string
	Model    models.AIModel
	// Input 可选附加输入（首切 stub 可忽略）。
	Input map[string]any
}

// InferResponse 节点推理响应。
type InferResponse struct {
	PayloadJSON string
	// Embedding 可选；task_type=embedding 时写入向量索引。
	Embedding []float32
}

// Node 可替换推理节点接口（ADR-0059）。
type Node interface {
	ID() string
	Infer(ctx context.Context, req InferRequest) (InferResponse, error)
}

// Repository 数据访问。
type Repository interface {
	ListModels(ctx context.Context) ([]models.AIModel, error)
	GetModel(ctx context.Context, id string) (*models.AIModel, error)
	UpsertModel(ctx context.Context, m *models.AIModel) error
	ListNodes(ctx context.Context) ([]models.AIInferenceNode, error)
	GetNode(ctx context.Context, id string) (*models.AIInferenceNode, error)
	UpsertNode(ctx context.Context, n *models.AIInferenceNode) error
	ListResultsByMedia(ctx context.Context, spaceID string, mediaID int64) ([]models.AIResult, error)
	ListResultsBySpace(ctx context.Context, spaceID string, taskType string, manual *bool) ([]models.AIResult, error)
	CreateResult(ctx context.Context, r *models.AIResult) error
	DeleteNonManualResults(ctx context.Context, spaceID string, mediaID int64, taskType, batchID string) (int64, error)
	UpsertEmbedding(ctx context.Context, e *models.AIEmbedding) error
	ListEmbeddingsBySpaceModel(ctx context.Context, spaceID, modelID string) ([]models.AIEmbedding, error)
	DeleteEmbeddings(ctx context.Context, spaceID string, mediaID int64, modelID, batchID string) (int64, error)
	GetResult(ctx context.Context, spaceID string, id int64) (*models.AIResult, error)
	UpdateResult(ctx context.Context, r *models.AIResult) error
	DeleteResult(ctx context.Context, spaceID string, id int64) error
}

// Service AI 管线服务。
type Service struct {
	repo     Repository
	settings *settings.Service
	tasks    *tasksvc.Service
	audit    audit.Recorder
	nodes    map[string]Node
}

// NewService 创建服务。
func NewService(repo Repository, settingsSvc *settings.Service) *Service {
	return &Service{
		repo:     repo,
		settings: settingsSvc,
		nodes:    map[string]Node{},
	}
}

// WithTasks 注入通用任务服务。
func (s *Service) WithTasks(tasks *tasksvc.Service) *Service {
	s.tasks = tasks
	return s
}

// WithAudit 注入审计。
func (s *Service) WithAudit(rec audit.Recorder) *Service {
	s.audit = rec
	return s
}

// RegisterNode 注册内存中的可执行节点实现。
func (s *Service) RegisterNode(n Node) {
	if n == nil {
		return
	}
	s.nodes[n.ID()] = n
}

// Status 返回 AI 能力状态摘要。
func (s *Service) Status(ctx context.Context) (enabled bool, modelsList []models.AIModel, nodes []models.AIInferenceNode, err error) {
	enabled = s.isEnabled()
	modelsList, err = s.repo.ListModels(ctx)
	if err != nil {
		return false, nil, nil, err
	}
	nodes, err = s.repo.ListNodes(ctx)
	if err != nil {
		return false, nil, nil, err
	}
	return enabled, modelsList, nodes, nil
}

// SetModelStatus 设置模型 available/disabled（FR2-011 设置页启用开关）。
func (s *Service) SetModelStatus(ctx context.Context, modelID, status, actorID string) error {
	modelID = strings.TrimSpace(modelID)
	status = strings.TrimSpace(status)
	if modelID == "" {
		return ErrInvalidInput
	}
	if status != models.AIModelStatusAvailable && status != models.AIModelStatusDisabled {
		return ErrInvalidInput
	}
	m, err := s.repo.GetModel(ctx, modelID)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrNotFound
	}
	if m.Status == status {
		return nil
	}
	before := m.Status
	m.Status = status
	m.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpsertModel(ctx, m); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSystem, ActorType: "user", ActorID: actorID,
			Action: "ai.model.status_updated", ResourceType: "ai_model", ResourceID: modelID,
			Metadata: map[string]any{"before": before, "after": status, "task_type": m.TaskType},
		})
	}
	return nil
}

// SetNodeEnabled 设置推理节点启用状态。
func (s *Service) SetNodeEnabled(ctx context.Context, nodeID string, enabled bool, actorID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidInput
	}
	n, err := s.repo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if n == nil {
		return ErrNotFound
	}
	if n.Enabled == enabled {
		return nil
	}
	before := n.Enabled
	n.Enabled = enabled
	n.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpsertNode(ctx, n); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSystem, ActorType: "user", ActorID: actorID,
			Action: "ai.node.enabled_updated", ResourceType: "ai_node", ResourceID: nodeID,
			Metadata: map[string]any{"before": before, "after": enabled, "kind": n.Kind},
		})
	}
	return nil
}

// EnsureReady 校验 AI 已启用且存在可用模型与节点。
func (s *Service) EnsureReady(ctx context.Context) error {
	if !s.isEnabled() {
		return ErrAIDisabled
	}
	modelsList, err := s.repo.ListModels(ctx)
	if err != nil {
		return err
	}
	nodes, err := s.repo.ListNodes(ctx)
	if err != nil {
		return err
	}
	hasModel, hasNode := false, false
	for _, m := range modelsList {
		if m.Status == models.AIModelStatusAvailable {
			hasModel = true
			break
		}
	}
	for _, n := range nodes {
		if n.Enabled {
			hasNode = true
			break
		}
	}
	if !hasModel || !hasNode {
		return ErrAIDisabled
	}
	return nil
}

func (s *Service) isEnabled() bool {
	if s.settings == nil {
		return false
	}
	raw, err := s.settings.Get(settings.KeyAIEnabled)
	if err != nil {
		return false
	}
	return settings.ParseBoolSetting(raw, false)
}

// InferPayload ai.infer 任务负载。
type InferPayload struct {
	SpaceID  string `json:"space_id"`
	MediaID  int64  `json:"media_id"`
	TaskType string `json:"task_type"`
	ModelID  string `json:"model_id,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
	BatchID  string `json:"batch_id,omitempty"`
}

// EnqueueInfer 入队 AI 推理任务。
func (s *Service) EnqueueInfer(ctx context.Context, spaceID string, mediaID int64, taskType, modelID, nodeID, actorID string) (*models.Task, error) {
	if s.tasks == nil {
		return nil, errors.New("任务服务未启用")
	}
	if err := s.EnsureReady(ctx); err != nil {
		return nil, err
	}
	spaceID = strings.TrimSpace(spaceID)
	taskType = strings.TrimSpace(taskType)
	if spaceID == "" || mediaID <= 0 || taskType == "" {
		return nil, ErrInvalidInput
	}
	model, node, err := s.resolveModelAndNode(ctx, taskType, modelID, nodeID)
	if err != nil {
		return nil, err
	}
	batchID := fmt.Sprintf("b-%d", time.Now().UnixNano())
	payload := InferPayload{
		SpaceID: spaceID, MediaID: mediaID, TaskType: taskType,
		ModelID: model.ID, NodeID: node.ID, BatchID: batchID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("ai-infer:%s:%d:%s:%s", spaceID, mediaID, taskType, model.ID)
	task, err := s.tasks.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeAIInfer,
		Priority: 0, MaxAttempts: 2, IdempotencyKey: key, PayloadJSON: string(encoded),
		ResourceType: "media", ResourceID: fmt.Sprintf("%d", mediaID),
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: spaceID, ActorType: "user", ActorID: actorID,
			Action: "ai.infer.enqueued", ResourceType: "media", ResourceID: fmt.Sprintf("%d", mediaID),
			Metadata: map[string]any{"task_id": task.ID, "task_type": taskType, "model_id": model.ID},
		})
	}
	return task, nil
}

func (s *Service) resolveModelAndNode(ctx context.Context, taskType, modelID, nodeID string) (*models.AIModel, *models.AIInferenceNode, error) {
	var model *models.AIModel
	var err error
	if strings.TrimSpace(modelID) != "" {
		model, err = s.repo.GetModel(ctx, modelID)
		if err != nil {
			return nil, nil, err
		}
		if model == nil || model.Status != models.AIModelStatusAvailable {
			return nil, nil, ErrNotFound
		}
	} else {
		all, err := s.repo.ListModels(ctx)
		if err != nil {
			return nil, nil, err
		}
		for i := range all {
			if all[i].TaskType == taskType && all[i].Status == models.AIModelStatusAvailable {
				model = &all[i]
				break
			}
		}
		if model == nil {
			return nil, nil, ErrAIDisabled
		}
	}
	if model.TaskType != taskType {
		return nil, nil, ErrInvalidInput
	}
	var node *models.AIInferenceNode
	if strings.TrimSpace(nodeID) != "" {
		node, err = s.repo.GetNode(ctx, nodeID)
		if err != nil {
			return nil, nil, err
		}
		if node == nil || !node.Enabled {
			return nil, nil, ErrNotFound
		}
	} else {
		all, err := s.repo.ListNodes(ctx)
		if err != nil {
			return nil, nil, err
		}
		for i := range all {
			if all[i].Enabled && nodeSupportsTask(all[i], taskType) {
				node = &all[i]
				break
			}
		}
		if node == nil {
			return nil, nil, ErrAIDisabled
		}
	}
	return model, node, nil
}

func nodeSupportsTask(n models.AIInferenceNode, taskType string) bool {
	var types []string
	if err := json.Unmarshal([]byte(n.TaskTypesJSON), &types); err != nil {
		return false
	}
	for _, t := range types {
		if t == taskType {
			return true
		}
	}
	return false
}

// ListResults 按 media 列结果（Space 隔离）。
func (s *Service) ListResults(ctx context.Context, spaceID string, mediaID int64) ([]models.AIResult, error) {
	if strings.TrimSpace(spaceID) == "" || mediaID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repo.ListResultsByMedia(ctx, spaceID, mediaID)
}

// ListResultsBySpace 按 Space 查全部结果，支持可选过滤（审核列表页）。
func (s *Service) ListResultsBySpace(ctx context.Context, spaceID, taskType string, manual *bool) ([]models.AIResult, error) {
	if strings.TrimSpace(spaceID) == "" {
		return nil, ErrInvalidInput
	}
	return s.repo.ListResultsBySpace(ctx, spaceID, taskType, manual)
}

// RebuildResults 删除非 manual 结果（可重建）。
func (s *Service) RebuildResults(ctx context.Context, spaceID string, mediaID int64, taskType, batchID, actorID string) (int64, error) {
	if !s.isEnabled() {
		return 0, ErrAIDisabled
	}
	if strings.TrimSpace(spaceID) == "" {
		return 0, ErrInvalidInput
	}
	n, err := s.repo.DeleteNonManualResults(ctx, spaceID, mediaID, taskType, batchID)
	if err != nil {
		return 0, err
	}
	// 同步删向量（embedding 可重建）
	if taskType == "" || taskType == models.AITaskTypeEmbedding {
		if _, err := s.repo.DeleteEmbeddings(ctx, spaceID, mediaID, "", batchID); err != nil {
			return n, err
		}
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: spaceID, ActorType: "user", ActorID: actorID,
			Action: "ai.results.rebuilt", ResourceType: "media", ResourceID: fmt.Sprintf("%d", mediaID),
			Metadata: map[string]any{"deleted": n, "task_type": taskType, "batch_id": batchID},
		})
	}
	return n, nil
}

// SearchHit 语义搜索命中。
type SearchHit struct {
	MediaID int64   `json:"media_id"`
	Score   float64 `json:"score"`
	ModelID string  `json:"model_id"`
}

// SemanticSearch 文本查询 → stub 嵌入 → 余弦 topK（Space 隔离）。
func (s *Service) SemanticSearch(ctx context.Context, spaceID, query string, topK int) ([]SearchHit, error) {
	if err := s.EnsureReady(ctx); err != nil {
		return nil, err
	}
	spaceID = strings.TrimSpace(spaceID)
	query = strings.TrimSpace(query)
	if spaceID == "" || query == "" {
		return nil, ErrInvalidInput
	}
	if topK <= 0 {
		topK = 10
	}
	if topK > 100 {
		topK = 100
	}
	// 选用可用 embedding 模型
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
	qVec := StubEmbedText(query, 8)
	rows, err := s.repo.ListEmbeddingsBySpaceModel(ctx, spaceID, model.ID)
	if err != nil {
		return nil, err
	}
	type scored struct {
		mediaID int64
		score   float64
	}
	var hits []scored
	for _, row := range rows {
		vec := DecodeVector(row.Vector)
		score := CosineSimilarity(qVec, vec)
		if score <= 0 {
			continue
		}
		hits = append(hits, scored{mediaID: row.MediaID, score: score})
	}
	// 简单选择排序取 topK
	for i := 0; i < len(hits); i++ {
		best := i
		for j := i + 1; j < len(hits); j++ {
			if hits[j].score > hits[best].score {
				best = j
			}
		}
		hits[i], hits[best] = hits[best], hits[i]
	}
	if len(hits) > topK {
		hits = hits[:topK]
	}
	out := make([]SearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, SearchHit{MediaID: h.mediaID, Score: h.score, ModelID: model.ID})
	}
	return out, nil
}

// HandleInferTask worker 入口。
func (s *Service) HandleInferTask(ctx context.Context, task models.Task) error {
	// 误唤醒时 no-op
	if !s.isEnabled() {
		return nil
	}
	var payload InferPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return err
	}
	model, err := s.repo.GetModel(ctx, payload.ModelID)
	if err != nil {
		return err
	}
	if model == nil {
		return ErrNotFound
	}
	nodeImpl, ok := s.nodes[payload.NodeID]
	if !ok {
		// 无内存实现时写失败结果摘要
		return fmt.Errorf("推理节点未注册: %s", payload.NodeID)
	}
	resp, err := nodeImpl.Infer(ctx, InferRequest{
		SpaceID: payload.SpaceID, MediaID: payload.MediaID, TaskType: payload.TaskType, Model: *model,
		// embedding 用稳定文本：space+media，便于搜索构造
		Input: map[string]any{"text": fmt.Sprintf("%s:%d", payload.SpaceID, payload.MediaID)},
	})
	if err != nil {
		if s.audit != nil {
			_ = s.audit.Record(ctx, audit.EventInput{
				Scope: audit.ScopeSpace, SpaceID: payload.SpaceID, ActorType: audit.ActorSystem, ActorID: "ai-worker",
				Action: "ai.infer.failed", ResourceType: "media", ResourceID: fmt.Sprintf("%d", payload.MediaID),
				Metadata: map[string]any{"error": err.Error(), "task_id": task.ID},
			})
		}
		return err
	}
	now := time.Now().UTC()
	result := &models.AIResult{
		SpaceID: payload.SpaceID, MediaID: payload.MediaID, TaskType: payload.TaskType,
		ModelID: model.ID, ModelVersion: model.Version, NodeID: payload.NodeID, BatchID: payload.BatchID,
		PayloadJSON: resp.PayloadJSON, Manual: false, CreatedAt: now, UpdatedAt: now,
	}
	if result.PayloadJSON == "" {
		result.PayloadJSON = "{}"
	}
	if err := s.repo.CreateResult(ctx, result); err != nil {
		return err
	}
	// embedding 任务同步写入向量索引
	if payload.TaskType == models.AITaskTypeEmbedding && len(resp.Embedding) > 0 {
		emb := &models.AIEmbedding{
			SpaceID: payload.SpaceID, MediaID: payload.MediaID,
			ModelID: model.ID, ModelVersion: model.Version, Dim: len(resp.Embedding),
			BatchID: payload.BatchID, Vector: EncodeVector(resp.Embedding),
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.UpsertEmbedding(ctx, emb); err != nil {
			return err
		}
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: payload.SpaceID, ActorType: audit.ActorSystem, ActorID: "ai-worker",
			Action: "ai.infer.succeeded", ResourceType: "media", ResourceID: fmt.Sprintf("%d", payload.MediaID),
			Metadata: map[string]any{"result_id": result.ID, "task_id": task.ID},
		})
	}
	return nil
}

// RegisterAIWorker 注册 ai.infer worker。
func RegisterAIWorker(registry *tasksvc.WorkerRegistry, svc *Service) error {
	if registry == nil || svc == nil {
		return errors.New("AI worker 注册参数无效")
	}
	return registry.Register(TaskTypeAIInfer, tasksvc.DefaultConcurrency(TaskTypeAIInfer), svc.HandleInferTask)
}

// SeedStubFixture 写入全部 stub 模型与节点（测试/开发用）。
func (s *Service) SeedStubFixture(ctx context.Context) error {
	now := time.Now().UTC()
	for _, m := range []models.AIModel{
		{ID: "stub-ocr-v1", Name: "Stub OCR", Version: "1.0.0", TaskType: models.AITaskTypeOCR, Status: models.AIModelStatusAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "stub-embed-v1", Name: "Stub Embedding", Version: "1.0.0", TaskType: models.AITaskTypeEmbedding, Status: models.AIModelStatusAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "stub-object-scene-v1", Name: "Stub Object/Scene", Version: "1.0.0", TaskType: models.AITaskTypeObjectScene, Status: models.AIModelStatusAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "stub-face-v1", Name: "Stub Face Detection", Version: "1.0.0", TaskType: models.AITaskTypeFace, Status: models.AIModelStatusAvailable, CreatedAt: now, UpdatedAt: now},
		{ID: "stub-video-understanding-v1", Name: "Stub Video Understanding", Version: "1.0.0", TaskType: models.AITaskTypeVideoUnderstanding, Status: models.AIModelStatusAvailable, CreatedAt: now, UpdatedAt: now},
	} {
		mm := m
		if err := s.repo.UpsertModel(ctx, &mm); err != nil {
			return err
		}
	}
	typesJSON, _ := json.Marshal([]string{
		models.AITaskTypeOCR, models.AITaskTypeEmbedding,
		models.AITaskTypeObjectScene, models.AITaskTypeFace, models.AITaskTypeVideoUnderstanding,
	})
	node := &models.AIInferenceNode{
		ID: "stub-local", Name: "Stub Local Node", Kind: models.AINodeKindLocal,
		Endpoint: "", Enabled: true, TaskTypesJSON: string(typesJSON), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.UpsertNode(ctx, node); err != nil {
		return err
	}
	s.RegisterNode(NewStubNode("stub-local"))
	return nil
}
