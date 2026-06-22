package library

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// newAlbumTestService 创建带相册相关表的测试服务。
func newAlbumTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.MediaFile{}, &models.Album{}, &models.AlbumItem{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return NewService(gdb), gdb
}

func TestCreateAndListAlbums(t *testing.T) {
	svc, _ := newAlbumTestService(t)

	if _, err := svc.CreateAlbum("", "空名称应失败"); err == nil {
		t.Fatal("空名称建相册应返回错误")
	}

	a, err := svc.CreateAlbum("旅行", "2025 年夏天")
	if err != nil {
		t.Fatalf("建相册失败: %v", err)
	}
	if a.ID == 0 || a.Name != "旅行" {
		t.Fatalf("建相册返回异常: %+v", a)
	}

	if _, err := svc.CreateAlbum("工作", ""); err != nil {
		t.Fatalf("建第二个相册失败: %v", err)
	}

	albums, err := svc.ListAlbums()
	if err != nil {
		t.Fatalf("列相册失败: %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("期望 2 个相册, 实际 %d", len(albums))
	}
}

func TestAddAndListAlbumItems_CrossLibrary(t *testing.T) {
	svc, _ := newAlbumTestService(t)

	// 跨目录媒体：library_id 不同
	m1, _ := svc.CreateMediaFile(1, "/lib1/a.mp4", 100)
	m2, _ := svc.CreateMediaFile(2, "/lib2/b.jpg", 200)

	album, err := svc.CreateAlbum("合集", "")
	if err != nil {
		t.Fatalf("建相册失败: %v", err)
	}

	if err := svc.AddAlbumItem(album.ID, m1.ID); err != nil {
		t.Fatalf("加成员 m1 失败: %v", err)
	}
	if err := svc.AddAlbumItem(album.ID, m2.ID); err != nil {
		t.Fatalf("加成员 m2 失败: %v", err)
	}

	// 重复加入应幂等，不报错也不重复
	if err := svc.AddAlbumItem(album.ID, m1.ID); err != nil {
		t.Fatalf("重复加成员应幂等, 实际报错: %v", err)
	}

	items, err := svc.ListAlbumItems(album.ID)
	if err != nil {
		t.Fatalf("列相册成员失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 个跨目录成员, 实际 %d", len(items))
	}
}

func TestAddAlbumItem_MediaNotFound(t *testing.T) {
	svc, _ := newAlbumTestService(t)
	album, _ := svc.CreateAlbum("x", "")

	err := svc.AddAlbumItem(album.ID, 9999)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("加入不存在的媒体应返回记录未找到, 实际: %v", err)
	}
}

func TestRemoveAlbumItem(t *testing.T) {
	svc, _ := newAlbumTestService(t)
	m, _ := svc.CreateMediaFile(1, "/lib1/a.mp4", 100)
	album, _ := svc.CreateAlbum("x", "")
	_ = svc.AddAlbumItem(album.ID, m.ID)

	if err := svc.RemoveAlbumItem(album.ID, m.ID); err != nil {
		t.Fatalf("移出成员失败: %v", err)
	}
	items, _ := svc.ListAlbumItems(album.ID)
	if len(items) != 0 {
		t.Fatalf("移出后期望 0 个成员, 实际 %d", len(items))
	}
}

// TestDeleteAlbum_KeepsMediaRecords 删相册不删源媒体记录（FR-40 核心验收）。
func TestDeleteAlbum_KeepsMediaRecords(t *testing.T) {
	svc, gdb := newAlbumTestService(t)
	m, _ := svc.CreateMediaFile(1, "/lib1/a.mp4", 100)
	album, _ := svc.CreateAlbum("待删", "")
	_ = svc.AddAlbumItem(album.ID, m.ID)

	if err := svc.DeleteAlbum(album.ID); err != nil {
		t.Fatalf("删相册失败: %v", err)
	}

	// 相册与成员都应被删
	var albumCount, itemCount int64
	gdb.Model(&models.Album{}).Count(&albumCount)
	gdb.Model(&models.AlbumItem{}).Count(&itemCount)
	if albumCount != 0 || itemCount != 0 {
		t.Fatalf("删相册后应清空 album/album_items, 实际 album=%d item=%d", albumCount, itemCount)
	}

	// 源媒体记录必须保留
	if _, err := svc.GetMediaFileByID(m.ID); err != nil {
		t.Fatalf("删相册后源媒体记录不应被删: %v", err)
	}
}

func TestDeleteAlbum_NotFound(t *testing.T) {
	svc, _ := newAlbumTestService(t)
	if err := svc.DeleteAlbum(9999); err == nil {
		t.Fatal("删不存在的相册应返回错误")
	}
}
