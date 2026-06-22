package db

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// TestAutoMigrate_FoundationModels 验证基础迁移：媒体文件新增列与相册/标签/设置新表均能迁移并 round-trip。
func TestAutoMigrate_FoundationModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate-test.db")
	g, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 gorm 失败: %v", err)
	}
	// Windows 下需先关闭连接，TempDir 才能删除 db 文件
	t.Cleanup(func() {
		if s, e := g.DB(); e == nil {
			s.Close()
		}
	})
	if err := g.AutoMigrate(
		&models.MediaExtension{}, &models.MediaFile{},
		&models.Album{}, &models.AlbumItem{},
		&models.Tag{}, &models.TagMapping{}, &models.Setting{},
	); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}

	// media_files 新增列 round-trip（软删/显示名/EXIF/收藏/观看状态）
	now := time.Now()
	mf := models.MediaFile{
		LibraryID: 1, FilePath: "/x/a.heic", FileName: "a.heic",
		DisplayName: "我的照片", DeletedAt: &now,
		MediaTime: &now, MediaTimeSource: "exif", Camera: "Canon", ISO: 100,
		GPSLat: 31.2, GPSLon: 121.4, Favorite: true, LastPosition: 12.5, Watched: true,
	}
	if err := g.Create(&mf).Error; err != nil {
		t.Fatalf("写入 MediaFile 失败: %v", err)
	}
	var got models.MediaFile
	if err := g.First(&got, mf.ID).Error; err != nil {
		t.Fatalf("读回 MediaFile 失败: %v", err)
	}
	if got.DisplayName != "我的照片" || got.MediaTimeSource != "exif" || !got.Favorite || got.LastPosition != 12.5 {
		t.Fatalf("新增列未正确持久化: %+v", got)
	}

	// 新表可写
	if err := g.Create(&models.Album{Name: "假期"}).Error; err != nil {
		t.Fatalf("写入 Album 失败: %v", err)
	}
	if err := g.Create(&models.Tag{Name: "风景"}).Error; err != nil {
		t.Fatalf("写入 Tag 失败: %v", err)
	}
	if err := g.Create(&models.Setting{Key: "recycle.path.D", Value: "D:/.recycle"}).Error; err != nil {
		t.Fatalf("写入 Setting 失败: %v", err)
	}
}
