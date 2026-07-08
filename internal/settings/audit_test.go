package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
)

type failingAuditRecorder struct{}

func (f failingAuditRecorder) Record(context.Context, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (f failingAuditRecorder) RecordTx(context.Context, *gorm.DB, audit.EventInput) error {
	return errors.New("审计写入失败")
}

func (f failingAuditRecorder) List(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, nil
}

func TestSetMany_RecordsAuditEventInSameTransaction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := NewService(db).WithAudit(audit.NewRecorder(db))

	if err := svc.SetMany(map[string]string{KeyScanInterval: "600"}); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	var event models.AuditEvent
	if err := db.First(&event, "action = ?", "settings.updated").Error; err != nil {
		t.Fatalf("应写入 settings.updated 审计事件: %v", err)
	}
	if event.Scope != audit.ScopeSystem || event.SpaceID != nil {
		t.Fatalf("设置变更应记录为系统级事件, scope=%q space=%v", event.Scope, event.SpaceID)
	}
}

func TestSetMany_RedactsSensitiveAuditPayload(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}, &models.AuditEvent{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := NewService(db).WithAudit(audit.NewRecorder(db))

	secretProxy := "http://user:secret@example.com:8080"
	if err := svc.SetMany(map[string]string{KeyNetworkProxy: secretProxy}); err != nil {
		t.Fatalf("保存代理失败: %v", err)
	}

	var event models.AuditEvent
	if err := db.First(&event, "action = ?", "settings.updated").Error; err != nil {
		t.Fatalf("应写入 settings.updated 审计事件: %v", err)
	}
	payload, err := json.Marshal(map[string]string{
		"before": event.BeforeJSON,
		"after":  event.AfterJSON,
	})
	if err != nil {
		t.Fatalf("序列化审计载荷失败: %v", err)
	}
	if strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "user:") {
		t.Fatalf("审计载荷不应包含代理凭据: %s", payload)
	}
	if !strings.Contains(string(payload), sensitiveDisplayValue) {
		t.Fatalf("审计载荷应只记录敏感值存在性: %s", payload)
	}
}

func TestSetMany_RollsBackWhenAuditFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := NewService(db).WithAudit(failingAuditRecorder{})

	err = svc.SetMany(map[string]string{KeyScanInterval: "600"})
	if err == nil {
		t.Fatal("审计失败时设置保存应失败")
	}

	got, err := svc.Get(KeyScanInterval)
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got != "" {
		t.Fatalf("审计失败时业务变更应回滚, 实际 scan_interval=%q", got)
	}
}
