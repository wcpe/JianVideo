package library

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// newTagTestService 创建带内存数据库的测试服务，额外迁移标签相关表。
func newTagTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.Tag{},
		&models.TagMapping{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(gdb), gdb
}

// createTestMedia 直接入库一条媒体记录，返回其 ID。
func createTestMedia(t *testing.T, svc *Service, libraryID int64, name string) int64 {
	t.Helper()
	mf, err := svc.CreateMediaFile(libraryID, "D:/Videos/"+name, 1024)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return mf.ID
}

func TestSetMediaFavorite(t *testing.T) {
	svc, _ := newTagTestService(t)
	id := createTestMedia(t, svc, 1, "a.mp4")

	// 设为收藏
	mf, err := svc.SetMediaFavorite(id, true)
	if err != nil {
		t.Fatalf("设置收藏失败: %v", err)
	}
	if !mf.Favorite {
		t.Fatal("期望已收藏")
	}

	// 取消收藏
	mf, err = svc.SetMediaFavorite(id, false)
	if err != nil {
		t.Fatalf("取消收藏失败: %v", err)
	}
	if mf.Favorite {
		t.Fatal("期望未收藏")
	}
}

func TestSetMediaFavorite_Idempotent(t *testing.T) {
	svc, _ := newTagTestService(t)
	id := createTestMedia(t, svc, 1, "a.mp4")

	if _, err := svc.SetMediaFavorite(id, true); err != nil {
		t.Fatalf("首次收藏失败: %v", err)
	}
	// 重复设同值应幂等，不报错
	mf, err := svc.SetMediaFavorite(id, true)
	if err != nil {
		t.Fatalf("重复收藏报错: %v", err)
	}
	if !mf.Favorite {
		t.Fatal("期望仍为已收藏")
	}
}

func TestSetMediaFavorite_NotFound(t *testing.T) {
	svc, _ := newTagTestService(t)
	if _, err := svc.SetMediaFavorite(999, true); err == nil {
		t.Fatal("期望对不存在媒体报错")
	}
}

func TestCreateTag(t *testing.T) {
	svc, _ := newTagTestService(t)
	tag, err := svc.CreateTag("旅行")
	if err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	if tag.ID == 0 || tag.Name != "旅行" {
		t.Fatalf("标签字段异常: %+v", tag)
	}
}

func TestCreateTag_UniqueByName(t *testing.T) {
	svc, _ := newTagTestService(t)
	t1, err := svc.CreateTag("旅行")
	if err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	// 同名重复创建应返回已存在的标签，不新建
	t2, err := svc.CreateTag(" 旅行 ")
	if err != nil {
		t.Fatalf("同名创建报错: %v", err)
	}
	if t1.ID != t2.ID {
		t.Fatalf("同名应复用同一标签: %d vs %d", t1.ID, t2.ID)
	}

	tags, err := svc.ListTags()
	if err != nil {
		t.Fatalf("列标签失败: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("期望 1 个标签, 实际 %d", len(tags))
	}
}

func TestCreateTag_EmptyName(t *testing.T) {
	svc, _ := newTagTestService(t)
	if _, err := svc.CreateTag("   "); err == nil {
		t.Fatal("期望空名报错")
	}
}

func TestAddAndRemoveMediaTag(t *testing.T) {
	svc, _ := newTagTestService(t)
	mediaID := createTestMedia(t, svc, 1, "a.mp4")
	tag, _ := svc.CreateTag("风景")

	if err := svc.AddMediaTag(mediaID, tag.ID); err != nil {
		t.Fatalf("打标签失败: %v", err)
	}

	tags, err := svc.ListMediaTags(mediaID)
	if err != nil {
		t.Fatalf("列媒体标签失败: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("期望媒体含标签 %d, 实际 %+v", tag.ID, tags)
	}

	// 去标签
	if err := svc.RemoveMediaTag(mediaID, tag.ID); err != nil {
		t.Fatalf("去标签失败: %v", err)
	}
	tags, _ = svc.ListMediaTags(mediaID)
	if len(tags) != 0 {
		t.Fatalf("期望去标签后无标签, 实际 %+v", tags)
	}
}

func TestAddMediaTag_Idempotent(t *testing.T) {
	svc, _ := newTagTestService(t)
	mediaID := createTestMedia(t, svc, 1, "a.mp4")
	tag, _ := svc.CreateTag("风景")

	if err := svc.AddMediaTag(mediaID, tag.ID); err != nil {
		t.Fatalf("首次打标签失败: %v", err)
	}
	// 重复打同标签应幂等
	if err := svc.AddMediaTag(mediaID, tag.ID); err != nil {
		t.Fatalf("重复打标签报错: %v", err)
	}
	tags, _ := svc.ListMediaTags(mediaID)
	if len(tags) != 1 {
		t.Fatalf("期望去重后 1 个标签, 实际 %d", len(tags))
	}
}

func TestAddMediaTag_MediaOrTagNotFound(t *testing.T) {
	svc, _ := newTagTestService(t)
	mediaID := createTestMedia(t, svc, 1, "a.mp4")
	tag, _ := svc.CreateTag("风景")

	if err := svc.AddMediaTag(999, tag.ID); err == nil {
		t.Fatal("期望媒体不存在报错")
	}
	if err := svc.AddMediaTag(mediaID, 999); err == nil {
		t.Fatal("期望标签不存在报错")
	}
}

func TestListMediaFilesFiltered_Favorite(t *testing.T) {
	svc, _ := newTagTestService(t)
	id1 := createTestMedia(t, svc, 1, "a.mp4")
	createTestMedia(t, svc, 1, "b.mp4")
	if _, err := svc.SetMediaFavorite(id1, true); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}

	fav := true
	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1, Favorite: &fav}, 1, 20)
	if err != nil {
		t.Fatalf("筛选失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != id1 {
		t.Fatalf("期望仅收藏项 %d, 实际 total=%d items=%+v", id1, total, items)
	}
}

func TestListMediaFilesFiltered_ByTag(t *testing.T) {
	svc, _ := newTagTestService(t)
	id1 := createTestMedia(t, svc, 1, "a.mp4")
	createTestMedia(t, svc, 1, "b.mp4")
	tag, _ := svc.CreateTag("精选")
	if err := svc.AddMediaTag(id1, tag.ID); err != nil {
		t.Fatalf("打标签失败: %v", err)
	}

	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1, TagID: tag.ID}, 1, 20)
	if err != nil {
		t.Fatalf("按标签筛选失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != id1 {
		t.Fatalf("期望仅含标签项 %d, 实际 total=%d items=%+v", id1, total, items)
	}
}

func TestListMediaFilesFiltered_NoFilterEqualsAll(t *testing.T) {
	svc, _ := newTagTestService(t)
	createTestMedia(t, svc, 1, "a.mp4")
	createTestMedia(t, svc, 1, "b.mp4")

	_, total, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1}, 1, 20)
	if err != nil {
		t.Fatalf("无筛选查询失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("期望返回全部 2 条, 实际 %d", total)
	}
}
