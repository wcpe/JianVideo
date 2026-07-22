package library

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func newNextEpisodeTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaInference{},
		&models.Album{},
		&models.AlbumItem{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(db), db
}

func seedSeriesEpisode(t *testing.T, svc *Service, _ *gorm.DB, libraryID int64, name string, season, episode int) *models.MediaFile {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, "D:/Series/"+name, 1024)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	// CreateMediaFile 可能已自动推断，用人工纠正覆盖为稳定 title/季集
	if _, err := svc.UpsertManualInferenceInSpace(models.DefaultSpaceID, mf.ID, InferenceManualInput{
		Kind:    models.LibraryKindSeries,
		Title:   "测试剧",
		Season:  season,
		Episode: episode,
	}); err != nil {
		t.Fatalf("写入推断失败: %v", err)
	}
	return mf
}

func TestFindNextEpisodeInSpace_SameSeasonThenNextSeason(t *testing.T) {
	svc, db := newNextEpisodeTestService(t)
	e01 := seedSeriesEpisode(t, svc, db, 1, "S01E01.mp4", 1, 1)
	e02 := seedSeriesEpisode(t, svc, db, 1, "S01E02.mp4", 1, 2)
	s2e1 := seedSeriesEpisode(t, svc, db, 1, "S02E01.mp4", 2, 1)

	// E01 → E02
	got, err := svc.FindNextEpisodeInSpace(models.DefaultSpaceID, e01.ID)
	if err != nil {
		t.Fatalf("查找下一集失败: %v", err)
	}
	if got.Media == nil || got.Media.ID != e02.ID {
		t.Fatalf("E01 下一集应为 E02, got %+v", got.Media)
	}
	if got.Next == nil || got.Next.Episode != 2 {
		t.Fatalf("下一集推断 episode 应为 2, got %+v", got.Next)
	}

	// E02 → S02E01
	got, err = svc.FindNextEpisodeInSpace(models.DefaultSpaceID, e02.ID)
	if err != nil {
		t.Fatalf("跨季查找失败: %v", err)
	}
	if got.Media == nil || got.Media.ID != s2e1.ID {
		t.Fatalf("E02 下一集应为 S02E01, got %+v", got.Media)
	}

	// 最后一集无下一集
	got, err = svc.FindNextEpisodeInSpace(models.DefaultSpaceID, s2e1.ID)
	if err != nil {
		t.Fatalf("末集查询失败: %v", err)
	}
	if got.Media != nil {
		t.Fatalf("末集不应有下一集, got media=%d", got.Media.ID)
	}
}

func TestFindNextEpisodeInSpace_SpaceIsolationAndMissing(t *testing.T) {
	svc, db := newNextEpisodeTestService(t)
	e01 := seedSeriesEpisode(t, svc, db, 1, "S01E01.mp4", 1, 1)
	// 另一 space 同 title 下一集不应被命中
	other := models.MediaFile{
		SpaceID:   "space-other",
		LibraryID: 1,
		FilePath:  "D:/Other/S01E02.mp4",
		FileName:  "S01E02.mp4",
		FileSize:  1024,
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("创建跨 space 媒体失败: %v", err)
	}
	if err := db.Create(&models.MediaInference{
		MediaID: other.ID,
		SpaceID: "space-other",
		Kind:    models.LibraryKindSeries,
		Title:   "测试剧",
		Season:  1,
		Episode: 2,
		Source:  InferenceSourceManual,
		Manual:  true,
	}).Error; err != nil {
		t.Fatalf("跨 space 推断失败: %v", err)
	}

	got, err := svc.FindNextEpisodeInSpace(models.DefaultSpaceID, e01.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Media != nil {
		t.Fatalf("不应连播到其他 Space, got media=%d space=%s", got.Media.ID, got.Media.SpaceID)
	}

	// 无推断：直接入库绕过 CreateMediaFile 的自动推断
	bare := models.MediaFile{
		SpaceID:   models.DefaultSpaceID,
		LibraryID: 1,
		FilePath:  "D:/Series/orphan.mp4",
		FileName:  "orphan.mp4",
		FileSize:  100,
	}
	if err := db.Create(&bare).Error; err != nil {
		t.Fatalf("创建无推断媒体失败: %v", err)
	}
	got, err = svc.FindNextEpisodeInSpace(models.DefaultSpaceID, bare.ID)
	if err != nil {
		t.Fatalf("无推断查询失败: %v", err)
	}
	if got.Media != nil || got.Current != nil {
		t.Fatalf("无推断应返回空, got %+v", got)
	}
}

func TestFindAlbumNeighborInSpace_Order(t *testing.T) {
	svc, _ := newNextEpisodeTestService(t)
	a, _ := svc.CreateMediaFile(1, "/a.mp4", 1)
	b, _ := svc.CreateMediaFile(1, "/b.mp4", 1)
	c, _ := svc.CreateMediaFile(1, "/c.mp4", 1)
	album, err := svc.CreateAlbum("list", "")
	if err != nil {
		t.Fatalf("建相册失败: %v", err)
	}
	if err := svc.AddAlbumItem(album.ID, a.ID); err != nil {
		t.Fatalf("加 a: %v", err)
	}
	if err := svc.AddAlbumItem(album.ID, b.ID); err != nil {
		t.Fatalf("加 b: %v", err)
	}
	if err := svc.AddAlbumItem(album.ID, c.ID); err != nil {
		t.Fatalf("加 c: %v", err)
	}

	next, err := svc.FindAlbumNeighborInSpace(models.DefaultSpaceID, album.ID, a.ID, +1)
	if err != nil || next == nil || next.ID != b.ID {
		t.Fatalf("a 下一首应为 b, got %+v err=%v", next, err)
	}
	prev, err := svc.FindAlbumNeighborInSpace(models.DefaultSpaceID, album.ID, b.ID, -1)
	if err != nil || prev == nil || prev.ID != a.ID {
		t.Fatalf("b 上一首应为 a, got %+v err=%v", prev, err)
	}
	// 末尾
	end, err := svc.FindAlbumNeighborInSpace(models.DefaultSpaceID, album.ID, c.ID, +1)
	if err != nil {
		t.Fatalf("末项下一首查询失败: %v", err)
	}
	if end != nil {
		t.Fatalf("末项不应有下一首")
	}
	// 不在合集
	orphan, _ := svc.CreateMediaFile(1, "/x.mp4", 1)
	_, err = svc.FindAlbumNeighborInSpace(models.DefaultSpaceID, album.ID, orphan.ID, +1)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("不在合集应 NotFound, got %v", err)
	}
}
