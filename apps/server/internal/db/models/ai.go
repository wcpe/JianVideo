package models

import "time"

// AI 任务类型枚举（FR2-011 / ADR-0059）。
const (
	AITaskTypeFace               = "face"
	AITaskTypeOCR                = "ocr"
	AITaskTypeObjectScene        = "object_scene"
	AITaskTypeVideoUnderstanding = "video_understanding"
	AITaskTypeEmbedding          = "embedding"
)

// AI 模型状态。
const (
	AIModelStatusAvailable = "available"
	AIModelStatusDisabled  = "disabled"
)

// AIModel 可替换模型注册表行。
type AIModel struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	Name      string    `gorm:"not null;size:128" json:"name"`
	Version   string    `gorm:"not null;size:64" json:"version"`
	TaskType  string    `gorm:"not null;size:64;index" json:"task_type"`
	Status    string    `gorm:"not null;size:32;default:available" json:"status"`
	Endpoint  string    `gorm:"not null;default:''" json:"endpoint"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

// TableName 表名。
func (AIModel) TableName() string { return "ai_models" }

// AI 推理节点类型。
const (
	AINodeKindLocal      = "local"
	AINodeKindSelfHosted = "self_hosted"
)

// AIInferenceNode 推理节点登记。
type AIInferenceNode struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Name          string    `gorm:"not null;size:128" json:"name"`
	Kind          string    `gorm:"not null;size:32" json:"kind"`
	Endpoint      string    `gorm:"not null;default:''" json:"endpoint"`
	Enabled       bool      `gorm:"not null;default:false" json:"enabled"`
	TaskTypesJSON string    `gorm:"not null;default:'[]'" json:"task_types_json"`
	CreatedAt     time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt     time.Time `gorm:"not null" json:"updated_at"`
}

// TableName 表名。
func (AIInferenceNode) TableName() string { return "ai_inference_nodes" }

// AIResult AI 推理结果（可重建；manual 优先）。
type AIResult struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SpaceID      string    `gorm:"not null;size:128;index:idx_ai_results_space_media,priority:1;index:idx_ai_results_space_type,priority:1" json:"space_id"`
	MediaID      int64     `gorm:"not null;index:idx_ai_results_space_media,priority:2" json:"media_id"`
	TaskType     string    `gorm:"not null;size:64;index:idx_ai_results_space_type,priority:2" json:"task_type"`
	ModelID      string    `gorm:"not null;size:64" json:"model_id"`
	ModelVersion string    `gorm:"not null;size:64" json:"model_version"`
	NodeID       string    `gorm:"not null;size:64;default:''" json:"node_id"`
	BatchID      string    `gorm:"not null;size:64;default:'';index" json:"batch_id"`
	PayloadJSON  string    `gorm:"not null;default:'{}'" json:"payload_json"`
	Manual       bool      `gorm:"not null;default:false" json:"manual"`
	CreatedAt    time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt    time.Time `gorm:"not null" json:"updated_at"`
}

// TableName 表名。
func (AIResult) TableName() string { return "ai_results" }

// AIEmbedding 媒体向量（可重建，Space 隔离）。
type AIEmbedding struct {
	ID           int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	SpaceID      string `gorm:"not null;size:128;uniqueIndex:uidx_ai_emb_space_media_model,priority:1;index:idx_ai_emb_space_model,priority:1" json:"space_id"`
	MediaID      int64  `gorm:"not null;uniqueIndex:uidx_ai_emb_space_media_model,priority:2" json:"media_id"`
	ModelID      string `gorm:"not null;size:64;uniqueIndex:uidx_ai_emb_space_media_model,priority:3;index:idx_ai_emb_space_model,priority:2" json:"model_id"`
	ModelVersion string `gorm:"not null;size:64" json:"model_version"`
	Dim          int    `gorm:"not null" json:"dim"`
	BatchID      string `gorm:"not null;size:64;default:'';index" json:"batch_id"`
	// Vector 为 float32 小端序列化；隔离在数据层，不泄漏到业务 JSON。
	Vector    []byte    `gorm:"not null" json:"-"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

// TableName 表名。
func (AIEmbedding) TableName() string { return "ai_embeddings" }
