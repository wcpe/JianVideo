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

// Infer 返回确定性 stub 结果；按 task_type 输出结构化 payload，embedding 附带向量。
func (n *StubNode) Infer(_ context.Context, req InferRequest) (InferResponse, error) {
	text := fmt.Sprintf("stub-%s-%d", req.TaskType, req.MediaID)
	// 允许 Input 覆盖文本，便于搜索测「必中」
	if req.Input != nil {
		if t, ok := req.Input["text"].(string); ok && t != "" {
			text = t
		}
	}
	payload := n.buildPayload(req, text)
	data, err := json.Marshal(payload)
	if err != nil {
		return InferResponse{}, err
	}
	resp := InferResponse{PayloadJSON: string(data)}
	if req.TaskType == models.AITaskTypeEmbedding {
		resp.Embedding = StubEmbedText(text, 8)
	}
	return resp, nil
}

// buildPayload 按 task_type 生成结构化 stub payload。
func (n *StubNode) buildPayload(req InferRequest, text string) map[string]any {
	base := map[string]any{
		"stub": true, "task_type": req.TaskType, "media_id": req.MediaID,
		"model_id": req.Model.ID, "version": req.Model.Version,
	}
	switch req.TaskType {
	case models.AITaskTypeObjectScene:
		// 确定性 stub：根据 mediaID 生成标签与置信度
		idx := int(req.MediaID % 3)
		labels := [][]string{{"猫", "室内"}, {"狗", "户外"}, {"天空", "风景"}}
		scenes := []string{"日常生活", "自然风光", "城市建筑"}
		objects := make([]map[string]any, 0, 2)
		for i, label := range labels[idx] {
			objects = append(objects, map[string]any{
				"label": label, "confidence": 0.85 + float64(i)*0.05,
			})
		}
		base["objects"] = objects
		base["scene"] = scenes[idx]
	case models.AITaskTypeFace:
		// face detection-only stub：只返回人脸框与置信度，不做身份库
		count := int(req.MediaID%2) + 1
		faces := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			faces = append(faces, map[string]any{
				"bbox":       []float64{0.1 + float64(i)*0.3, 0.2, 0.25, 0.35},
				"confidence": 0.9 - float64(i)*0.1,
			})
		}
		base["faces"] = faces
	case models.AITaskTypeVideoUnderstanding:
		// 视频理解 stub：返回摘要与分段描述（不抽帧）
		base["summary"] = fmt.Sprintf("视频 #%d 的 stub 摘要", req.MediaID)
		base["segments"] = []map[string]any{
			{"start": 0, "end": 10, "description": "片头 stub"},
			{"start": 10, "end": 60, "description": "主体 stub"},
		}
	default:
		// OCR / embedding / 未知 → 兼容旧格式
		base["text"] = text
	}
	return base
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
