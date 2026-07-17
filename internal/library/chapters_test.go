package library

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestRealEmbeddedChapterFixture使用配置FFprobe且无副作用(t *testing.T) {
	ffprobePath := getFFprobePath()
	if ffprobePath == "" {
		t.Fatal("项目配置的 ffprobe 路径不能为空")
	}
	if _, err := exec.LookPath(ffprobePath); err != nil {
		t.Skipf("项目配置的 ffprobe 不可用，跳过真实章节集成测试: %s", ffprobePath)
	}

	fixturePath := filepath.Join("testdata", "chapters", "embedded-chapters-three.mp4")
	beforeInfo, err := os.Stat(fixturePath)
	if err != nil {
		t.Fatalf("读取章节素材失败: %v", err)
	}
	beforeHash := fileSHA256(t, fixturePath)

	svc, db, media := newChapterTestService(t)
	media.FilePath = fixturePath
	media.FileName = filepath.Base(fixturePath)
	media.FileSize = beforeInfo.Size()
	media.ModifiedAt = beforeInfo.ModTime()
	media.ContentHash = beforeHash
	media.ContentHashAlgo = ContentHashAlgoSHA256
	media.ContentHashStale = false
	if err := db.Save(&media).Error; err != nil {
		t.Fatalf("绑定真实章节素材失败: %v", err)
	}

	cacheDir := t.TempDir()
	previousThumbnailDir := thumbnailDir
	thumbnailDir = cacheDir
	t.Cleanup(func() { thumbnailDir = previousThumbnailDir })

	var firstIDs []string
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := svc.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID); err != nil {
			t.Fatalf("第 %d 次真实 ffprobe 解析失败: %v", attempt+1, err)
		}
		chapters, err := svc.ListMediaChapters(context.Background(), media.SpaceID, media.ID)
		if err != nil {
			t.Fatalf("第 %d 次查询章节失败: %v", attempt+1, err)
		}
		assertRealChapterBoundaries(t, chapters)
		if attempt == 0 {
			firstIDs = []string{chapters[0].ID, chapters[1].ID, chapters[2].ID}
			continue
		}
		for index := range chapters {
			if chapters[index].ID != firstIDs[index] {
				t.Fatalf("重复解析应整组替换且保持稳定 ID: got=%v want=%v", chapters, firstIDs)
			}
		}
	}

	afterInfo, err := os.Stat(fixturePath)
	if err != nil {
		t.Fatalf("再次读取章节素材失败: %v", err)
	}
	if afterHash := fileSHA256(t, fixturePath); afterHash != beforeHash {
		t.Fatalf("真实章节解析修改了源文件 hash: before=%s after=%s", beforeHash, afterHash)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("真实章节解析修改了源文件 mtime: before=%v after=%v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
	stored, err := svc.GetMediaFileByIDInSpace(media.SpaceID, media.ID)
	if err != nil {
		t.Fatalf("读取解析后的媒体记录失败: %v", err)
	}
	if stored.ContentHash != beforeHash || stored.ContentHashStale || !stored.ModifiedAt.Equal(beforeInfo.ModTime()) {
		t.Fatalf("真实章节解析修改了源指纹字段: %+v", stored)
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("读取图片缓存目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("章节解析不应生成图片缓存: %+v", entries)
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

func assertRealChapterBoundaries(t *testing.T, chapters []models.MediaChapter) {
	t.Helper()
	want := []struct {
		startMS int64
		endMS   int64
		title   string
	}{
		{startMS: 0, endMS: 5000, title: "开场"},
		{startMS: 5000, endMS: 10000, title: "Main"},
		{startMS: 10000, endMS: 15000, title: "结尾"},
	}
	if len(chapters) != len(want) {
		t.Fatalf("真实素材应解析出三个且不重复的章节: %+v", chapters)
	}
	for index := range want {
		if chapters[index].StartMS != want[index].startMS || chapters[index].EndMS != want[index].endMS || chapters[index].Title != want[index].title {
			t.Fatalf("真实章节边界错误: index=%d got=%+v want=%+v", index, chapters[index], want[index])
		}
	}
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
