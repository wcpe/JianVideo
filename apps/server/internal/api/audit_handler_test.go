package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

func setupAuditRouter(t *testing.T) (*gin.Engine, audit.Recorder, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditEvent{}, &models.Space{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if err := db.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("创建默认 Space 失败: %v", err)
	}
	rec := audit.NewRecorder(db)
	h := NewHandler(library.NewService(db)).WithAudit(rec)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, rec, db
}

func TestListAuditEvents_SpaceIsolation(t *testing.T) {
	router, rec, _ := setupAuditRouter(t)
	_ = rec.Record(context.Background(), audit.EventInput{Scope: audit.ScopeSpace, SpaceID: models.DefaultSpaceID, ActorType: audit.ActorSystem, Action: "media.deleted", ResourceType: "media", ResourceID: "1"})
	_ = rec.Record(context.Background(), audit.EventInput{Scope: audit.ScopeSystem, ActorType: audit.ActorSystem, Action: "migration.started", ResourceType: "migration"})

	req := httptest.NewRequest("GET", "/api/audit/events", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("查询审计事件期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "media.deleted") {
		t.Fatalf("应返回 Space 事件, body=%s", body)
	}
	if strings.Contains(body, "migration.started") {
		t.Fatalf("Space 查询不应返回系统事件, body=%s", body)
	}
}

func TestListAuditEvents_SystemScope(t *testing.T) {
	router, rec, _ := setupAuditRouter(t)
	_ = rec.Record(context.Background(), audit.EventInput{Scope: audit.ScopeSystem, ActorType: audit.ActorSystem, Action: "migration.started", ResourceType: "migration"})

	req := httptest.NewRequest("GET", "/api/audit/events?scope=system", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("查询系统审计事件期望 200, 实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "migration.started") {
		t.Fatalf("应返回系统事件, body=%s", w.Body.String())
	}
}

func TestListAuditEvents_QuerySpaceIDAndJSONPayload(t *testing.T) {
	router, rec, db := setupAuditRouter(t)
	const targetSpace = "space-two"
	if err := db.Create(&models.Space{ID: targetSpace, Name: "第二 Space", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("创建测试 Space 失败: %v", err)
	}
	_ = rec.Record(context.Background(), audit.EventInput{
		Scope:        audit.ScopeSpace,
		SpaceID:      targetSpace,
		ActorType:    audit.ActorSystem,
		Action:       "settings.updated",
		ResourceType: "setting",
		Before:       map[string]any{"network_proxy": "http://user:secret@example.test"},
		After:        map[string]any{"theme": "dark"},
	})

	req := httptest.NewRequest("GET", "/api/audit/events?space_id="+targetSpace+"&from=2026-07-08", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("查询指定 Space 审计事件期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Items []struct {
			Action     string         `json:"action"`
			BeforeJSON map[string]any `json:"before_json"`
			AfterJSON  map[string]any `json:"after_json"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Action != "settings.updated" {
		t.Fatalf("应只返回目标 Space 事件, body=%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("敏感代理凭据不应出现在响应中, body=%s", w.Body.String())
	}
	if body.Items[0].AfterJSON["theme"] != "dark" {
		t.Fatalf("普通字段应以 JSON 对象返回, body=%s", w.Body.String())
	}
}
