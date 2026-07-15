package migration

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestWatchStatesMigration回填可重入且不覆盖新状态(t *testing.T) {
	db := newWatchStateMigrationDB(t)
	watchedAt := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	rows := []models.MediaFile{
		{ID: 1, SpaceID: "space-a", LibraryID: 1, FilePath: "/a.mp4", FileName: "a.mp4", LastPosition: 42, LastWatchedAt: &watchedAt},
		{ID: 2, SpaceID: "space-a", LibraryID: 1, FilePath: "/b.mp4", FileName: "b.mp4", Watched: true, LastPosition: 99, LastWatchedAt: &watchedAt},
		{ID: 3, SpaceID: "space-a", LibraryID: 1, FilePath: "/c.mp4", FileName: "c.mp4", LastPosition: 12},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("创建历史观看数据失败: %v", err)
	}

	plan, err := estimateWatchStates(context.Background(), db)
	if err != nil {
		t.Fatalf("估算迁移失败: %v", err)
	}
	if plan.EstimatedRows != 2 || len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "缺少 last_watched_at") {
		t.Fatalf("迁移估算未报告跳过旧行: %+v", plan)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateWatchStates(context.Background(), db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", attempt+1, err)
		}
	}
	if _, err := validateWatchStates(context.Background(), db); err != nil {
		t.Fatalf("迁移校验失败: %v", err)
	}

	var states []models.WatchState
	if err := db.Order("media_id ASC").Find(&states).Error; err != nil {
		t.Fatalf("读取回填状态失败: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("应回填两行，实际 %+v", states)
	}
	if states[0].PositionSeconds != 42 || states[0].Completed || states[0].Revision != 1 {
		t.Fatalf("未完成状态回填错误: %+v", states[0])
	}
	if states[1].PositionSeconds != 0 || !states[1].Completed || states[1].CompletedAt == nil {
		t.Fatalf("完成状态回填错误: %+v", states[1])
	}

	if err := db.Model(&models.WatchState{}).
		Where("space_id = ? AND media_id = ?", "space-a", 1).
		Updates(map[string]any{"position_seconds": 88, "revision": 7}).Error; err != nil {
		t.Fatalf("模拟新逻辑更新失败: %v", err)
	}
	if err := migrateWatchStates(context.Background(), db); err != nil {
		t.Fatalf("迁移重入失败: %v", err)
	}
	var current models.WatchState
	if err := db.Where("space_id = ? AND media_id = ?", "space-a", 1).First(&current).Error; err != nil {
		t.Fatalf("读取重入后状态失败: %v", err)
	}
	if current.PositionSeconds != 88 || current.Revision != 7 {
		t.Fatalf("重入迁移覆盖了新状态: %+v", current)
	}
}

func TestWatchStatesMigration校验拒绝真源与兼容投影不一致(t *testing.T) {
	db := newWatchStateMigrationDB(t)
	watchedAt := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	media := models.MediaFile{
		ID: 1, SpaceID: "space-a", LibraryID: 1, FilePath: "/a.mp4", FileName: "a.mp4",
		LastPosition: 42, LastWatchedAt: &watchedAt,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建历史观看数据失败: %v", err)
	}
	if err := migrateWatchStates(context.Background(), db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if err := db.Model(&models.MediaFile{}).Where("id = ?", media.ID).Update("last_position", 7).Error; err != nil {
		t.Fatalf("模拟兼容投影损坏失败: %v", err)
	}
	if _, err := validateWatchStates(context.Background(), db); err == nil || !strings.Contains(err.Error(), "兼容投影") {
		t.Fatalf("真源与兼容投影不一致时应校验失败，实际 %v", err)
	}
}

func TestWatchStatesMigration建立Space媒体唯一键和查询索引(t *testing.T) {
	db := newWatchStateMigrationDB(t)
	if err := migrateWatchStates(context.Background(), db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	now := time.Now().UTC()
	state := models.WatchState{SpaceID: "space-a", MediaID: 9, LastWatchedAt: now, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&state).Error; err != nil {
		t.Fatalf("创建观看状态失败: %v", err)
	}
	if err := db.Create(&state).Error; err == nil {
		t.Fatal("同一 Space 和媒体的重复状态应被唯一键拒绝")
	}
	state.SpaceID = "space-b"
	if err := db.Create(&state).Error; err != nil {
		t.Fatalf("不同 Space 应允许相同 media_id: %v", err)
	}
	for _, indexName := range []string{
		"idx_watch_states_space_media",
		"idx_watch_states_space_history",
		"idx_watch_states_space_continue",
	} {
		if !indexExists(db, indexName) {
			t.Fatalf("缺少观看状态索引 %s", indexName)
		}
	}
}

func TestDefaultMigrations包含FR2045观看状态迁移(t *testing.T) {
	migrations := DefaultMigrations()
	last := migrations[len(migrations)-1]
	if last.ID != "20260712_0021_fr2_045_watch_states" {
		t.Fatalf("FR2-045 应追加为新迁移，实际最后迁移为 %s", last.ID)
	}
	if last.Estimate == nil || last.Up == nil || last.Validate == nil || !last.SafeToRetry {
		t.Fatalf("FR2-045 迁移定义不完整: %+v", last)
	}
}

func newWatchStateMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开迁移测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaFile{}); err != nil {
		t.Fatalf("创建历史媒体表失败: %v", err)
	}
	return db
}
