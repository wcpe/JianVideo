package migration

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFR2059封面迁移创建可信选择字段与隔离索引(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cover-migration.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("读取底层数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := migrateBaselineSchema(context.Background(), db); err != nil {
		t.Fatalf("初始化基础 schema 失败: %v", err)
	}
	if err := migrateSmartCovers(context.Background(), db); err != nil {
		t.Fatalf("执行封面迁移失败: %v", err)
	}
	if _, err := validateSmartCovers(context.Background(), db); err != nil {
		t.Fatalf("封面迁移校验失败: %v", err)
	}

	for _, column := range []string{
		"media_id", "space_id", "selected_asset_id", "selected_source",
		"selected_timestamp_seconds", "selected_fingerprint", "manual", "updated_at",
	} {
		if !columnExists(db, "media_covers", column) {
			t.Fatalf("media_covers 缺少字段 %s", column)
		}
	}
	for _, index := range []string{
		"idx_media_covers_space_media", "idx_cover_candidates_space_media_created",
		"idx_cover_candidates_space_media_fingerprint", "idx_cover_candidates_asset_id",
	} {
		if !indexExists(db, index) {
			t.Fatalf("封面索引不存在: %s", index)
		}
	}
}

func TestDefaultMigrations包含FR2059封面迁移(t *testing.T) {
	for _, item := range DefaultMigrations() {
		if item.ID == "20260712_0018_fr2_059_smart_covers" {
			return
		}
	}
	t.Fatal("默认迁移缺少 FR2-059 智能封面步骤")
}
