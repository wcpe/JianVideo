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

func TestChaptersBookmarksMigration媒体物理删除级联清理(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:chapters-cascade?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开外键测试库失败: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE spaces (id TEXT PRIMARY KEY)`,
		`CREATE TABLE media_files (id INTEGER PRIMARY KEY, space_id TEXT NOT NULL)`,
		`INSERT INTO spaces(id) VALUES ('space-a')`,
		`INSERT INTO media_files(id, space_id) VALUES (1, 'space-a')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备父表失败: %v", err)
		}
	}
	if err := migrateChaptersBookmarks(context.Background(), db); err != nil {
		t.Fatalf("执行章节书签迁移失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO media_chapters
		(id, space_id, media_id, source, source_index, start_ms, end_ms, title, language, source_fingerprint, parsed_at, created_at, updated_at)
		VALUES ('chapter-1', 'space-a', 1, 'embedded', 0, 0, 1000, '开场', '', 'fingerprint', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatalf("创建章节失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO media_bookmarks
		(id, space_id, media_id, position_ms, title, note, revision, created_at, updated_at)
		VALUES ('bookmark-1', 'space-a', 1, 500, '重点', NULL, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatalf("创建书签失败: %v", err)
	}
	if err := migrateChaptersBookmarksCascade(context.Background(), db); err != nil {
		t.Fatalf("升级级联外键失败: %v", err)
	}
	if _, err := validateChaptersBookmarksCascade(context.Background(), db); err != nil {
		t.Fatalf("级联外键校验失败: %v", err)
	}

	if err := db.Exec(`DELETE FROM media_files WHERE id = 1`).Error; err != nil {
		t.Fatalf("媒体物理删除不应被章节书签阻断: %v", err)
	}
	for _, table := range []string{"media_chapters", "media_bookmarks"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("媒体删除后 %s 应级联清理，实际剩余 %d", table, count)
		}
	}
}

func TestDefaultMigrations在0020后追加FR2060(t *testing.T) {
	migrations := DefaultMigrations()
	last := migrations[len(migrations)-1]
	if last.ID != "20260712_0023_fr2_060_media_delete_cascade" {
		t.Fatalf("FR2-060 级联修复应追加为 0023，实际最后迁移为 %s", last.ID)
	}
	if last.Estimate == nil || last.Up == nil || last.Validate == nil || !last.SafeToRetry {
		t.Fatalf("FR2-060 迁移定义不完整: %+v", last)
	}
}
