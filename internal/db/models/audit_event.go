package models

import "time"

// AuditEvent 审计事件真源记录（FR2-040）。
type AuditEvent struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Scope        string    `gorm:"not null;index:idx_audit_events_scope_space_created,priority:1" json:"scope"`
	SpaceID      *string   `gorm:"index:idx_audit_events_scope_space_created,priority:2" json:"space_id"`
	ActorType    string    `gorm:"not null;default:system" json:"actor_type"`
	ActorID      string    `json:"actor_id,omitempty"`
	Action       string    `gorm:"not null;index:idx_audit_events_action_created,priority:1" json:"action"`
	EventType    string    `gorm:"not null" json:"-"`
	ResourceType string    `gorm:"not null;index:idx_audit_events_resource_created,priority:1" json:"resource_type"`
	ResourceID   string    `gorm:"index:idx_audit_events_resource_created,priority:2" json:"resource_id,omitempty"`
	BeforeJSON   string    `json:"before_json,omitempty"`
	AfterJSON    string    `json:"after_json,omitempty"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	RequestID    string    `json:"request_id,omitempty"`
	CreatedAt    time.Time `gorm:"not null;index:idx_audit_events_scope_space_created,priority:3;index:idx_audit_events_action_created,priority:2;index:idx_audit_events_resource_created,priority:3" json:"created_at"`
}
