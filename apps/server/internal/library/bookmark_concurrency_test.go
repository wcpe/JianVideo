package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const bookmarkRaceTimeout = 5 * time.Second

func TestBookmarkSQLiteWAL同Revision并发更新返回冲突(t *testing.T) {
	fixture := newBookmarkWALFixture(t)
	bookmark := fixture.createBookmark(t)

	winnerErr, loserErr := fixture.runRace(t,
		func(svc *Service) error {
			_, err := svc.UpdateMediaBookmark(context.Background(), fixture.media.SpaceID, fixture.media.ID, bookmark.ID, BookmarkUpdate{PositionMS: 2000, Title: "优先更新", Revision: 1})
			return err
		},
		func(svc *Service) error {
			_, err := svc.UpdateMediaBookmark(context.Background(), fixture.media.SpaceID, fixture.media.ID, bookmark.ID, BookmarkUpdate{PositionMS: 3000, Title: "迟到更新", Revision: 1})
			return err
		},
	)

	if winnerErr != nil {
		t.Fatalf("先提交的更新应成功: %v", winnerErr)
	}
	assertBookmarkConflict(t, loserErr, 2, false)
	var conflict *BookmarkConflictError
	if !errors.As(loserErr, &conflict) || conflict.Current.Title != "优先更新" {
		t.Fatalf("更新冲突应携带最新服务端记录: %+v", conflict)
	}
}

func TestBookmarkSQLiteWAL同Revision并发删除返回冲突(t *testing.T) {
	fixture := newBookmarkWALFixture(t)
	bookmark := fixture.createBookmark(t)

	winnerErr, loserErr := fixture.runRace(t,
		func(svc *Service) error {
			return svc.DeleteMediaBookmark(context.Background(), fixture.media.SpaceID, fixture.media.ID, bookmark.ID, 1)
		},
		func(svc *Service) error {
			return svc.DeleteMediaBookmark(context.Background(), fixture.media.SpaceID, fixture.media.ID, bookmark.ID, 1)
		},
	)

	if winnerErr != nil {
		t.Fatalf("先提交的删除应成功: %v", winnerErr)
	}
	assertBookmarkConflict(t, loserErr, 0, true)
}

func TestBookmarkCAS非Busy数据库错误原样返回(t *testing.T) {
	t.Run("更新", func(t *testing.T) {
		svc, db, media := newBookmarkTestService(t)
		bookmark := createBookmarkForDatabaseError(t, svc, media)
		if err := db.Exec(`CREATE TRIGGER fail_bookmark_update BEFORE UPDATE ON media_bookmarks BEGIN SELECT RAISE(ABORT, '书签更新失败'); END;`).Error; err != nil {
			t.Fatalf("创建更新失败触发器失败: %v", err)
		}
		updated, err := svc.UpdateMediaBookmark(context.Background(), media.SpaceID, media.ID, bookmark.ID, BookmarkUpdate{PositionMS: 2000, Title: "触发失败", Revision: 1})
		assertBookmarkDatabaseError(t, err, "书签更新失败")
		if updated != nil {
			t.Fatalf("更新失败时不得返回未提交的书签副本: %+v", updated)
		}
	})

	t.Run("删除", func(t *testing.T) {
		svc, db, media := newBookmarkTestService(t)
		bookmark := createBookmarkForDatabaseError(t, svc, media)
		if err := db.Exec(`CREATE TRIGGER fail_bookmark_delete BEFORE DELETE ON media_bookmarks BEGIN SELECT RAISE(ABORT, '书签删除失败'); END;`).Error; err != nil {
			t.Fatalf("创建删除失败触发器失败: %v", err)
		}
		err := svc.DeleteMediaBookmark(context.Background(), media.SpaceID, media.ID, bookmark.ID, 1)
		assertBookmarkDatabaseError(t, err, "书签删除失败")
	})
}

func createBookmarkForDatabaseError(t *testing.T, svc *Service, media models.MediaFile) *models.MediaBookmark {
	t.Helper()
	bookmark, err := svc.CreateMediaBookmark(context.Background(), media.SpaceID, media.ID, BookmarkInput{PositionMS: 1000, Title: "数据库错误"})
	if err != nil {
		t.Fatalf("创建数据库错误测试书签失败: %v", err)
	}
	return bookmark
}

