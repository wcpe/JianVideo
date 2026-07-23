package audit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	// ScopeSpace 表示 Space 资源相关事件。
	ScopeSpace = "space"
	// ScopeSystem 表示全局系统级事件。
	ScopeSystem = "system"
	// ActorSystem 表示系统或单用户阶段默认操作者。
	ActorSystem = "system"
)

const defaultLimit = 50
const maxLimit = 200

// Recorder 是审计事件写入与查询接口。
type Recorder interface {
	Record(context.Context, EventInput) error
	RecordTx(context.Context, *gorm.DB, EventInput) error
	List(context.Context, Query) (Page, error)
	// GetByID 按主键取事件；不存在返回 gorm.ErrRecordNotFound。
	GetByID(context.Context, int64) (*models.AuditEvent, error)
}

// EventInput 表示待写入的审计事件。
type EventInput struct {
	Scope        string
	SpaceID      string
	ActorType    string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Before       any
	After        any
	Metadata     any
	RequestID    string
}

// Query 表示审计事件查询条件。
type Query struct {
	SpaceID      string
	System       bool
	Action       string
	ResourceType string
	ResourceID   string
	From         time.Time
	To           time.Time
	Cursor       string
	Limit        int
}

// Page 表示审计事件分页结果。
type Page struct {
	Items      []models.AuditEvent `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// Service 实现审计事件写入与查询。
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// NewRecorder 创建审计服务。
func NewRecorder(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// SetNowForTest 覆盖时间来源，仅供测试。
func (s *Service) SetNowForTest(now func() time.Time) {
	s.now = now
}

// Record 写入审计事件。
func (s *Service) Record(ctx context.Context, input EventInput) error {
	return s.RecordTx(ctx, s.db, input)
}

// RecordTx 在指定事务内写入审计事件。
func (s *Service) RecordTx(ctx context.Context, tx *gorm.DB, input EventInput) error {
	event, err := s.buildEvent(input)
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(event).Error
}

// GetByID 按主键取审计事件。
func (s *Service) GetByID(ctx context.Context, id int64) (*models.AuditEvent, error) {
	if id <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var event models.AuditEvent
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// List 查询审计事件，按 created_at desc, id desc 返回。
func (s *Service) List(ctx context.Context, query Query) (Page, error) {
	limit := normalizeLimit(query.Limit)
	db := s.db.WithContext(ctx).Model(&models.AuditEvent{})
	if query.System {
		db = db.Where("scope = ?", ScopeSystem)
	} else {
		spaceID := strings.TrimSpace(query.SpaceID)
		if spaceID == "" {
			spaceID = models.DefaultSpaceID
		}
		db = db.Where("scope = ? AND space_id = ?", ScopeSpace, spaceID)
	}
	if query.Action != "" {
		db = db.Where("action = ?", query.Action)
	}
	if query.ResourceType != "" {
		db = db.Where("resource_type = ?", query.ResourceType)
	}
	if query.ResourceID != "" {
		db = db.Where("resource_id = ?", query.ResourceID)
	}
	if !query.From.IsZero() {
		db = db.Where("created_at >= ?", query.From)
	}
	if !query.To.IsZero() {
		db = db.Where("created_at <= ?", query.To)
	}
	if query.Cursor != "" {
		cursor, err := decodeCursor(query.Cursor)
		if err != nil {
			return Page{}, err
		}
		db = db.Where("(created_at < ?) OR (created_at = ? AND id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}

	var events []models.AuditEvent
	if err := db.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&events).Error; err != nil {
		return Page{}, err
	}
	page := Page{Items: events}
	if len(events) > limit {
		page.Items = events[:limit]
		last := page.Items[len(page.Items)-1]
		next, err := encodeCursor(auditCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}

func (s *Service) buildEvent(input EventInput) (*models.AuditEvent, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	action := strings.TrimSpace(input.Action)
	actorType := strings.TrimSpace(input.ActorType)
	if actorType == "" {
		actorType = ActorSystem
	}
	var spaceID *string
	if value := strings.TrimSpace(input.SpaceID); value != "" {
		spaceID = &value
	}
	return &models.AuditEvent{
		Scope:        strings.TrimSpace(input.Scope),
		SpaceID:      spaceID,
		ActorType:    actorType,
		ActorID:      strings.TrimSpace(input.ActorID),
		Action:       action,
		EventType:    action,
		ResourceType: strings.TrimSpace(input.ResourceType),
		ResourceID:   strings.TrimSpace(input.ResourceID),
		BeforeJSON:   mustRedactedJSON(input.Before),
		AfterJSON:    mustRedactedJSON(input.After),
		MetadataJSON: mustRedactedJSON(input.Metadata),
		RequestID:    strings.TrimSpace(input.RequestID),
		CreatedAt:    s.now().UTC(),
	}, nil
}

func validateInput(input EventInput) error {
	scope := strings.TrimSpace(input.Scope)
	if scope != ScopeSpace && scope != ScopeSystem {
		return fmt.Errorf("审计 scope 无效: %s", input.Scope)
	}
	spaceID := strings.TrimSpace(input.SpaceID)
	if scope == ScopeSpace && spaceID == "" {
		return errors.New("space 审计事件必须包含 space_id")
	}
	if scope == ScopeSystem && spaceID != "" {
		return errors.New("system 审计事件不能包含 space_id")
	}
	if strings.TrimSpace(input.Action) == "" {
		return errors.New("审计 action 不能为空")
	}
	if strings.TrimSpace(input.ResourceType) == "" {
		return errors.New("审计 resource_type 不能为空")
	}
	return nil
}

func mustRedactedJSON(value any) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	redacted, err := RedactJSON(raw)
	if err != nil {
		return ""
	}
	return string(redacted)
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

type auditCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

func encodeCursor(cursor auditCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(token string) (auditCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return auditCursor{}, fmt.Errorf("审计游标无效: %w", err)
	}
	var cursor auditCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return auditCursor{}, fmt.Errorf("审计游标无效: %w", err)
	}
	if cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return auditCursor{}, fmt.Errorf("审计游标无效: %s", strconv.Quote(token))
	}
	return cursor, nil
}
