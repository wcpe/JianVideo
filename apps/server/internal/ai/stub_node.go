package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// StubNode 可测的本地 stub 推理节点。
type StubNode struct {
	id string
}

// NewStubNode 创建 stub 节点。
func NewStubNode(id string) *StubNode {
	if id == "" {
		id = "stub-local"
	}
	return &StubNode{id: id}
}

// ID 节点标识。
func (n *StubNode) ID() string { return n.id }

// Infer 返回确定性 stub 结果；embedding 任务附带向量。
func (n *StubNode) Infer(_ context.Context, req InferRequest) (InferResponse, error) {
	text := fmt.Sprintf("stub-%s-%d", req.TaskType, req.MediaID)
	// 允许 Input 覆盖文本，便于搜索测「必中」
	if req.Input != nil {
		if t, ok := req.Input["text"].(string); ok && t != "" {
			text = t
		}
	}
	payload, err := json.Marshal(map[string]any{
		"stub": true, "task_type": req.TaskType, "media_id": req.MediaID,
		"model_id": req.Model.ID, "version": req.Model.Version, "text": text,
	})
	if err != nil {
		return InferResponse{}, err
	}
	resp := InferResponse{PayloadJSON: string(payload)}
	if req.TaskType == models.AITaskTypeEmbedding {
		resp.Embedding = StubEmbedText(text, 8)
	}
	return resp, nil
}

// Ensure StubNode 实现 Node。
var _ Node = (*StubNode)(nil)

// ValidTaskType 是否合法 task_type。
func ValidTaskType(t string) bool {
	switch t {
	case models.AITaskTypeFace, models.AITaskTypeOCR, models.AITaskTypeObjectScene,
		models.AITaskTypeVideoUnderstanding, models.AITaskTypeEmbedding:
		return true
	default:
		return false
	}
}