func assertBookmarkDatabaseError(t *testing.T, err error, message string) {
	t.Helper()
	var conflict *BookmarkConflictError
	if err == nil || !strings.Contains(err.Error(), message) || errors.As(err, &conflict) {
		t.Fatalf("非 busy 数据库错误必须原样返回: %v", err)
	}
}

type bookmarkWALFixture struct {
	serviceA *Service
	serviceB *Service
	media    models.MediaFile
}

func newBookmarkWALFixture(t *testing.T) bookmarkWALFixture {
	t.Helper()
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "bookmarks.db")) + "?_busy_timeout=5000&_journal_mode=WAL"
	dbA := openBookmarkWALConnection(t, dsn)
	dbB := openBookmarkWALConnection(t, dsn)
	if err := dbA.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaBookmark{}); err != nil {
		t.Fatalf("迁移并发书签测试表失败: %v", err)
	}
	var journalMode string
	if err := dbA.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil || journalMode != "wal" {
		t.Fatalf("并发书签测试必须使用 WAL: mode=%q err=%v", journalMode, err)
	}
	media := models.MediaFile{SpaceID: "space-a", LibraryID: 1, FilePath: "bookmark-race.mp4", FileName: "bookmark-race.mp4", Format: "mp4", Duration: 10, AddedAt: time.Now(), ModifiedAt: time.Now()}
	if err := dbA.Create(&media).Error; err != nil {
		t.Fatalf("创建并发书签媒体失败: %v", err)
	}
	return bookmarkWALFixture{serviceA: NewService(dbA), serviceB: NewService(dbB), media: media}
}

func openBookmarkWALConnection(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开并发书签测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取并发书签数据库连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func (f bookmarkWALFixture) createBookmark(t *testing.T) *models.MediaBookmark {
	t.Helper()
	bookmark, err := f.serviceA.CreateMediaBookmark(context.Background(), f.media.SpaceID, f.media.ID, BookmarkInput{PositionMS: 1000, Title: "并发书签"})
	if err != nil {
		t.Fatalf("创建并发测试书签失败: %v", err)
	}
	return bookmark
}

func (f bookmarkWALFixture) runRace(t *testing.T, winner, loser func(*Service) error) (error, error) {
	t.Helper()
	ready := make(chan struct{}, 2)
	winnerRelease := make(chan struct{})
	loserRelease := make(chan struct{})
	f.serviceA.bookmarkRepo = &gatedBookmarkRepository{bookmarkRepository: f.serviceA.bookmarkRepo, ready: ready, release: winnerRelease}
	f.serviceB.bookmarkRepo = &gatedBookmarkRepository{bookmarkRepository: f.serviceB.bookmarkRepo, ready: ready, release: loserRelease}
	winnerResult := make(chan error, 1)
	loserResult := make(chan error, 1)
	go func() { winnerResult <- winner(f.serviceA) }()
	go func() { loserResult <- loser(f.serviceB) }()
	awaitBookmarkReads(t, ready)
	close(winnerRelease)
	winnerErr := awaitBookmarkResult(t, winnerResult)
	close(loserRelease)
	return winnerErr, awaitBookmarkResult(t, loserResult)
}

type gatedBookmarkRepository struct {
	bookmarkRepository
	ready   chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *gatedBookmarkRepository) GetTx(ctx context.Context, tx *gorm.DB, spaceID string, mediaID int64, id string) (*models.MediaBookmark, error) {
	bookmark, err := r.bookmarkRepository.GetTx(ctx, tx, spaceID, mediaID, id)
	if err == nil {
		r.once.Do(func() {
			r.ready <- struct{}{}
			<-r.release
		})
	}
	return bookmark, err
}

func awaitBookmarkReads(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	for range 2 {
		select {
		case <-ready:
		case <-time.After(bookmarkRaceTimeout):
			t.Fatal("等待两个书签事务读取相同 revision 超时")
		}
	}
}

func awaitBookmarkResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(bookmarkRaceTimeout):
		t.Fatal("等待书签并发操作完成超时")
		return nil
	}
}
