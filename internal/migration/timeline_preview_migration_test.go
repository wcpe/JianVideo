package migration

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestTimelinePreviewMigration可重试并建立约束(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	plan, err := estimateTimelinePreviews(context.Background(), db)
	if err != nil || plan.EstimatedRows != 0 {
		t.Fatalf("迁移估算异常: plan=%+v err=%v", plan, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateTimelinePreviews(context.Background(), db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", attempt+1, err)
		}
	}
	if _, err := validateTimelinePreviews(context.Background(), db); err != nil {
		t.Fatalf("迁移校验失败: %v", err)
	}
	assertTimelinePreviewConstraints(t, db)
	for _, column := range []string{"pending_source_fingerprint", "pending_generation_id", "pending_task_id"} {
		if !columnExists(db, "media_timeline_previews", column) {
			t.Fatalf("时间线预览请求指针缺少列 %s", column)
		}
	}
	if !indexExists(db, "idx_media_timeline_previews_pending_task") {
		t.Fatal("时间线预览请求指针缺少 pending task 索引")
	}
}

func assertTimelinePreviewConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	row := models.MediaTimelinePreview{
		SpaceID: "space-a", MediaID: 7, ProfileID: "desktop",
		SourceFingerprint: "source-a", GenerationID: "generation-a", AssetID: 11, UpdatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("创建时间线预览指针失败: %v", err)
	}
	duplicate := row
	duplicate.ID = 0
	duplicate.SourceFingerprint = "source-b"
	duplicate.GenerationID = "generation-b"
	duplicate.AssetID = 12
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("唯一键应拒绝同 Space、媒体和 profile 的重复指针")
	}
	otherSpace := duplicate
	otherSpace.ID = 0
	otherSpace.SpaceID = "space-b"
	if err := db.Create(&otherSpace).Error; err != nil {
		t.Fatalf("不同 Space 应允许相同媒体和 profile: %v", err)
	}
	plan, err := estimateTimelinePreviews(context.Background(), db)
	if err != nil || plan.EstimatedRows != 2 {
		t.Fatalf("已有指针迁移估算异常: plan=%+v err=%v", plan, err)
	}
}

func TestDefaultMigrations包含FR2029时间线预览迁移(t *testing.T) {
	migrations := DefaultMigrations()
	positions := map[string]int{}
	for index, migration := range migrations {
		positions[migration.ID] = index
	}
	id := "20260712_0019_fr2_029_timeline_previews"
	position, ok := positions[id]
	if !ok {
		t.Fatalf("默认迁移缺少 FR2-029: %v", positions)
	}
	migration := migrations[position]
	if migration.Estimate == nil || migration.Up == nil || migration.Validate == nil || !migration.SafeToRetry {
		t.Fatalf("FR2-029 迁移定义不完整: %+v", migration)
	}
}
