package library

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

func TestDeleteLibraryPathInSpace重试WAL快照写冲突(t *testing.T) {
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "library-delete.db")) + "?_busy_timeout=1000&_journal_mode=WAL&_foreign_keys=on"
	dbA := openLibraryDeleteWALDB(t, dsn)
	dbB := openLibraryDeleteWALDB(t, dsn)
	if err := dbA.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.WatchState{},
		&models.MediaMetadata{},
	); err != nil {
		t.Fatalf("迁移媒体库删除并发测试表失败: %v", err)
	}

	now := time.Now().UTC()
	libraryPath := models.LibraryPath{
		SpaceID: models.DefaultSpaceID,
		Path:    t.TempDir(),
		Type:    "local",
		Label:   "待删除媒体库",
		Enabled: 1,
	}
	if err := dbA.Create(&libraryPath).Error; err != nil {
		t.Fatalf("创建待删除媒体库失败: %v", err)
	}
	media := models.MediaFile{
		SpaceID:    models.DefaultSpaceID,
		LibraryID:  libraryPath.ID,
		FilePath:   "snapshot-race.mp4",
		FileName:   "snapshot-race.mp4",
		Format:     "mp4",
		AddedAt:    now,
		ModifiedAt: now,
	}
	if err := dbA.Create(&media).Error; err != nil {
		t.Fatalf("创建待删除媒体失败: %v", err)
	}

	const callbackName = "test:library-delete-busy-snapshot"
	const conflictCount = int32(4)
	var attempts atomic.Int32
	var injected atomic.Int32
	if err := dbA.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "library_paths" {
			return
		}
		attempt := attempts.Add(1)
		if attempt > conflictCount {
			return
		}
		injection := injected.Add(1)
		if err := dbB.Create(&models.MediaExtension{
			LibraryID: libraryPath.ID,
			Extension: fmt.Sprintf("snapshot-race-%d", injection),
			Type:      "video",
		}).Error; err != nil {
			t.Errorf("第 %d 次注入 WAL 并发写失败: %v", injection, err)
		}
	}); err != nil {
		t.Fatalf("注册 WAL 快照冲突回调失败: %v", err)
	}

	service := NewService(dbA)
	if err := service.DeleteLibraryPathInSpace(models.DefaultSpaceID, libraryPath.ID); err != nil {
		t.Fatalf("WAL 快照冲突后删除媒体库应自动重试: %v", err)
	}
	if injected.Load() != conflictCount {
		t.Fatalf("应连续注入 %d 次 WAL 快照冲突，实际注入 %d 次", conflictCount, injected.Load())
	}
	if attempts.Load() != conflictCount+1 {
		t.Fatalf("连续 WAL 快照冲突后应在第 5 次事务成功，实际事务尝试 %d 次", attempts.Load())
	}
	if err := dbA.Callback().Query().Remove(callbackName); err != nil {
		t.Fatalf("移除 WAL 快照冲突回调失败: %v", err)
	}

	var libraryCount int64
	if err := dbA.Model(&models.LibraryPath{}).Where("id = ?", libraryPath.ID).Count(&libraryCount).Error; err != nil {
		t.Fatalf("统计媒体库失败: %v", err)
	}
	if libraryCount != 0 {
		t.Fatalf("媒体库应已删除, 实际剩余 %d 条", libraryCount)
	}
	for _, related := range []struct {
		name  string
		model any
	}{
		{name: "媒体记录", model: &models.MediaFile{}},
		{name: "扩展名记录", model: &models.MediaExtension{}},
	} {
		var count int64
		if err := dbA.Model(related.model).Where("library_id = ?", libraryPath.ID).Count(&count).Error; err != nil {
			t.Fatalf("统计%s失败: %v", related.name, err)
		}
		if count != 0 {
			t.Fatalf("媒体库删除后%s不应残留, 实际 %d 条", related.name, count)
		}
	}
}

func openLibraryDeleteWALDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开媒体库删除 WAL 测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取媒体库删除底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
