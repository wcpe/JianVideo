package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestParseVideoEmbeddedMetadata解析章节时间回退与非法过滤(t *testing.T) {
	raw := []byte(`{
		"format":{"duration":"12"},
		"streams":[],
		"chapters":[
			{"id":3,"start_time":"10","end_time":"20","tags":{"title":" 结尾 ","language":"zh"}},
			{"id":1,"time_base":"1/1000","start":5000,"end":10000,"tags":{}},
			{"id":0,"start_time":"0","end_time":"5","tags":{"title":"开场"}},
			{"id":4,"start_time":"12.1","end_time":"13"},
			{"id":5,"start_time":"NaN","end_time":"14"},
			{"id":6,"start_time":"8","end_time":"8"},
			{"id":7,"start_time":"-1","end_time":"1"}
		]
	}`)

	parsed, err := parseVideoEmbeddedMetadata(raw)
	if err != nil {
		t.Fatalf("解析章节失败: %v", err)
	}
	if len(parsed.Chapters) != 3 {
		t.Fatalf("应过滤非法章节，实际 %+v", parsed.Chapters)
	}
	if parsed.Chapters[0].StartMS != 0 || parsed.Chapters[0].EndMS != 5000 || parsed.Chapters[0].Title != "开场" {
		t.Fatalf("首章错误: %+v", parsed.Chapters[0])
	}
	if parsed.Chapters[1].StartMS != 5000 || parsed.Chapters[1].EndMS != 10000 || parsed.Chapters[1].Title != "章节 2" {
		t.Fatalf("time_base 或标题回退错误: %+v", parsed.Chapters[1])
	}
	if parsed.Chapters[2].StartMS != 10000 || parsed.Chapters[2].EndMS != 12000 || parsed.Chapters[2].Language != "zh" {
		t.Fatalf("时长夹取或标签错误: %+v", parsed.Chapters[2])
	}
}

func TestMetadataChapterRefresh整组替换空列表清旧失败保留旧(t *testing.T) {
	svc, db, media := newChapterTestService(t)
	first := parsedMetadataWithChapters([]ChapterMetadata{
		{SourceIndex: 0, StartMS: 0, EndMS: 5000, Title: "开场"},
		{SourceIndex: 1, StartMS: 5000, EndMS: 10000, Title: "Main"},
	})
	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) { return first, nil }
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("首次刷新失败: %v", err)
	}
	assertChapterTitles(t, svc, media, []string{"开场", "Main"})

	empty := parsedMetadataWithChapters(nil)
	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) { return empty, nil }
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("空章节刷新失败: %v", err)
	}
	assertChapterTitles(t, svc, media, nil)

	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) { return first, nil }
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
		t.Fatalf("恢复旧章节失败: %v", err)
	}
	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) {
		return ParsedEmbeddedMetadata{}, errors.New("ffprobe 失败")
	}
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err == nil {
		t.Fatal("解析失败必须返回错误")
	}
	assertChapterTitles(t, svc, media, []string{"开场", "Main"})

	if err := db.Exec(`CREATE TRIGGER reject_chapter_insert BEFORE INSERT ON media_chapters BEGIN SELECT RAISE(ABORT, '拒绝章节写入'); END;`).Error; err != nil {
		t.Fatalf("创建失败注入触发器失败: %v", err)
	}
	replacement := parsedMetadataWithChapters([]ChapterMetadata{{SourceIndex: 2, StartMS: 1000, EndMS: 2000, Title: "替换"}})
	svc.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) { return replacement, nil }
	if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err == nil {
		t.Fatal("章节事务写入失败必须返回错误")
	}
	assertChapterTitles(t, svc, media, []string{"开场", "Main"})
}

func newChapterTestService(t *testing.T) (*Service, *gorm.DB, models.MediaFile) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaMetadata{}, &models.MediaChapter{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	media := models.MediaFile{SpaceID: "space-a", LibraryID: 1, FilePath: "chapter.mkv", FileName: "chapter.mkv", Format: "mkv", Duration: 12, AddedAt: time.Now(), ModifiedAt: time.Now()}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return NewService(db), db, media
}

func parsedMetadataWithChapters(chapters []ChapterMetadata) ParsedEmbeddedMetadata {
	normalized := NormalizedEmbeddedMetadata{MediaType: MediaTypeVideo, Chapters: chapters}
	return ParsedEmbeddedMetadata{
		Source: MetadataSourceFFprobe, Tool: "ffprobe", ToolVersion: "test",
		RawJSON: `{}`, NormalizedJSON: `{}`, Normalized: normalized,
	}
}

func assertChapterTitles(t *testing.T, svc *Service, media models.MediaFile, want []string) {
	t.Helper()
	chapters, err := svc.ListMediaChapters(context.Background(), media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("查询章节失败: %v", err)
	}
	if len(chapters) != len(want) {
		t.Fatalf("章节数量错误: got=%+v want=%+v", chapters, want)
	}
	for i := range want {
		if chapters[i].Title != want[i] {
			t.Fatalf("章节标题错误: got=%+v want=%+v", chapters, want)
		}
	}
}
