package api

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// setupTestDB 创建测试用的内存数据库。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.User{},
		&models.PlaybackSession{},
	); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	return db
}
