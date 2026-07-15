package migration

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestChaptersBookmarksMigration创建表约束与索引(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateChaptersBookmarks(context.Background(), db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", attempt+1, err)
		}
	}
	if _, err := validateChaptersBookmarks(context.Background(), db); err != nil {
		t.Fatalf("迁移校验失败: %v", err)
	}
	for _, table := range []string{"media_chapters", "media_bookmarks"} {
		if !tableExists(db, table) {
			t.Fatalf("缺少表 %s", table)
		}
	}
	for _, index := range []string{
		"idx_media_chapters_space_media_source_index",
		"idx_media_chapters_space_media_start",
		"idx_media_bookmarks_space_media_position_created",
	} {
		if !indexExists(db, index) {
			t.Fatalf("缺少索引 %s", index)
		}
	}
}

func TestDefaultMigrations在0020后追加FR2060(t *testing.T) {
	migrations := DefaultMigrations()
	last := migrations[len(migrations)-1]
	if last.ID != "20260712_0022_fr2_060_chapters_bookmarks" {
		t.Fatalf("FR2-060 应追加为 0021，实际最后迁移为 %s", last.ID)
	}
	if last.Estimate == nil || last.Up == nil || last.Validate == nil || !last.SafeToRetry {
		t.Fatalf("FR2-060 迁移定义不完整: %+v", last)
	}
}
