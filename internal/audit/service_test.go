package audit

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func newAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditEvent{}); err != nil {
		t.Fatalf("迁移审计表失败: %v", err)
	}
	return db
}

func TestRecorderRecord_ValidatesScopeAndRedactsPayload(t *testing.T) {
	db := newAuditTestDB(t)
	rec := NewRecorder(db)

	err := rec.Record(context.Background(), EventInput{
		Scope:        ScopeSpace,
		SpaceID:      models.DefaultSpaceID,
		ActorType:    ActorSystem,
		Action:       "settings.updated",
		ResourceType: "settings",
		ResourceID:   "scan_interval",
		After:        map[string]any{"password": "secret", "scan_interval": "60"},
	})
	if err != nil {
		t.Fatalf("写入审计事件失败: %v", err)
	}

	var event models.AuditEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("读取审计事件失败: %v", err)
	}
	if event.Action != "settings.updated" || event.EventType != event.Action {
		t.Fatalf("动作字段未正确写入: action=%q event_type=%q", event.Action, event.EventType)
	}
	if event.AfterJSON == "" || event.AfterJSON == `{"password":"secret","scan_interval":"60"}` {
		t.Fatalf("after_json 应脱敏后写入, 实际 %q", event.AfterJSON)
	}
}

func TestRecorderRecord_RejectsInvalidScope(t *testing.T) {
	db := newAuditTestDB(t)
	rec := NewRecorder(db)

	err := rec.Record(context.Background(), EventInput{
		Scope:        ScopeSpace,
		ActorType:    ActorSystem,
		Action:       "settings.updated",
		ResourceType: "settings",
	})
	if err == nil {
		t.Fatal("space 作用域缺少 space_id 应返回错误")
	}

	err = rec.Record(context.Background(), EventInput{
		Scope:        ScopeSystem,
		SpaceID:      models.DefaultSpaceID,
		ActorType:    ActorSystem,
		Action:       "migration.started",
		ResourceType: "migration",
	})
	if err == nil {
		t.Fatal("system 作用域带 space_id 应返回错误")
	}
}

func TestRecorderList_FiltersSpaceAndCursor(t *testing.T) {
	db := newAuditTestDB(t)
	rec := NewRecorder(db)
	base := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	rec.SetNowForTest(func() time.Time { return base.Add(2 * time.Second) })
	_ = rec.Record(context.Background(), EventInput{Scope: ScopeSpace, SpaceID: "space-a", ActorType: ActorSystem, Action: "media.deleted", ResourceType: "media", ResourceID: "1"})
	rec.SetNowForTest(func() time.Time { return base.Add(time.Second) })
	_ = rec.Record(context.Background(), EventInput{Scope: ScopeSpace, SpaceID: "space-a", ActorType: ActorSystem, Action: "library.created", ResourceType: "library", ResourceID: "1"})
	rec.SetNowForTest(func() time.Time { return base })
	_ = rec.Record(context.Background(), EventInput{Scope: ScopeSpace, SpaceID: "space-b", ActorType: ActorSystem, Action: "media.deleted", ResourceType: "media", ResourceID: "2"})

	first, err := rec.List(context.Background(), Query{SpaceID: "space-a", Limit: 1})
	if err != nil {
		t.Fatalf("第一页查询失败: %v", err)
	}
	if len(first.Items) != 1 || first.Items[0].Action != "media.deleted" {
		t.Fatalf("第一页应返回 space-a 最新事件, 实际 %+v", first.Items)
	}
	if first.NextCursor == "" {
		t.Fatal("第一页应返回 next_cursor")
	}

	second, err := rec.List(context.Background(), Query{SpaceID: "space-a", Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("第二页查询失败: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].Action != "library.created" {
		t.Fatalf("第二页应返回 space-a 下一条事件, 实际 %+v", second.Items)
	}
}
