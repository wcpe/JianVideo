package migration

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestSubtitleTracksMigration建立来源唯一约束(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateSubtitleTracks(context.Background(), db); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", attempt+1, err)
		}
	}
	if _, err := validateSubtitleTracks(context.Background(), db); err != nil {
		t.Fatalf("迁移校验失败: %v", err)
	}
	assertSubtitleTrackUniqueConstraints(t, db)
}

func assertSubtitleTrackUniqueConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []models.MediaSubtitleTrack{
		{ID: "emb-a", SpaceID: "space-a", MediaID: 1, Source: "embedded", StreamIndex: 2, Format: "srt"},
		{ID: "sid-a", SpaceID: "space-a", MediaID: 1, Source: "sidecar", SourceRef: "Movie.EN.SRT", StreamIndex: -1, Format: "srt"},
		{ID: "upl-a", SpaceID: "space-a", MediaID: 1, Source: "uploaded", SourceRef: "upload.srt", StreamIndex: -1, Format: "srt", StorageRelativePath: "subtitles/space-a/1/upl-a.srt"},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("创建轨道失败: %+v err=%v", row, err)
		}
	}
	assertSubtitleDuplicateRejected(t, db, models.MediaSubtitleTrack{ID: "emb-b", SpaceID: "space-a", MediaID: 1, Source: "embedded", StreamIndex: 2, Format: "vtt"})
	assertSubtitleDuplicateRejected(t, db, models.MediaSubtitleTrack{ID: "sid-b", SpaceID: "space-a", MediaID: 1, Source: "sidecar", SourceRef: "movie.en.srt", StreamIndex: -1, Format: "srt"})
	assertSubtitleDuplicateRejected(t, db, models.MediaSubtitleTrack{ID: "upl-a", SpaceID: "space-b", MediaID: 1, Source: "uploaded", SourceRef: "upload.srt", StreamIndex: -1, Format: "srt", StorageRelativePath: "subtitles/space-b/1/upl-a.srt"})
}

func assertSubtitleDuplicateRejected(t *testing.T, db *gorm.DB, row models.MediaSubtitleTrack) {
	t.Helper()
	if err := db.Create(&row).Error; err == nil {
		t.Fatalf("唯一约束应拒绝重复轨道: %+v", row)
	}
}

func TestSubtitleTracksMigration规范化并去重历史索引行(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.MediaSubtitleTrack{}); err != nil {
		t.Fatalf("创建历史表失败: %v", err)
	}
	for _, statement := range subtitleTrackIndexStatements() {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建历史唯一索引失败: %v", err)
		}
	}
	createdAt := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	rows := historicalSubtitleConflictRows(createdAt)
	if err := db.Session(&gorm.Session{SkipHooks: true}).Create(&rows).Error; err != nil {
		t.Fatalf("创建历史冲突记录失败: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateSubtitleTracks(context.Background(), db); err != nil {
			t.Fatalf("第 %d 次迁移历史记录失败: %v", attempt+1, err)
		}
	}
	if _, err := validateSubtitleTracks(context.Background(), db); err != nil {
		t.Fatalf("历史冲突迁移后唯一索引无效: %v", err)
	}
	assertHistoricalSubtitleRows(t, db)
	assertSubtitleDuplicateRejected(t, db, models.MediaSubtitleTrack{ID: "sid-c", SpaceID: "space-a", MediaID: 1, Source: "sidecar", SourceRef: "folder/movie.en.srt", StreamIndex: -1, Format: "srt"})
	assertSubtitleDuplicateRejected(t, db, models.MediaSubtitleTrack{ID: "emb-c", SpaceID: "space-a", MediaID: 1, Source: "embedded", StreamIndex: 2, Format: "srt"})
}

func historicalSubtitleConflictRows(createdAt time.Time) []models.MediaSubtitleTrack {
	later := createdAt.Add(time.Minute)
	return []models.MediaSubtitleTrack{
		{ID: "sid-a", SpaceID: "space-a", MediaID: 1, Source: "SIDECAR", SourceRef: `Folder\Movie.EN.SRT`, StreamIndex: -1, Format: "SRT", CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: "sid-b", SpaceID: "space-a", MediaID: 1, Source: " sidecar ", SourceRef: "folder/movie.en.srt", StreamIndex: -1, Format: "VTT", CreatedAt: later, UpdatedAt: later},
		{ID: "emb-a", SpaceID: "space-a", MediaID: 1, Source: "EMBEDDED", StreamIndex: 2, Format: "SRT", CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: "emb-b", SpaceID: "space-a", MediaID: 1, Source: " embedded ", StreamIndex: 2, Format: "VTT", CreatedAt: createdAt, UpdatedAt: later},
		{ID: "upl-a", SpaceID: "space-a", MediaID: 1, Source: "UPLOADED", SourceRef: "same.srt", StorageRelativePath: "subtitles/space-a/1/upl-a.srt", StreamIndex: -1, Format: "SRT", CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: "upl-b", SpaceID: "space-a", MediaID: 1, Source: " uploaded ", SourceRef: "same.srt", StorageRelativePath: "subtitles/space-a/1/upl-b.srt", StreamIndex: -1, Format: "SRT", CreatedAt: later, UpdatedAt: later},
	}
}

func assertHistoricalSubtitleRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []models.MediaSubtitleTrack
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("读取迁移后字幕记录失败: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("应仅去除可重建的重复索引行，实际记录: %+v", rows)
	}
	ids := map[string]models.MediaSubtitleTrack{}
	for _, row := range rows {
		ids[row.ID] = row
	}
	if _, ok := ids["sid-b"]; ok {
		t.Fatalf("应删除较晚的外挂重复索引行: %+v", rows)
	}
	if _, ok := ids["emb-b"]; ok {
		t.Fatalf("应删除较晚的内嵌重复索引行: %+v", rows)
	}
	if ids["sid-a"].SourceRef != "folder/movie.en.srt" || ids["sid-a"].Format != "srt" {
		t.Fatalf("保留的外挂索引行未规范化: %+v", ids["sid-a"])
	}
	if _, ok := ids["upl-a"]; !ok {
		t.Fatalf("uploaded 行不得被去重删除: %+v", rows)
	}
	if _, ok := ids["upl-b"]; !ok {
		t.Fatalf("uploaded 行不得被去重删除: %+v", rows)
	}
}

func TestBaselineSchema包含字幕轨道表(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 baseline 测试库失败: %v", err)
	}
	if err := migrateBaselineSchema(context.Background(), db); err != nil {
		t.Fatalf("执行 baseline 迁移失败: %v", err)
	}
	if !tableExists(db, "media_subtitle_tracks") {
		t.Fatal("baseline 必须包含 media_subtitle_tracks 表")
	}
}

func TestSubtitleTracksValidate拒绝缺失或错误索引(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开校验测试库失败: %v", err)
	}
	if err := migrateSubtitleTracks(context.Background(), db); err != nil {
		t.Fatalf("执行字幕迁移失败: %v", err)
	}
	if err := db.Exec("DROP INDEX idx_media_subtitle_tracks_sidecar_unique").Error; err != nil {
		t.Fatalf("删除测试索引失败: %v", err)
	}
	if _, err := validateSubtitleTracks(context.Background(), db); err == nil {
		t.Fatal("缺失来源唯一索引时 validate 必须失败")
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_media_subtitle_tracks_sidecar_unique
		ON media_subtitle_tracks(space_id, media_id, source_ref)`).Error; err != nil {
		t.Fatalf("创建错误测试索引失败: %v", err)
	}
	if _, err := validateSubtitleTracks(context.Background(), db); err == nil {
		t.Fatal("来源唯一索引缺少 partial 条件时 validate 必须失败")
	}
}

func TestDefaultMigrations包含FR2044字幕轨道迁移(t *testing.T) {
	for _, migration := range DefaultMigrations() {
		if migration.ID != "20260712_0020_fr2_044_subtitle_tracks" {
			continue
		}
		if migration.Estimate == nil || migration.Up == nil || migration.Validate == nil || !migration.SafeToRetry {
			t.Fatalf("FR2-044 迁移定义不完整: %+v", migration)
		}
		return
	}
	t.Fatal("默认迁移缺少 FR2-044")
}
		if migration.Estimate == nil || migration.Up == nil || migration.Validate == nil || !migration.SafeToRetry {
			t.Fatalf("FR2-044 迁移定义不完整: %+v", migration)
		}
		return
=======
	migrations := DefaultMigrations()
	var target *Migration
	for index := range migrations {
		if migrations[index].ID == "20260712_0020_fr2_044_subtitle_tracks" {
			target = &migrations[index]
			break
		}
	}
	if target == nil {
		t.Fatal("FR2-044 字幕轨道迁移不存在")
	}
	if target.Estimate == nil || target.Up == nil || target.Validate == nil || !target.SafeToRetry {
		t.Fatalf("FR2-044 迁移定义不完整: %+v", target)
>>>>>>> b1a05ef (feat(library): 建立章节解析与书签真源)
	}
	t.Fatal("默认迁移缺少 FR2-044")
}
