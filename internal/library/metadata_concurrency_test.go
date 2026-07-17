package library

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const metadataRaceTimeout = 5 * time.Second

func TestParseAndStoreMetadata删除在Upsert前获胜不产生孤儿(t *testing.T) {
	cases := []struct {
		name       string
		fileName   string
		normalized NormalizedEmbeddedMetadata
	}{
		{
			name:     "视频章节与媒体更新",
			fileName: "race.mkv",
			normalized: NormalizedEmbeddedMetadata{
				MediaType: MediaTypeVideo,
				Container: ContainerMetadata{Duration: 12, Bitrate: 2048},
				Chapters:  []ChapterMetadata{{SourceIndex: 0, StartMS: 0, EndMS: 1000, Title: "开场"}},
			},
		},
		{name: "图片空章节无媒体更新", fileName: "race.jpg", normalized: NormalizedEmbeddedMetadata{MediaType: MediaTypeImage}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parser, deleter, db, media := newMetadataRaceFixture(t, tc.fileName)
			parser.metadataParser = func(context.Context, models.MediaFile) (ParsedEmbeddedMetadata, error) {
				return ParsedEmbeddedMetadata{
					Source: MetadataSourceFFprobe, Tool: "ffprobe", ToolVersion: "test",
					RawJSON: `{}`, NormalizedJSON: `{}`, Normalized: tc.normalized,
				}, nil
			}

			reached := make(chan struct{}, 1)
			release := make(chan struct{})
			parser.metadataRepo = &gatedMetadataRepository{metadataRepository: parser.metadataRepo, reached: reached, release: release}
			result := make(chan error, 1)
			go func() {
				_, err := parser.ParseAndStoreMetadata(context.Background(), media.SpaceID, media.ID)
				result <- err
			}()

			awaitMetadataGate(t, reached)
			if err := deleter.DeleteMediaFileByLibraryAndPath(media.LibraryID, media.FilePath); err != nil {
				t.Fatalf("删除事务应先提交成功: %v", err)
			}
			close(release)

			if err := awaitMetadataResult(t, result); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("删除获胜后解析事务应回滚并返回媒体不存在: %v", err)
			}
			assertNoMetadataOrChapterOrphans(t, db, media)
		})
	}
}

type gatedMetadataRepository struct {
	metadataRepository
	reached chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *gatedMetadataRepository) Upsert(ctx context.Context, row *models.MediaMetadata, chapters []models.MediaChapter, updates map[string]any) error {
	r.once.Do(func() {
		r.reached <- struct{}{}
		<-r.release
	})
	return r.metadataRepository.Upsert(ctx, row, chapters, updates)
}

func newMetadataRaceFixture(t *testing.T, fileName string) (*Service, *Service, *gorm.DB, models.MediaFile) {
	t.Helper()
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "metadata-race.db")) + "?_busy_timeout=5000&_journal_mode=WAL"
	parserDB := openMetadataRaceConnection(t, dsn)
	deleterDB := openMetadataRaceConnection(t, dsn)
	if err := parserDB.AutoMigrate(&models.MediaFile{}, &models.MediaMetadata{}, &models.MediaChapter{}); err != nil {
		t.Fatalf("迁移元数据并发测试表失败: %v", err)
	}
	media := models.MediaFile{
		SpaceID: "space-a", LibraryID: 1, FilePath: filepath.ToSlash(filepath.Join(t.TempDir(), fileName)),
		FileName: fileName, Format: filepath.Ext(fileName), FileSize: 100,
		AddedAt: time.Now(), ModifiedAt: time.Now(),
	}
	if err := parserDB.Create(&media).Error; err != nil {
		t.Fatalf("创建元数据并发测试媒体失败: %v", err)
	}
	return NewService(parserDB), NewService(deleterDB), parserDB, media
}

func openMetadataRaceConnection(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开元数据并发测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取元数据并发数据库连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func awaitMetadataGate(t *testing.T, reached <-chan struct{}) {
	t.Helper()
	select {
	case <-reached:
	case <-time.After(metadataRaceTimeout):
		t.Fatal("等待元数据解析进入 upsert 前闸门超时")
	}
}

func awaitMetadataResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(metadataRaceTimeout):
		t.Fatal("等待元数据并发解析结束超时")
		return nil
	}
}

func assertNoMetadataOrChapterOrphans(t *testing.T, db *gorm.DB, media models.MediaFile) {
	t.Helper()
	for name, model := range map[string]any{"metadata": &models.MediaMetadata{}, "chapters": &models.MediaChapter{}} {
		var count int64
		if err := db.Model(model).Where("space_id = ? AND media_id = ?", media.SpaceID, media.ID).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("删除获胜后不得遗留 %s 孤儿: %d", name, count)
		}
	}
}
