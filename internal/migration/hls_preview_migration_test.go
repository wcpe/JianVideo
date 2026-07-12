package migration

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestHLSPreviewMigrationConvertsLegacyTaskWithoutChangingTasksCenterMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	spaceID := DefaultSpaceID
	legacy := models.Task{
		Scope: models.TaskScopeSpace, SpaceID: &spaceID, Type: "transcode.hls",
		Status: models.TaskStatusPending, MaxAttempts: 1, IdempotencyKey: "transcode:9",
		PayloadJSON:  `{"legacy_table":"transcode_tasks","legacy_id":9,"media_id":42,"preset_id":7,"codec":"h264","width":1280,"height":720}`,
		ResourceType: "media", ResourceID: "42",
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("创建旧 HLS 任务失败: %v", err)
	}

	plan, err := estimateHLSPreviewTasks(context.Background(), db)
	if err != nil || plan.EstimatedRows != 1 {
		t.Fatalf("HLS preview 迁移估算异常: plan=%+v err=%v", plan, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateHLSPreviewTasks(context.Background(), db); err != nil {
			t.Fatalf("HLS preview 迁移第 %d 次执行失败: %v", attempt+1, err)
		}
	}
	if _, err := validateHLSPreviewTasks(context.Background(), db); err != nil {
		t.Fatalf("HLS preview 迁移校验失败: %v", err)
	}

	var migrated models.Task
	if err := db.First(&migrated, legacy.ID).Error; err != nil {
		t.Fatalf("读取迁移后任务失败: %v", err)
	}
	if migrated.Type != "transcode.hls.preview" || migrated.MaxAttempts != 3 {
		t.Fatalf("迁移后任务信封异常: %+v", migrated)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(migrated.PayloadJSON), &payload); err != nil {
		t.Fatalf("解析迁移后载荷失败: %v", err)
	}
	if payload["legacy_table"] != "transcode_tasks" || payload["profile_id"] != "h264" || payload["space_id"] != DefaultSpaceID {
		t.Fatalf("迁移后载荷缺少兼容字段: %v", payload)
	}
	if payload["media_id"] != float64(42) || payload["preset_id"] != float64(7) || payload["width"] != float64(1280) || payload["height"] != float64(720) {
		t.Fatalf("迁移后载荷快照异常: %v", payload)
	}
}

func TestHLSPreviewMigrationUsesNewIDAfterExistingMigrations(t *testing.T) {
	migrations := DefaultMigrations()
	positions := map[string]int{}
	for index, migration := range migrations {
		positions[migration.ID] = index
	}
	if positions["20260708_0006_fr2_037_tasks_center"] >= positions["20260712_0016_fr2_008_hls_preview_tasks"] {
		t.Fatalf("FR2-008 必须使用新增迁移 ID，不能改写旧任务中心迁移: %v", positions)
	}
	if positions["20260712_0015_fr2_017_settings_preflight"] >= positions["20260712_0016_fr2_008_hls_preview_tasks"] {
		t.Fatalf("FR2-008 迁移必须追加在 settings 预检之后: %v", positions)
	}
}
