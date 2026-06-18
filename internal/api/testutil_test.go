package api

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
)

// setupTestDB 创建测试用的内存数据库。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	db.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.User{},
		&models.PlaybackSession{},
	)
	return db
}
